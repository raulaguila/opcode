// Package evaluation validates reproducible fixtures and evaluates observable
// criteria. Offline validation never simulates an agent or claims model quality.
package evaluation

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/localstate"
)

//go:embed testdata/cases.json
var fixtures embed.FS

type Suite struct {
	Version int    `json:"version"`
	Cases   []Case `json:"cases"`
}
type Case struct {
	ID             string            `json:"id"`
	Mode           string            `json:"mode"`
	Files          map[string]string `json:"files"`
	Prompts        []string          `json:"prompts"`
	Checks         map[string]string `json:"checks,omitempty"`
	Criteria       []Criterion       `json:"criteria"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
}
type Criterion struct {
	Kind  string `json:"kind"`
	Path  string `json:"path,omitempty"`
	Value string `json:"value,omitempty"`
}
type CriterionResult struct {
	Kind   string `json:"kind"`
	Path   string `json:"path,omitempty"`
	Passed *bool  `json:"passed"`
}
type CaseResult struct {
	Trial                 int               `json:"trial,omitempty"`
	DurationMS            int64             `json:"duration_ms,omitempty"`
	ID                    string            `json:"id"`
	Status                string            `json:"status"`
	Criteria              []CriterionResult `json:"criteria"`
	Error                 string            `json:"error,omitempty"`
	Model                 string            `json:"model,omitempty"`
	ToolCalls             int               `json:"tool_calls,omitempty"`
	TaskStatus            string            `json:"task_status,omitempty"`
	RequiresChanges       bool              `json:"requires_changes,omitempty"`
	ChangesVerified       bool              `json:"changes_verified,omitempty"`
	NoChangeJustification bool              `json:"no_change_justification,omitempty"`
	ToolNames             []string          `json:"tool_names,omitempty"`
}
type Report struct {
	Repeats       int          `json:"repeats,omitempty"`
	SelectedCases []string     `json:"selected_cases,omitempty"`
	Version       int          `json:"version"`
	Mode          string       `json:"mode"`
	SuiteSHA256   string       `json:"suite_sha256"`
	Cases         []CaseResult `json:"cases"`
	Notice        string       `json:"notice"`
}
type Options struct {
	Repeats        int
	CaseIDs        []string
	Live           bool
	AllowExecution bool
	ProviderConfig string
	Executable     string
}

func Load(path string) (Suite, []byte, error) {
	var raw []byte
	var err error
	if path == "" {
		raw, err = fixtures.ReadFile("testdata/cases.json")
	} else {
		raw, err = readBounded(path, 2<<20)
	}
	if err != nil {
		return Suite{}, nil, err
	}
	var suite Suite
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&suite); err != nil {
		return Suite{}, nil, fmt.Errorf("invalid evaluation JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Suite{}, nil, fmt.Errorf("evaluation JSON must contain exactly one document")
	}
	if err := suite.Validate(); err != nil {
		return Suite{}, nil, err
	}
	return suite, raw, nil
}
func (s Suite) Validate() error {
	if s.Version != 1 || len(s.Cases) == 0 || len(s.Cases) > 32 {
		return fmt.Errorf("evaluation requires version 1 and 1–32 cases")
	}
	ids := map[string]bool{}
	for _, c := range s.Cases {
		if c.ID == "" || len(c.ID) > 80 || strings.Trim(c.ID, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_") != "" || ids[c.ID] {
			return fmt.Errorf("case identifiers must be unique ASCII names")
		}
		ids[c.ID] = true
		if c.Mode != "ask" && c.Mode != "plan" && c.Mode != "execute" {
			return fmt.Errorf("case %s: mode must be ask, plan or execute", c.ID)
		}
		if len(c.Files) == 0 || len(c.Files) > 32 || len(c.Prompts) == 0 || len(c.Prompts) > 4 || len(c.Criteria) == 0 || len(c.Criteria) > 16 || len(c.Checks) > 8 {
			return fmt.Errorf("case %s exceeds fixture limits", c.ID)
		}
		size := 0
		for path, text := range c.Files {
			if !safePath(path) {
				return fmt.Errorf("case %s: unsafe fixture path", c.ID)
			}
			size += len(text)
		}
		if size > 256<<10 {
			return fmt.Errorf("case %s: fixture exceeds 256 KiB", c.ID)
		}
		for _, prompt := range c.Prompts {
			if len(prompt) == 0 || len(prompt) > 8192 {
				return fmt.Errorf("case %s: each prompt requires 1–8192 bytes", c.ID)
			}
		}
		if c.TimeoutSeconds < 0 || c.TimeoutSeconds > 600 {
			return fmt.Errorf("case %s: timeout must be at most 600 seconds", c.ID)
		}
		for name, command := range c.Checks {
			if name == "" || len(name) > 80 || command == "" || len(command) > 4096 {
				return fmt.Errorf("case %s: invalid configured check", c.ID)
			}
		}
		for _, criterion := range c.Criteria {
			if len(criterion.Value) > 2000 {
				return fmt.Errorf("case %s: criterion value exceeds 2000 bytes", c.ID)
			}
			switch criterion.Kind {
			case "response_contains", "response_not_contains", "tool_called", "expect_error", "task_status", "check_passed":
				if criterion.Value == "" {
					return fmt.Errorf("case %s: criterion value is required", c.ID)
				}
			case "file_contains", "file_not_contains":
				if !safePath(criterion.Path) || criterion.Value == "" {
					return fmt.Errorf("case %s: invalid file criterion", c.ID)
				}
			default:
				return fmt.Errorf("case %s: unknown criterion kind", c.ID)
			}
		}
	}
	return nil
}
func safePath(path string) bool {
	if !filepath.IsLocal(path) || path == "." || len(path) > 240 || strings.ContainsAny(path, "\\\x00") {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if strings.HasPrefix(part, ".") {
			return false
		}
	}
	return true
}
func readBounded(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("input must be a bounded regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("input exceeds size limit")
	}
	return raw, err
}
func Prepare(c Case) (string, string, error) {
	base, err := os.MkdirTemp("", "opcode-eval-")
	if err != nil {
		return "", "", err
	}
	workspace := filepath.Join(base, "workspace")
	if err := os.Mkdir(workspace, 0700); err != nil {
		os.RemoveAll(base)
		return "", "", err
	}
	for path, text := range c.Files {
		if !safePath(path) {
			os.RemoveAll(base)
			return "", "", fmt.Errorf("unsafe fixture path")
		}
		target := filepath.Join(workspace, path)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			os.RemoveAll(base)
			return "", "", err
		}
		if err := os.WriteFile(target, []byte(text), 0600); err != nil {
			os.RemoveAll(base)
			return "", "", err
		}
	}
	return base, workspace, nil
}
func Run(ctx context.Context, suite Suite, raw []byte, options Options) (Report, error) {
	if err := suite.Validate(); err != nil {
		return Report{}, err
	}
	repeats := options.Repeats
	if repeats == 0 {
		repeats = 1
	}
	if repeats < 1 || repeats > 3 {
		return Report{}, fmt.Errorf("repeat must be 1–3")
	}
	if !options.Live && repeats != 1 {
		return Report{}, fmt.Errorf("repeat requires --live; offline validates fixtures once")
	}
	if len(options.CaseIDs) > 0 {
		selected := map[string]bool{}
		for _, id := range options.CaseIDs {
			selected[id] = true
		}
		var cases []Case
		for _, c := range suite.Cases {
			if selected[c.ID] {
				cases = append(cases, c)
				delete(selected, c.ID)
			}
		}
		if len(selected) > 0 {
			return Report{}, fmt.Errorf("unknown evaluation case")
		}
		suite.Cases = cases
	}
	hash := sha256.Sum256(raw)
	report := Report{Repeats: repeats, SelectedCases: options.CaseIDs, Version: 1, Mode: "offline_fixture_validation", SuiteSHA256: hex.EncodeToString(hash[:]), Notice: "Offline only validates fixture contracts and safe preparation. Null criteria were not run; this is not an agent quality score."}
	if options.Live {
		if options.ProviderConfig == "" {
			return Report{}, fmt.Errorf("--live requires --provider-config; providers are never enabled implicitly")
		}
		if options.Executable == "" {
			return Report{}, fmt.Errorf("live evaluation requires an executable")
		}
		for _, c := range suite.Cases {
			if c.Mode == "execute" && !options.AllowExecution {
				return report, fmt.Errorf("case %s requires --allow-execution before any provider request", c.ID)
			}
		}
		report.Mode = "live_provider"
		report.Notice = "Live provider runs in disposable fixtures. Results verify only declared observable criteria; they do not prove general model quality."
	}
	for trial := 1; trial <= repeats; trial++ {
		for _, c := range suite.Cases {
			started := time.Now()
			if err := ctx.Err(); err != nil {
				return report, err
			}
			result := CaseResult{ID: c.ID, Status: "validated"}
			for _, criterion := range c.Criteria {
				result.Criteria = append(result.Criteria, CriterionResult{Kind: criterion.Kind, Path: criterion.Path})
			}
			if options.Live && c.Mode == "execute" && !options.AllowExecution {
				return report, fmt.Errorf("case %s requires --allow-execution", c.ID)
			}
			base, workspace, err := Prepare(c)
			if err != nil {
				return report, err
			}
			if options.Live {
				result, err = runLive(ctx, base, workspace, c, options)
			}
			removeErr := os.RemoveAll(base)
			if err != nil {
				return report, err
			}
			if removeErr != nil {
				return report, removeErr
			}
			if options.Live {
				result.DurationMS = time.Since(started).Milliseconds()
				if repeats > 1 {
					result.Trial = trial
				}
			}
			report.Cases = append(report.Cases, result)
		}
	}
	return report, nil
}

type Job struct {
	Workspace      string `json:"workspace"`
	Config         string `json:"config"`
	Case           Case   `json:"case"`
	AllowExecution bool   `json:"allow_execution"`
}

func runLive(ctx context.Context, base, workspace string, c Case, options Options) (CaseResult, error) {
	configRaw, err := readBounded(options.ProviderConfig, 2<<20)
	if err != nil {
		return CaseResult{}, fmt.Errorf("cannot read provider configuration")
	}
	var source map[string]json.RawMessage
	if json.Unmarshal(configRaw, &source) != nil {
		return CaseResult{}, fmt.Errorf("invalid provider configuration JSON")
	}
	var agentDefinitions map[string]map[string]any
	if json.Unmarshal(source["agents"], &agentDefinitions) != nil || agentDefinitions["coder"]["model"] == nil {
		return CaseResult{}, fmt.Errorf("provider configuration must explicitly select agents.coder.model")
	}
	for _, role := range []string{"title", "summarizer", "task"} {
		if agentDefinitions[role] == nil {
			agentDefinitions[role] = agentDefinitions["coder"]
		}
	}
	isolated := map[string]any{}
	for _, key := range []string{"providers", "providerProfiles", "models", "agents"} {
		if value, ok := source[key]; ok {
			isolated[key] = value
		}
	}
	isolated["agents"] = agentDefinitions
	isolated["data"] = map[string]any{"directory": filepath.Join(base, "state")}
	isolated["security"] = map[string]any{"sandbox": "bubblewrap"}
	isolated["shell"] = map[string]any{"path": "/bin/bash", "args": []string{"--noprofile", "--norc"}}
	isolated["runtime"] = map[string]any{"mode": c.Mode, "maxSteps": 20, "maxSeconds": timeout(c), "toolBudget": 64000}
	checks := map[string]any{}
	for name, command := range c.Checks {
		checks[name] = map[string]any{"command": command, "timeout": 30000}
	}
	isolated["checks"] = checks
	isolated["autoCompact"] = false
	configPath := filepath.Join(base, "config.json")
	if err := localstate.Write(configPath, isolated, 2<<20); err != nil {
		return CaseResult{}, err
	}
	jobPath := filepath.Join(base, "job.json")
	if err := localstate.Write(jobPath, Job{Workspace: workspace, Config: configPath, Case: c, AllowExecution: options.AllowExecution}, 2<<20); err != nil {
		return CaseResult{}, err
	}
	childCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout(c)+15)*time.Second)
	defer cancel()
	command := exec.CommandContext(childCtx, options.Executable, "evaluate-worker", "--job", jobPath)
	command.Dir = workspace
	allowedEnv := map[string]bool{}
	for _, key := range []string{"providers", "providerProfiles"} {
		var entries map[string]struct {
			APIKeyEnv string `json:"apiKeyEnv"`
		}
		_ = json.Unmarshal(source[key], &entries)
		for _, entry := range entries {
			if entry.APIKeyEnv != "" {
				for _, alias := range config.EnvironmentAliases(entry.APIKeyEnv) {
					allowedEnv[alias] = true
				}
			}
		}
	}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "HOME" && key != "XDG_CONFIG_HOME" && (!strings.HasPrefix(key, "OPCODE_") && !config.IsLegacyEnvironment(key) || allowedEnv[key]) {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, "HOME="+base, "XDG_CONFIG_HOME="+filepath.Join(base, "config"))
	var output boundedBuffer
	command.Stdout = &output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		message := "evaluation worker failed"
		if childCtx.Err() != nil {
			message = "evaluation worker timeout or cancellation"
		}
		return CaseResult{ID: c.ID, Status: "failed", Error: message}, nil
	}
	var result CaseResult
	if output.overflow || json.Unmarshal(output.data, &result) != nil {
		return CaseResult{ID: c.ID, Status: "failed", Error: "invalid or oversized worker report"}, nil
	}
	return result, nil
}
func timeout(c Case) int {
	if c.TimeoutSeconds > 0 {
		return c.TimeoutSeconds
	}
	return 180
}

type boundedBuffer struct {
	data     []byte
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > 128<<10 {
		b.overflow = true
	} else {
		b.data = append(b.data, p...)
	}
	return len(p), nil
}
func Failed(report Report) bool {
	for _, result := range report.Cases {
		if result.Status == "failed" {
			return true
		}
	}
	return false
}
