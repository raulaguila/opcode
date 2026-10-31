package evaluation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/db"
	"github.com/raulaguila/opcode/internal/history"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/llm/prompt"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/lsp"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/version"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/spf13/viper"
)

// Worker runs in a fresh process: config and prompt caches cannot cross fixture
// boundaries. It constructs the real agent and real provider, never a fake.
func Worker(ctx context.Context, jobPath string, out io.Writer) error {
	job, cfg, err := prepareWorker(jobPath)
	if err != nil {
		return err
	}
	conn, err := db.Connect()
	if err != nil {
		return fmt.Errorf("evaluation storage initialization failed")
	}
	defer conn.Close()
	queries := db.New(conn)
	sessions := session.NewService(queries)
	messages := message.NewService(queries, message.WithSearchIndex(config.Get().Data.Directory, config.WorkingDirectory(), tools.RedactCredentials))
	files := history.NewService(queries, conn)
	permissions := permission.NewPermissionService()
	processes := tools.NewProcessManager()
	defer processes.Close()
	tasks := workflow.New(conn, job.Workspace, workflow.Mode(job.Case.Mode))
	questions := interaction.NewQuestions()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout(job.Case))*time.Second)
	defer cancel()
	requests := permissions.Subscribe(ctx)
	go func() {
		for event := range requests {
			request := event.Payload
			if allowedPermission(job.Workspace, job.AllowExecution, job.Case.Mode, request) {
				permissions.Grant(request)
			} else {
				permissions.Deny(request)
			}
		}
	}()
	service, err := agent.NewAgent(config.AgentCoder, sessions, messages, agent.CoderAgentTools(permissions, sessions, messages, files, map[string]*lsp.Client{}, processes), agent.WithWorkflow(tasks, questions))
	if err != nil {
		return fmt.Errorf("evaluation agent initialization failed")
	}
	if closer, ok := service.(interface{ Close() error }); ok {
		defer closer.Close()
	}
	saved, err := sessions.Create(ctx, "Evaluation "+job.Case.ID)
	if err != nil {
		return err
	}
	response, runError := runWorkerPrompts(ctx, service, saved.ID, job.Case.Prompts)
	task, taskErr := tasks.Get(context.Background(), saved.ID)
	history, historyErr := messages.List(context.Background(), saved.ID)
	if taskErr != nil || historyErr != nil {
		return fmt.Errorf("evaluation results unavailable")
	}

	calls, count := observedToolCalls(history)
	retrieved, strategies := workerRetrieval(history)
	result := Evaluate(job.Case, job.Workspace, Observation{Response: response, Error: runError, Tools: calls, Task: task, RetrievedPaths: retrieved})
	result.RetrievalStrategies = strategies
	result.ToolFailures = workerToolFailures(history)
	if runError != nil {
		result.AgentFailureKind = evaluationFailureKind(runError.Error())
	}
	projectWorkerUsage(ctx, service, saved.ID, cfg.Agents[config.AgentCoder].MaxTokens, job.Conditions.ContextWindow, &result)
	result.Model = string(service.Model().ID)
	result.ToolCalls = count
	result.TaskStatus = task.Status
	result.RequiresChanges = task.RequiresChanges
	result.ChangesVerified = task.StartDigest != "" && task.Digest != "" && task.StartDigest != task.Digest
	result.NoChangeJustification = strings.TrimSpace(task.NoChangeReason) != ""
	result.ToolNames = workerToolNames(calls)
	return json.NewEncoder(out).Encode(result)
}

