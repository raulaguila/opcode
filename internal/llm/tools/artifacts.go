package tools

import (
	"context"
	"encoding/json"
	"path/filepath"

	"github.com/raulaguila/opcode/internal/artifact"
	"github.com/raulaguila/opcode/internal/config"
)

type artifactTool struct{ name string }

func artifactStore() *artifact.Store {
	cfg := config.Get()
	if cfg == nil || cfg.Data.Directory == "" {
		return artifact.New("")
	}
	return artifact.New(filepath.Join(cfg.Data.Directory, "artifacts"))
}
func NewArtifactTools() []BaseTool {
	return []BaseTool{&artifactTool{"get_artifact_info"}, &artifactTool{"read_artifact"}, &artifactTool{"search_artifact"}}
}
func (t *artifactTool) Info() ToolInfo {
	required := []string{"artifact_id"}
	if t.name == "search_artifact" {
		required = append(required, "query")
	}
	return ToolInfo{Name: t.name, Description: "Inspect a private immutable snapshot of prior tool output in this session. Byte offsets, pages up to 32768 bytes, searches up to 100 matches. Snapshots are not current file-read evidence. Never rerun a command just to recover its saved output.", Required: required, Parameters: map[string]any{"artifact_id": map[string]any{"type": "string"}, "query": map[string]any{"type": "string"}, "offset": map[string]any{"type": "integer", "minimum": 0}, "limit": map[string]any{"type": "integer", "minimum": 1}}}
}
func (t *artifactTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var p struct {
		ID            string `json:"artifact_id"`
		Query         string `json:"query"`
		Offset, Limit int
	}
	if json.Unmarshal([]byte(call.Input), &p) != nil {
		return NewTextErrorResponse("invalid artifact parameters"), nil
	}
	session, _ := GetContextValues(ctx)
	store := artifactStore()
	var result any
	var err error
	switch t.name {
	case "get_artifact_info":
		result, err = store.Info(ctx, session, p.ID)
	case "read_artifact":
		result, err = store.Read(ctx, session, p.ID, p.Offset, p.Limit)
	default:
		result, err = store.Search(ctx, session, p.ID, p.Query, p.Offset, p.Limit)
	}
	if err != nil {
		return NewTextErrorResponse(err.Error()), nil
	}
	return jsonToolResult(result), nil
}

// SnapshotOutput saves the bounded, redacted output once, before presentation
// truncation. Errors remain visible; an unavailable store never claims a snapshot.
func SnapshotOutput(ctx context.Context, label, content string) (string, error) {
	session, _ := GetContextValues(ctx)
	info, err := artifactStore().Put(ctx, session, redact(label), redactLive(cleanProcessOutput(content), true))
	if err != nil {
		return "", err
	}
	raw, _ := json.Marshal(info)
	return string(raw), nil
}
