package agent

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
)

// SourceCitation is a reference written in a response, not proof of a source read.
type SourceCitation struct {
	Path               string
	StartLine, EndLine int
}

// SourceCitations shares native citation recognition with consumers such as the
// source browser. Provenance, line coverage and freshness still need validation.
func SourceCitations(answer string) []SourceCitation {
	refs := sourceReferences(answer)
	out := make([]SourceCitation, len(refs))
	for i, ref := range refs {
		out[i] = SourceCitation{Path: ref.path, StartLine: ref.first, EndLine: ref.last}
	}
	return out
}

// SourcesRead lists, in order and once each, the files that calls read
// successfully: what a reply stands on, whether or not it cites them.
func SourcesRead(calls []message.ToolCall, results map[string]message.ToolResult) []string {
	var out []string
	for _, call := range calls {
		switch call.Name {
		case tools.ViewToolName, "read_lines", "read_symbol":
		default:
			continue
		}
		if result, ok := results[call.ID]; !ok || result.IsError {
			continue
		}
		var input struct {
			FilePath string `json:"file_path"`
			Path     string `json:"path"`
		}
		_ = json.Unmarshal([]byte(call.Input), &input)
		path := cmp.Or(input.FilePath, input.Path)
		if path == "" {
			continue
		}
		if config.Get() != nil {
			path = citationDisplayPath(path)
		}
		if !slices.Contains(out, path) {
			out = append(out, path)
		}
	}
	return out
}

// SourcesLabel names up to three sources, "main.go, cmd/root.go +2".
func SourcesLabel(paths []string) string {
	label := strings.Join(paths[:min(3, len(paths))], ", ")
	if len(paths) > 3 {
		label += fmt.Sprintf(" +%d", len(paths)-3)
	}
	return label
}
