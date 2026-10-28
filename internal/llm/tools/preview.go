package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// NewPreviewTool is a UI read surface. It deliberately does not establish file
// read evidence for the model; callers still wrap it in SecureTool.
func NewPreviewTool() BaseTool { return &previewTool{} }

type previewTool struct{}

func (*previewTool) Info() ToolInfo {
	return ToolInfo{Name: "file_preview", Description: "Preview a protected text file for the user without recording agent read evidence. Up to 200 lines and 64 KiB.", Required: []string{"file_path"}, Parameters: map[string]any{"file_path": map[string]any{"type": "string"}, "offset": map[string]any{"type": "integer", "minimum": 0}, "limit": map[string]any{"type": "integer", "maximum": 200}}}
}
func (*previewTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var p ViewParams
	if json.Unmarshal([]byte(call.Input), &p) != nil || p.FilePath == "" || p.Offset < 0 || p.Limit < 0 {
		return NewTextErrorResponse("invalid preview parameters"), nil
	}
	path, err := authorizeFile(ctx, p.FilePath, "read")
	if err != nil {
		return ToolResponse{}, err
	}
	if p.Limit == 0 {
		p.Limit = 200
	}
	p.Limit = min(p.Limit, 200)
	content, _, err := readTextFile(ctx, path, p.Offset, p.Limit)
	if err != nil {
		return ToolResponse{}, err
	}
	if strings.ContainsRune(content, 0) {
		return NewTextErrorResponse("binary files cannot be previewed"), nil
	}
	if len(content) > 64<<10 {
		content = content[:64<<10] + "\n[preview truncated]"
	}
	content = strings.ToValidUTF8(content, "�")
	return NewTextResponse(redact(fmt.Sprintf("%s\n%s", path, addLineNumbers(content, p.Offset+1)))), nil
}