func observedToolCalls(history []message.Message) (map[string]bool, int) {
	calls := map[string]bool{}
	count := 0
	for _, msg := range history {
		for _, call := range msg.ToolCalls() {
			if call.Origin == "context-reused" {
				continue
			}
			calls[call.Name] = true
			count++
		}
	}
	return calls, count
}
func allowedPermission(root string, execution bool, mode string, request permission.PermissionRequest) bool {
	if mode != "execute" || !execution {
		return false
	}
	path := request.Path
	if !filepath.IsAbs(path) {
		return false
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || (relative != "." && !filepath.IsLocal(relative)) {
		return false
	}
	switch request.ToolName {
	case "write", "edit", "patch", "bash", "run_checks", "lsp_rename", "lsp_replace_symbol":
		return true
	}
	return false
}

type Observation struct {
	RetrievedPaths []string
	Response       string
	Error          error
	Tools          map[string]bool
	Task           workflow.Task
}

func Evaluate(c Case, root string, observation Observation) CaseResult {
	result := CaseResult{ID: c.ID, Status: "passed"}
	if len(c.ExpectedRetrieval) > 0 {
		recall, mrr := retrievalQuality(c.ExpectedRetrieval, observation.RetrievedPaths)
		result.RetrievalRecall = &recall
		result.RetrievalMRR = &mrr
	}
	expectsError := false
	for _, criterion := range c.Criteria {
		passed := false
		switch criterion.Kind {
		case "response_cites":
			passed = responseCitesAnchor(c.Files, observation.Response, criterion)
		case "response_contains":
			passed = strings.Contains(strings.ToLower(observation.Response), strings.ToLower(criterion.Value))
		case "response_not_contains":
			passed = !strings.Contains(strings.ToLower(observation.Response), strings.ToLower(criterion.Value))
		case "tool_called":
			passed = observation.Tools[criterion.Value]
		case "expect_error":
			expectsError = true
			passed = observation.Error != nil && strings.Contains(strings.ToLower(observation.Error.Error()), strings.ToLower(criterion.Value))
		case "task_status":
			passed = observation.Task.Status == criterion.Value
		case "check_passed":
			for _, receipt := range observation.Task.Validation {
				passed = passed || receipt.Name == criterion.Value && receipt.Passed && receipt.Revision == observation.Task.Revision && receipt.Digest == observation.Task.Digest
			}
		case "file_contains", "file_not_contains":
			passed = evaluateFileCriterion(root, criterion)
		}
		value := passed
		result.Criteria = append(result.Criteria, CriterionResult{Kind: criterion.Kind, Path: criterion.Path, Passed: &value})
		if !passed {
			result.Status = "failed"
		}
	}
	if observation.Error != nil && !expectsError {
		result.Status = "failed"
		result.Error = "agent returned an error (payload omitted)"
	}
	return result
}

func retrievalQuality(expected, actual []string) (float64, float64) {
	if len(expected) == 0 {
		return 0, 0
	}
	wanted := map[string]bool{}
	for _, p := range expected {
		wanted[p] = true
	}
	found := map[string]bool{}
	mrr := 0.
	for i, p := range actual {
		if wanted[p] {
			found[p] = true
			if mrr == 0 {
				mrr = 1 / float64(i+1)
			}
		}
	}
	return float64(len(found)) / float64(len(wanted)), mrr
}

func evaluationFailureKind(text string) string {
	text = strings.ToLower(text)
	switch {
	case strings.Contains(text, "context") && strings.Contains(text, "budget"):
		return "context_budget"
	case strings.Contains(text, "permission") || strings.Contains(text, "denied"):
		return "permission"
	case strings.Contains(text, "read") && (strings.Contains(text, "before") || strings.Contains(text, "first")):
		return "current_read_required"
	case strings.Contains(text, "invalid") || strings.Contains(text, "parameter") || strings.Contains(text, "arguments") || strings.Contains(text, "schema"):
		return "arguments"
	case strings.Contains(text, "not found") || strings.Contains(text, "does not exist"):
		return "missing_resource"
	case strings.Contains(text, "match") || strings.Contains(text, "changed"):
		return "conflict_or_match"
	case strings.Contains(text, "concluded") || strings.Contains(text, "required actions"):
		return "completion_requirements"
	default:
		return "other"
	}
}

func evaluateFileCriterion(root string, criterion Criterion) bool {
	if !safePath(criterion.Path) {
		return false
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return false
	}
	defer r.Close()
	info, err := r.Lstat(criterion.Path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	file, err := r.Open(criterion.Path)
	if err != nil {
		return false
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 256<<10+1))
	if err != nil || len(raw) > 256<<10 {
		return false
	}
	passed := strings.Contains(string(raw), criterion.Value)
	if criterion.Kind == "file_not_contains" {
		passed = !passed
	}
	return passed
}

func runWorkerPrompts(ctx context.Context, service agent.Service, sessionID string, prompts []string) (string, error) {
	var response strings.Builder
	var runError error
	for _, prompt := range prompts {
		done, err := service.Run(ctx, sessionID, prompt)
		if err != nil {
			runError = err
			break
		}
		event := <-done
		response.Reset()
		response.WriteString(event.Message.Content().Text)
		response.WriteByte('\n')
		if event.Error != nil {
			runError = event.Error
			break
		}
	}
	return response.String(), runError
}

type workerRetrievalProjection struct{ paths, strategies []string }

func workerRetrieval(history []message.Message) ([]string, []string) {
	p := workerRetrievalProjection{paths: []string{}, strategies: []string{}}
	for _, msg := range history {
		for _, result := range msg.ToolResults() {
			p.add(result)
		}
	}
	return p.paths, p.strategies
}
func (p *workerRetrievalProjection) add(result message.ToolResult) {
	if result.IsError {
		return
	}
	var page struct {
		Results  []struct{ Path string }
		Strategy string
	}
	if json.Unmarshal([]byte(result.Content), &page) != nil {
		return
	}
	if page.Strategy != "" && len(p.strategies) < 8 && !contains(p.strategies, page.Strategy) {
		p.strategies = append(p.strategies, page.Strategy)
	}
	for _, hit := range page.Results {
		if len(p.paths) < 200 {
			p.paths = append(p.paths, hit.Path)
		}
	}
}

func workerToolFailures(history []message.Message) map[string]int {
	var failures map[string]int
	callsByID := map[string]string{}
	for _, msg := range history {
		for _, call := range msg.ToolCalls() {
			callsByID[call.ID] = call.Name
		}
	}
	for _, msg := range history {
		for _, r := range msg.ToolResults() {
			if r.IsError {
				if failures == nil {
					failures = map[string]int{}
				}
				name := boundedToolName(callsByID[r.ToolCallID])
				failures[name+":"+evaluationFailureKind(r.Content)]++
			}
		}
	}
	return failures
}

func boundedToolName(name string) string {
	name = tools.RedactCredentials(name)
	if len(name) > 80 {
		name = "oversized_tool_name"
	}
	return name
}

func workerToolNames(calls map[string]bool) []string {
	var names []string
	for name := range calls {
		if len(names) < 64 {
			names = append(names, tools.RedactCredentials(strings.Map(func(r rune) rune {
				if r < ' ' || r == 127 {
					return -1
				}
				return r
			}, name)))
		}
	}
	sort.Strings(names)
	return names
}

func projectWorkerUsage(ctx context.Context, service agent.Service, sessionID string, configuredReserve, requestedContext int64, result *CaseResult) {
	result.Application = "opcode"
	result.ApplicationVersion = version.Version
	result.APIModel = service.Model().APIModel
	result.Protocol = service.Model().API
	if result.Protocol == "ollama" {
		zero := 0.0
		result.Temperature = &zero
	}
	result.OutputReserve = configuredReserve
	result.PromptVersion = prompt.Version
	result.ContextWindow = service.Model().ContextWindow
	if control, ok := service.(agent.ContextControl); ok {
		if status, e := control.ContextStatus(ctx, sessionID); e == nil {
			result.ContextWindow = status.ContextWindow
			result.ContextMethod = status.Method
			result.ContextComponents = status.Components
			result.OutputReserve = status.ReservedOutputTokens
		}
	}
	result.PromptProfile = prompt.ProfileForWindow(result.ContextWindow)
	result.MetricsOrigin = "native runtime usage and observed tool receipts; title generation excluded"
	if requestedContext > 0 && result.ContextWindow != requestedContext {
		result.MetricsOrigin += "; effective context differs from requested comparison"
	}
	if usage, ok := service.(agent.UsageControl); ok {
		if measured, e := usage.Usage(ctx, sessionID); e == nil {
			result.PhaseMetrics = &measured.PhaseMetrics
			if measured.InputTokens+measured.OutputTokens > 0 {
				result.InputTokens = &measured.InputTokens
				result.OutputTokens = &measured.OutputTokens
			}
		}
	}
}

func readWorkerJob(jobPath string) (job Job, err error) {
	raw, err := readBounded(jobPath, 2<<20)
	if err != nil {
		return job, err
	}
	if json.Unmarshal(raw, &job) != nil {
		return job, fmt.Errorf("invalid evaluation job")
	}
	base := filepath.Dir(jobPath)
	if !strings.HasPrefix(filepath.Base(base), "opcode-eval-") || job.Workspace != filepath.Join(base, "workspace") || job.Config != filepath.Join(base, "config.json") {
		return job, fmt.Errorf("worker only accepts a disposable evaluation workspace")
	}
	if err := (Suite{Version: 1, Cases: []Case{job.Case}}).Validate(); err != nil {
		return job, err
	}
	if job.Case.Mode == "execute" && !job.AllowExecution {
		return job, fmt.Errorf("execution was not explicitly enabled")
	}

	return job, nil
}

func prepareWorker(jobPath string) (Job, *config.Config, error) {
	job, err := readWorkerJob(jobPath)
	if err != nil {
		return job, nil, err
	}
	if config.Get() != nil {
		return job, nil, fmt.Errorf("evaluation worker requires a fresh process")
	}
	if err := os.Chdir(job.Workspace); err != nil {
		return job, nil, err
	}
	viper.SetConfigFile(job.Config)
	cfg, err := config.Load(job.Workspace, false)
	if err != nil {
		return job, nil, fmt.Errorf("evaluation provider configuration failed")
	}
	if err := cfg.ValidateStartup(); err != nil {
		return job, nil, fmt.Errorf("evaluation provider startup validation failed")
	}
	cfg.Extensions = config.ExtensionsConfig{}
	cfg.MCPServers = map[string]config.MCPServer{}
	cfg.LSP = map[string]config.LSPConfig{}
	cfg.Security = config.SecurityConfig{Sandbox: "bubblewrap"}
	cfg.Runtime.Mode = job.Case.Mode
	cfg.Runtime.MaxSeconds = timeout(job.Case)
	return job, cfg, nil
}
