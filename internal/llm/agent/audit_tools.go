package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/workflow"
)

// Analyzers read what a model only guesses at, and do not invent. They run
// first, in the sandbox with ordinary approval, when installed.
type auditAnalyzer struct {
	name, binary, arguments string
	languages               []string
}

const auditAnalyzerTimeout = 300_000 // ms

var auditAnalyzers = []auditAnalyzer{
	{"go vet", "go", "vet ./...", []string{".go"}},
	{"golangci-lint", "golangci-lint", "run --enable gosec --output.text.path stdout --output.text.colors=false --output.text.print-issued-lines=false --max-issues-per-linter 0 --max-same-issues 0 ./...", []string{".go"}},
	{"ruff", "ruff", "check --output-format=concise .", []string{".py"}},
	{"eslint", "eslint", "-f unix .", []string{".js", ".jsx", ".ts", ".tsx"}},
}

var (
	auditAnalyzerLine = regexp.MustCompile(`^(?:vet: )?(?:\./)?([^\s:]+):(\d+)(?::\d+)?:\s*(.+)$`)
	auditLinter       = regexp.MustCompile(`\((\w+)\)\s*$`)
)

// bash returns the raw output of an approved command, or false when the mode,
// the approval or the tool does not allow it.
func (r *auditRun) bash(ctx context.Context, command string, timeout int) (string, bool) {
	index := slices.IndexFunc(r.agent.tools, func(tool tools.BaseTool) bool { return tool.Info().Name == tools.BashToolName })
	if index < 0 || !r.canExecute(ctx) {
		return "", false
	}
	input, err := json.Marshal(tools.BashParams{Command: command, Timeout: timeout})
	if err != nil {
		return "", false
	}
	ctx = tools.WithRawCommandOutput(r.toolContext(ctx))
	response, err := r.agent.tools[index].Run(ctx, tools.ToolCall{ID: uuid.NewString(), Name: tools.BashToolName, Input: string(input)})
	if err != nil {
		return "", false
	}
	return response.Content, true
}

func (r *auditRun) canExecute(ctx context.Context) bool {
	return r.executeMode(ctx) && !r.agent.commandsRefused(r.session)
}

func (r *auditRun) executeMode(ctx context.Context) bool {
	if r.agent.workflow == nil {
		return false
	}
	task, err := r.agent.workflow.Get(ctx, r.session)
	return err == nil && task.EffectiveMode() == workflow.Execute
}

func (r *auditRun) toolContext(ctx context.Context) context.Context {
	return context.WithValue(context.WithValue(ctx, tools.SessionIDContextKey, r.session), tools.MessageIDContextKey, r.reply.ID)
}

func (r *auditRun) analyze(ctx context.Context) {
	for _, analyzer := range auditAnalyzers {
		if _, err := exec.LookPath(analyzer.binary); err != nil || !r.hasLanguage(analyzer.languages) || ctx.Err() != nil {
			continue
		}
		r.progress(ctx, fmt.Sprintf(config.Text("Executando %s…", "Running %s…"), analyzer.name))
		output, ok := r.bash(ctx, analyzer.binary+" "+analyzer.arguments+" 2>&1", auditAnalyzerTimeout)
		if !ok {
			continue
		}
		// A linter built for an older Go cannot read the project at all.
		if strings.Contains(output, "(typecheck)") {
			r.report.tools = append(r.report.tools, analyzer.name+config.Text(" (falhou ao ler o projeto)", " (could not read the project)"))
			continue
		}
		r.report.tools = append(r.report.tools, analyzer.name)
		r.report.findings = append(r.report.findings, parseAnalyzerOutput(analyzer.name, output, r.source)...)
	}
}

func (r *auditRun) hasLanguage(languages []string) bool {
	for path := range r.source {
		if slices.Contains(languages, strings.ToLower(filepath.Ext(path))) {
			return true
		}
	}
	return false
}

// parseAnalyzerOutput keeps what lands in source the audit covers; output that
// names no such line, like a summary, is not a finding.
func parseAnalyzerOutput(name, output string, source map[string][]string) []auditFinding {
	var findings []auditFinding
	for line := range strings.SplitSeq(output, "\n") {
		match := auditAnalyzerLine.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		lines, covered := source[match[1]]
		number, _ := strconv.Atoi(match[2])
		if !covered || number < 1 || number > len(lines) {
			continue
		}
		findings = append(findings, auditFinding{Severity: analyzerSeverity(name, match[3]), Path: match[1], Line: number, Code: strings.TrimSpace(auditClip(lines[number-1])), Problem: match[3], Source: name})
	}
	return findings
}

func analyzerSeverity(name, problem string) string {
	if name == "go vet" {
		return "high"
	}
	linter := ""
	if match := auditLinter.FindStringSubmatch(problem); match != nil {
		linter = match[1]
	}
	switch linter {
	case "errcheck", "gosec", "bodyclose", "sqlclosecheck":
		return "medium"
	case "staticcheck", "govet":
		if strings.Contains(problem, "SA") || linter == "govet" {
			return "high"
		}
	}
	return "low"
}

// checks runs the project's configured checks where its mode allows commands,
// asked first, while someone is watching.
func (r *auditRun) checks(ctx context.Context) string {
	if r.agent.workflow == nil {
		return ""
	}
	if r.report.noSandbox {
		return config.Text("Não executadas: o sandbox não inicia nesta máquina.", "Not run: the sandbox cannot start on this machine.")
	}
	if !r.canExecute(ctx) {
		return config.Text("Não executadas: só rodam no modo Executar.", "Not run: they only run in Execute mode.")
	}
	index := slices.IndexFunc(r.agent.tools, func(tool tools.BaseTool) bool { return tool.Info().Name == "run_checks" })
	if index < 0 || len(config.Get().Checks) == 0 {
		return config.Text("Nenhuma verificação configurada.", "No checks configured.")
	}
	response, err := r.agent.tools[index].Run(r.toolContext(ctx), tools.ToolCall{ID: uuid.NewString(), Name: "run_checks", Input: "{}"})
	if err != nil {
		return err.Error()
	}
	var result struct {
		Checks []struct {
			Name    string `json:"name"`
			Output  string `json:"output"`
			IsError bool   `json:"is_error"`
		} `json:"checks"`
	}
	if json.Unmarshal([]byte(response.Content), &result) != nil {
		return tools.Clip(response.Content, 1500)
	}
	var text strings.Builder
	for _, check := range result.Checks {
		if check.IsError {
			fmt.Fprintf(&text, "- ✗ %s\n```\n%s\n```\n", check.Name, tools.Clip(check.Output, 1500))
		} else {
			fmt.Fprintf(&text, "- ✓ %s\n", check.Name)
		}
	}
	return text.String()
}
