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
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/lsp"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/spf13/viper"
)

// Worker runs in a fresh process: config and prompt caches cannot cross fixture
// boundaries. It constructs the real agent and real provider, never a fake.
func Worker(ctx context.Context, jobPath string, out io.Writer) error {
	raw, err := readBounded(jobPath, 2<<20)
	if err != nil {
		return err
	}
	var job Job
	if json.Unmarshal(raw, &job) != nil {
		return fmt.Errorf("invalid evaluation job")
	}
	base := filepath.Dir(jobPath)
	if !strings.HasPrefix(filepath.Base(base), "opcode-eval-") || job.Workspace != filepath.Join(base, "workspace") || job.Config != filepath.Join(base, "config.json") {
		return fmt.Errorf("worker only accepts a disposable evaluation workspace")
	}
	if err := (Suite{Version: 1, Cases: []Case{job.Case}}).Validate(); err != nil {
		return err
	}
	if job.Case.Mode == "execute" && !job.AllowExecution {
		return fmt.Errorf("execution was not explicitly enabled")
	}
	if config.Get() != nil {
		return fmt.Errorf("evaluation worker requires a fresh process")
	}
	if err := os.Chdir(job.Workspace); err != nil {
		return err
	}
	viper.SetConfigFile(job.Config)
	cfg, err := config.Load(job.Workspace, false)
	if err != nil {
		return fmt.Errorf("evaluation provider configuration failed")
	}
	if err := cfg.ValidateStartup(); err != nil {
		return fmt.Errorf("evaluation provider startup validation failed")
	}
	cfg.Extensions = config.ExtensionsConfig{}
	cfg.MCPServers = map[string]config.MCPServer{}
	cfg.LSP = map[string]config.LSPConfig{}
	cfg.Security = config.SecurityConfig{Sandbox: "bubblewrap"}
	cfg.Runtime.Mode = job.Case.Mode
	cfg.Runtime.MaxSeconds = timeout(job.Case)
	conn, err := db.Connect()
	if err != nil {
		return fmt.Errorf("evaluation storage initialization failed")
	}
	defer conn.Close()
	queries := db.New(conn)
	sessions := session.NewService(queries)
	messages := message.NewService(queries)
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
	var response strings.Builder
	var runError error
	for _, prompt := range job.Case.Prompts {
		done, err := service.Run(ctx, saved.ID, prompt)
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
	task, taskErr := tasks.Get(context.Background(), saved.ID)
	history, historyErr := messages.List(context.Background(), saved.ID)
	if taskErr != nil || historyErr != nil {
		return fmt.Errorf("evaluation results unavailable")
	}

	calls := map[string]bool{}
	count := 0
	for _, msg := range history {
		for _, call := range msg.ToolCalls() {
			calls[call.Name] = true
			count++
		}
	}
	result := Evaluate(job.Case, job.Workspace, Observation{Response: response.String(), Error: runError, Tools: calls, Task: task})
	result.Model = string(service.Model().ID)
	result.ToolCalls = count
	result.TaskStatus = task.Status
	result.RequiresChanges = task.RequiresChanges
	result.ChangesVerified = task.StartDigest != "" && task.Digest != "" && task.StartDigest != task.Digest
	result.NoChangeJustification = strings.TrimSpace(task.NoChangeReason) != ""
	for name := range calls {
		if len(result.ToolNames) < 64 {
			result.ToolNames = append(result.ToolNames, tools.RedactCredentials(strings.Map(func(r rune) rune {
				if r < ' ' || r == 127 {
					return -1
				}
				return r
			}, name)))
		}
	}
	sort.Strings(result.ToolNames)
	return json.NewEncoder(out).Encode(result)
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
	Response string
	Error    error
	Tools    map[string]bool
	Task     workflow.Task
}

func Evaluate(c Case, root string, observation Observation) CaseResult {
	result := CaseResult{ID: c.ID, Status: "passed"}
	expectsError := false
	for _, criterion := range c.Criteria {
		passed := false
		switch criterion.Kind {
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
			if safePath(criterion.Path) {
				r, err := os.OpenRoot(root)
				if err == nil {
					info, err := r.Lstat(criterion.Path)
					if err == nil && info.Mode().IsRegular() {
						file, err := r.Open(criterion.Path)
						if err == nil {
							raw, readErr := io.ReadAll(io.LimitReader(file, 256<<10+1))
							file.Close()
							if readErr == nil && len(raw) <= 256<<10 {
								passed = strings.Contains(string(raw), criterion.Value)
								if criterion.Kind == "file_not_contains" {
									passed = !passed
								}
							}
						}
					}
					r.Close()
				}
			}
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
