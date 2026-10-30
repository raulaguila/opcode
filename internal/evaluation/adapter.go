package evaluation

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/workflow"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Adapter is a trusted executable selected explicitly by the user. JSON is
// passed over stdin; neither prompts nor credentials become shell arguments.
// Each application keeps its own permissions; reported policy is distinguished
// from the native Opcode worker's independently checked receipts.
type Adapter struct {
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
}
type Conditions struct {
	Model         string `json:"model"`
	APIModel      string `json:"api_model,omitempty"`
	Protocol      string `json:"protocol,omitempty"`
	OutputReserve int64  `json:"output_reserve,omitempty"`
	ContextWindow int64  `json:"context_window"`
	Mode          string `json:"mode"`
	FixtureSHA256 string `json:"fixture_sha256"`
}
type AdapterObservation struct {
	Temperature            *float64      `json:"temperature"`
	EffectiveContextWindow *int64        `json:"effective_context_window,omitempty"`
	EffectiveOutputReserve *int64        `json:"effective_output_reserve,omitempty"`
	Unavailable            string        `json:"unavailable,omitempty"`
	UnsupportedCriteria    []string      `json:"unsupported_criteria,omitempty"`
	MetricsUnavailable     []string      `json:"metrics_unavailable,omitempty"`
	Conditions             Conditions    `json:"conditions"`
	Task                   workflow.Task `json:"task,omitempty"`
	Response               string        `json:"response"`
	Error                  string        `json:"error,omitempty"`
	Tools                  []string      `json:"tools"`
	LocalTools             []string      `json:"local_tools,omitempty"`
	InputTokens            *int64        `json:"input_tokens"`
	OutputTokens           *int64        `json:"output_tokens"`
	ToolCalls              *int          `json:"tool_calls"`
	RetrievedPaths         []string      `json:"retrieved_paths"`
	Version                string        `json:"version"`
}

func LoadAdapter(path string) (*Adapter, error) {
	raw, err := readBounded(path, 32<<10)
	if err != nil {
		return nil, err
	}
	var a Adapter
	if json.Unmarshal(raw, &a) != nil || a.Name != "codaro" && a.Name != "crush" && a.Name != "opcode" || !filepath.IsAbs(a.Command) || len(a.Args) > 64 {
		return nil, fmt.Errorf("adapter needs name opcode/codaro/crush, absolute command and at most 64 arguments")
	}
	for _, arg := range a.Args {
		if len(arg) > 4096 || strings.ContainsRune(arg, 0) {
			return nil, fmt.Errorf("invalid adapter argument")
		}
	}
	return &a, nil
}
func canonicalTool(app, name string) string {
	if app == "codaro" {
		switch name {
		case "list_files":
			return "ls"
		case "search_code":
			return "search_code_index"
		case "read_lines":
			return "view"
		case "read_symbol":
			return "view"
		case "run_command":
			return "bash"
		case "run_check":
			return "run_checks"
		case "apply_changes":
			return "patch"
		}
	}
	if app == "crush" {
		switch name {
		case "multiedit":
			return "patch"
		case "rg":
			return "grep"
		}
	}
	return name
}
func adapterCommand(ctx context.Context, a *Adapter, workspace string, job Job, environment []string) (CaseResult, error) {
	raw, err := json.Marshal(job)
	if err != nil {
		return CaseResult{}, err
	}
	cmd := exec.CommandContext(ctx, a.Command, a.Args...)
	configureEvaluationProcess(cmd)
	cmd.Dir = workspace
	cmd.Env = environment
	cmd.Stdin = strings.NewReader(string(raw))
	var output boundedBuffer
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return CaseResult{ID: job.Case.ID, Status: "failed", Error: "external adapter failed or timed out; output omitted"}, nil
	}
	observed, err := decodeAdapterObservation(output, job)
	if err != nil {
		return CaseResult{}, err
	}
	if observed.Unavailable != "" {
		result := unavailableResult(job.Case, "adapter capability unavailable; see adapter documentation")
		result.Application = a.Name
		result.Model = job.Conditions.APIModel
		result.APIModel = job.Conditions.APIModel
		result.OutputReserve = job.Conditions.OutputReserve
		result.ContextWindow = job.Conditions.ContextWindow
		result.Protocol = job.Conditions.Protocol
		return result, nil
	}
	if err := validateAdapterMetrics(observed); err != nil {
		return CaseResult{}, err
	}
	calls := map[string]bool{}
	for _, name := range observed.Tools {
		calls[canonicalTool(a.Name, name)] = true
	}
	var runError error
	if observed.Error != "" {
		runError = fmt.Errorf("external adapter reported failure")
	}
	result := Evaluate(job.Case, workspace, Observation{Response: observed.Response, Error: runError, Tools: calls, Task: observed.Task, RetrievedPaths: observed.RetrievedPaths})
	if observed.Temperature != nil && (*observed.Temperature < 0 || *observed.Temperature > 2) {
		return CaseResult{}, fmt.Errorf("invalid adapter temperature")
	}
	result.Temperature = observed.Temperature
	projectAdapterCriteria(&result, job.Case, observed, runError)
	result.Application = a.Name
	projectAdapterTools(&result, a.Name, observed)
	if adapterVersionPattern.MatchString(observed.Version) {
		result.ApplicationVersion = tools.RedactCredentials(observed.Version)
	}
	projectAdapterConditions(&result, observed)
	return result, nil
}

func contains(values []string, target string) bool {
	return slices.Contains(values, target)
}

