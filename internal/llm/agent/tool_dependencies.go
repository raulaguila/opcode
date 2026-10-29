package agent

import (
	"encoding/json"
	"path/filepath"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/diff"
	"github.com/raulaguila/opcode/internal/message"
)

func toolFilePath(raw string) string {
	var input struct {
		FilePath string `json:"file_path"`
		Path     string `json:"path"`
	}
	if json.Unmarshal([]byte(raw), &input) != nil {
		return ""
	}
	if input.FilePath != "" {
		return input.FilePath
	}
	return input.Path
}

func normalizedToolPath(path string) string {
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(config.WorkingDirectory(), path)
	}
	return filepath.Clean(path)
}

// Tools in one model response share the same input history: even when executed
// sequentially, an edit in that response cannot be based on a sibling read's
// result. Return a no-op receipt for that dependent edit, so the next generation
// can use the actual source. Independent reads/edits remain batchable.
func batchSourceReads(calls []message.ToolCall) map[string]bool {
	paths := map[string]bool{}
	for _, call := range calls {
		switch call.Name {
		case "view", "read_lines", "read_symbol":
			if path := normalizedToolPath(toolFilePath(call.Input)); path != "" {
				paths[path] = true
			}
		}
	}
	return paths
}

func mutationAwaitingSource(call message.ToolCall, reads map[string]bool) bool {
	switch call.Name {
	case "edit", "write", "lsp_replace_symbol", "lsp_rename":
		return reads[normalizedToolPath(toolFilePath(call.Input))]
	case "patch":
		var input struct {
			Patch string `json:"patch_text"`
		}
		if json.Unmarshal([]byte(call.Input), &input) == nil {
			for _, path := range diff.IdentifyFilesNeeded(input.Patch) {
				if reads[normalizedToolPath(path)] {
					return true
				}
			}
		}
	}
	return false
}
