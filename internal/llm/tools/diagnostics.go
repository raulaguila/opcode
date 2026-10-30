package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/raulaguila/opcode/internal/lsp"
	"github.com/raulaguila/opcode/internal/lsp/protocol"
)

type DiagnosticsParams struct {
	FilePath string `json:"file_path"`
}
type diagnosticsTool struct {
	lspClients map[string]*lsp.Client
}

const (
	DiagnosticsToolName    = "diagnostics"
	diagnosticsDescription = `Get diagnostics for a file and/or project.
WHEN TO USE THIS TOOL:
- Use when you need to check for errors or warnings in your code
- Helpful for debugging and ensuring code quality
- Good for getting a quick overview of issues in a file or project
HOW TO USE:
- Provide a path to a file to get diagnostics for that file
- Leave the path empty to get diagnostics for the entire project
- Results are displayed in a structured format with severity levels
FEATURES:
- Displays errors, warnings, and hints
- Groups diagnostics by severity
- Provides detailed information about each diagnostic
LIMITATIONS:
- Results are limited to the diagnostics provided by the LSP clients
- May not cover all possible issues in the code
- Does not provide suggestions for fixing issues
TIPS:
- Use in conjunction with other tools for a comprehensive code review
- Combine with the LSP client for real-time diagnostics
`
)

func NewDiagnosticsTool(lspClients map[string]*lsp.Client) BaseTool {
	return &diagnosticsTool{
		lspClients,
	}
}

func (b *diagnosticsTool) Info() ToolInfo {
	return ToolInfo{
		Name:        DiagnosticsToolName,
		Description: diagnosticsDescription,
		Parameters: map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "The path to the file to get diagnostics for (leave w empty for project diagnostics)",
			},
		},
		Required: []string{},
	}
}

func (b *diagnosticsTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params DiagnosticsParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("error parsing parameters: %s", err)), nil
	}

	if params.FilePath != "" {
		path, err := authorizeFile(ctx, params.FilePath, "read")
		if err != nil {
			return ToolResponse{}, err
		}
		if err := lsp.EnsureClients(ctx, b.lspClients, path); err != nil {
			return NewTextErrorResponse(err.Error()), nil
		}
	}
	lsps := lsp.SnapshotClients(b.lspClients)

	if len(lsps) == 0 {
		return NewTextErrorResponse("no LSP clients available"), nil
	}

	if params.FilePath != "" {
		notifyLspOpenFile(ctx, params.FilePath, lsps)
		waitForLspDiagnostics(ctx, params.FilePath, lsps)
	}

	output := getDiagnostics(params.FilePath, lsps)

	return NewTextResponse(output), nil
}

func notifyLspOpenFile(ctx context.Context, filePath string, lsps map[string]*lsp.Client) {
	if lsp.EnsureClients(ctx, lsps, filePath) != nil {
		return
	}
	for _, client := range lsp.SnapshotClients(lsps) {
		err := client.OpenFile(ctx, filePath)
		if err != nil {
			continue
		}
	}
}

func waitForLspDiagnostics(ctx context.Context, path string, clients map[string]*lsp.Client) {
	clients = lsp.SnapshotClients(clients)
	if len(clients) == 0 {
		return
	}
	previous := map[string]map[protocol.DocumentUri][]protocol.Diagnostic{}
	for name, client := range clients {
		previous[name] = client.GetDiagnostics()
		if client.IsFileOpen(path) {
			_ = client.NotifyChange(ctx, path)
		} else {
			_ = client.OpenFile(ctx, path)
		}
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			return
		case <-ticker.C:
			for name, client := range clients {
				if !reflect.DeepEqual(previous[name], client.GetDiagnostics()) {
					return
				}
			}
		}
	}
}