func unavailableResult(c Case, reason string) CaseResult {
	r := CaseResult{ID: c.ID, Status: "unavailable", Error: reason}
	for _, criterion := range c.Criteria {
		r.Criteria = append(r.Criteria, CriterionResult{Kind: criterion.Kind, Path: criterion.Path})
	}
	return r
}

func decodeAdapterObservation(output boundedBuffer, job Job) (AdapterObservation, error) {
	var observed AdapterObservation
	if output.overflow || json.Unmarshal(output.data, &observed) != nil || len(observed.Tools) > 2000 || len(observed.LocalTools) > 2000 || len(observed.RetrievedPaths) > 200 || len(observed.Response) > 64000 {
		return observed, fmt.Errorf("invalid or oversized adapter observation")
	}
	if observed.Conditions != job.Conditions {
		return observed, fmt.Errorf("adapter conditions differ: model, explicit context, mode and fixture hash must match")
	}
	return observed, nil
}

func validateAdapterMetrics(observed AdapterObservation) error {
	if len(observed.UnsupportedCriteria) > 8 || len(observed.MetricsUnavailable) > 12 {
		return fmt.Errorf("adapter unavailable fields exceed limits")
	}
	if observed.InputTokens != nil && *observed.InputTokens < 0 || observed.OutputTokens != nil && *observed.OutputTokens < 0 || observed.ToolCalls != nil && *observed.ToolCalls < 0 {
		return fmt.Errorf("adapter metrics must be nonnegative or null")
	}
	if observed.EffectiveContextWindow != nil && (*observed.EffectiveContextWindow < 4096 || *observed.EffectiveContextWindow > 2000000) || observed.EffectiveOutputReserve != nil && (*observed.EffectiveOutputReserve < 1 || *observed.EffectiveOutputReserve > 2000000) {
		return fmt.Errorf("invalid effective adapter context")
	}
	return nil
}

func projectAdapterCriteria(result *CaseResult, c Case, observed AdapterObservation, runError error) {
	for i, criterion := range c.Criteria {
		if contains(observed.UnsupportedCriteria, criterion.Kind) {
			result.Criteria[i].Passed = nil
		}
	}
	result.Status = "passed"
	for _, criterion := range result.Criteria {
		if criterion.Passed == nil && result.Status == "passed" {
			result.Status = "unavailable"
		}
		if criterion.Passed != nil && !*criterion.Passed {
			result.Status = "failed"
		}
	}
	if runError != nil {
		expected := adapterExpectsError(c.Criteria)
		if !expected {
			result.Status = "failed"
		}
	}
}

func projectAdapterTools(result *CaseResult, app string, observed AdapterObservation) {
	for _, name := range observed.Tools {
		if len(name) <= 80 && !contains(result.NativeToolNames, name) && len(result.NativeToolNames) < 64 {
			result.NativeToolNames = append(result.NativeToolNames, tools.RedactCredentials(name))
		}
		canonical := canonicalTool(app, name)
		if len(canonical) <= 80 && !contains(result.ToolNames, canonical) && len(result.ToolNames) < 64 {
			result.ToolNames = append(result.ToolNames, tools.RedactCredentials(canonical))
		}
	}
	projectAdapterLocalTools(result, observed.LocalTools)
}

var adapterVersionPattern = regexp.MustCompile(`^(?:v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][a-zA-Z0-9.-]{1,40})?|[a-f0-9]{7,40})$`)

func projectAdapterConditions(result *CaseResult, observed AdapterObservation) {
	result.Model = observed.Conditions.Model
	result.APIModel = observed.Conditions.APIModel
	result.Protocol = observed.Conditions.Protocol
	result.OutputReserve = observed.Conditions.OutputReserve
	result.ContextWindow = observed.Conditions.ContextWindow
	if observed.EffectiveContextWindow == nil {
		result.ConditionsUnavailable = append(result.ConditionsUnavailable, "context_window")
	}
	if observed.EffectiveOutputReserve == nil {
		result.ConditionsUnavailable = append(result.ConditionsUnavailable, "output_reserve")
	}
	if observed.EffectiveContextWindow != nil {
		result.ContextWindow = *observed.EffectiveContextWindow
	}
	if observed.EffectiveOutputReserve != nil {
		result.OutputReserve = *observed.EffectiveOutputReserve
	}
	result.InputTokens = observed.InputTokens
	result.OutputTokens = observed.OutputTokens
	result.ReportedToolCalls = observed.ToolCalls
	result.MetricsOrigin = "external adapter report; policy/tool receipts not independently verified"
	for _, name := range observed.MetricsUnavailable {
		if contains([]string{"input_tokens", "output_tokens", "tool_calls", "retrieval", "task_receipts", "prompt_profile", "context_budget"}, name) {
			result.MetricsUnavailable = append(result.MetricsUnavailable, name)
		}
	}
	if contains(result.MetricsUnavailable, "retrieval") {
		result.RetrievalRecall, result.RetrievalMRR = nil, nil
	}
}

func adapterExpectsError(criteria []Criterion) bool {
	expected := false
	for _, criterion := range criteria {
		if criterion.Kind == "expect_error" {
			expected = true
		}
	}
	return expected
}

func projectAdapterLocalTools(result *CaseResult, names []string) {
	for _, name := range names {
		if len(name) <= 80 && !contains(result.LocalToolNames, name) && len(result.LocalToolNames) < 64 {
			result.LocalToolNames = append(result.LocalToolNames, tools.RedactCredentials(name))
		}
	}
}
