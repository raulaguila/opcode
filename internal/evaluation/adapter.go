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
	ContextWindow int64  `json:"context_window"`
	Mode          string `json:"mode"`
	FixtureSHA256 string `json:"fixture_sha256"`
}
type AdapterObservation struct {
	Conditions     Conditions    `json:"conditions"`
	Task           workflow.Task `json:"task,omitempty"`
	Response       string        `json:"response"`
	Error          string        `json:"error,omitempty"`
	Tools          []string      `json:"tools"`
	InputTokens    *int64        `json:"input_tokens"`
	OutputTokens   *int64        `json:"output_tokens"`
	ToolCalls      *int          `json:"tool_calls"`
	RetrievedPaths []string      `json:"retrieved_paths"`
	Version        string        `json:"version"`
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
	cmd.Dir = workspace
	cmd.Env = environment
	cmd.Stdin = strings.NewReader(string(raw))
	var output boundedBuffer
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return CaseResult{ID: job.Case.ID, Status: "failed", Error: "external adapter failed or timed out; output omitted"}, nil
	}
	var observed AdapterObservation
	if output.overflow || json.Unmarshal(output.data, &observed) != nil || len(observed.Tools) > 2000 || len(observed.RetrievedPaths) > 200 || len(observed.Response) > 64000 {
		return CaseResult{}, fmt.Errorf("invalid or oversized adapter observation")
	}
	if observed.Conditions != job.Conditions {
		return CaseResult{}, fmt.Errorf("adapter conditions differ: model, explicit context, mode and fixture hash must match")
	}
	if observed.InputTokens != nil && *observed.InputTokens < 0 || observed.OutputTokens != nil && *observed.OutputTokens < 0 || observed.ToolCalls != nil && *observed.ToolCalls < 0 {
		return CaseResult{}, fmt.Errorf("adapter metrics must be nonnegative or null")
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
	result.Application = a.Name
	if regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][a-zA-Z0-9.-]{1,30})?$`).MatchString(observed.Version) {
		result.ApplicationVersion = tools.RedactCredentials(observed.Version)
	}
	result.Model = observed.Conditions.Model
	result.ContextWindow = observed.Conditions.ContextWindow
	result.InputTokens = observed.InputTokens
	result.OutputTokens = observed.OutputTokens
	result.ReportedToolCalls = observed.ToolCalls
	result.MetricsOrigin = "external adapter report; policy/tool receipts not independently verified"
	return result, nil
}