func getDiagnostics(filePath string, lsps map[string]*lsp.Client) string {
	fileDiagnostics, projectDiagnostics := collectSourceDiagnostics(filePath, lsps)
	sortSourceDiagnostics(fileDiagnostics)
	sortSourceDiagnostics(projectDiagnostics)
	output := renderDiagnosticGroup("file_diagnostics", fileDiagnostics) + renderDiagnosticGroup("project_diagnostics", projectDiagnostics)
	if len(fileDiagnostics) > 0 || len(projectDiagnostics) > 0 {
		output += "\n<diagnostic_summary>\n"
		output += fmt.Sprintf("Current file: %d errors, %d warnings\n", countSeverity(fileDiagnostics, "Error"), countSeverity(fileDiagnostics, "Warn"))
		output += fmt.Sprintf("Project: %d errors, %d warnings\n", countSeverity(projectDiagnostics, "Error"), countSeverity(projectDiagnostics, "Warn"))
		output += "</diagnostic_summary>\n"
	}
	return output
}

func collectSourceDiagnostics(filePath string, lsps map[string]*lsp.Client) ([]string, []string) {
	fileDiagnostics, projectDiagnostics := []string{}, []string{}
	for source, client := range lsp.SnapshotClients(lsps) {
		for location, diagnostics := range client.GetDiagnostics() {
			isCurrent := location.Path() == filePath
			for _, diagnostic := range diagnostics {
				formatted := formatSourceDiagnostic(location.Path(), diagnostic, source)
				if isCurrent {
					fileDiagnostics = append(fileDiagnostics, formatted)
				} else {
					projectDiagnostics = append(projectDiagnostics, formatted)
				}
			}
		}
	}
	return fileDiagnostics, projectDiagnostics
}
func formatSourceDiagnostic(path string, diagnostic protocol.Diagnostic, source string) string {
	severity := "Info"
	switch diagnostic.Severity {
	case protocol.SeverityError:
		severity = "Error"
	case protocol.SeverityWarning:
		severity = "Warn"
	case protocol.SeverityHint:
		severity = "Hint"
	}
	location := fmt.Sprintf("%s:%d:%d", path, diagnostic.Range.Start.Line+1, diagnostic.Range.Start.Character+1)
	sourceInfo := source
	if diagnostic.Source != "" {
		sourceInfo = diagnostic.Source
	}
	codeInfo := ""
	if diagnostic.Code != nil {
		codeInfo = fmt.Sprintf("[%v]", diagnostic.Code)
	}
	return fmt.Sprintf("%s: %s [%s]%s%s %s", severity, location, sourceInfo, codeInfo, formatDiagnosticTags(diagnostic.Tags), diagnostic.Message)
}
func formatDiagnosticTags(values []protocol.DiagnosticTag) string {
	tags := []string{}
	for _, tag := range values {
		switch tag {
		case protocol.Unnecessary:
			tags = append(tags, "unnecessary")
		case protocol.Deprecated:
			tags = append(tags, "deprecated")
		}
	}
	if len(tags) == 0 {
		return ""
	}
	return " (" + strings.Join(tags, ", ") + ")"
}
func sortSourceDiagnostics(values []string) {
	sort.Slice(values, func(i, j int) bool {
		iError, jError := strings.HasPrefix(values[i], "Error"), strings.HasPrefix(values[j], "Error")
		if iError != jError {
			return iError
		}
		return values[i] < values[j]
	})
}
func renderDiagnosticGroup(tag string, values []string) string {
	if len(values) == 0 {
		return ""
	}
	output := "\n<" + tag + ">\n" + strings.Join(values[:min(10, len(values))], "\n")
	if len(values) > 10 {
		output += fmt.Sprintf("\n... and %d more diagnostics", len(values)-10)
	}
	return output + "\n</" + tag + ">\n"
}

func countSeverity(diagnostics []string, severity string) int {
	count := 0
	for _, diag := range diagnostics {
		if strings.HasPrefix(diag, severity) {
			count++
		}
	}
	return count
}
