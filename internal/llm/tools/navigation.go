package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/raulaguila/opcode/internal/lsp"
	"github.com/raulaguila/opcode/internal/lsp/protocol"
)

type navigationClient interface {
	OpenFile(context.Context, string) error
	Call(context.Context, string, any, any) error
}
type navigationTool struct {
	name    string
	clients map[string]*lsp.Client
}

func NewNavigationTools(clients map[string]*lsp.Client) []BaseTool {
	return []BaseTool{&navigationTool{"symbols", clients}, &navigationTool{"definition", clients}, &navigationTool{"references", clients}}
}
func (n *navigationTool) Info() ToolInfo {
	required := []string{"file_path"}
	if n.name != "symbols" {
		required = append(required, "line", "character")
	}
	return ToolInfo{Name: n.name, Description: "Navigate code using configured LSP servers. Positions are zero-based and characters count UTF-16 code units. Results are bounded to 100 entries and include file URI and ranges.", Parameters: map[string]any{"file_path": map[string]any{"type": "string"}, "line": map[string]any{"type": "integer", "minimum": 0}, "character": map[string]any{"type": "integer", "minimum": 0}}, Required: required}
}
func (n *navigationTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var input struct {
		FilePath  string `json:"file_path"`
		Line      int    `json:"line"`
		Character int    `json:"character"`
	}
	if json.Unmarshal([]byte(call.Input), &input) != nil || input.FilePath == "" || input.Line < 0 || input.Character < 0 {
		return NewTextErrorResponse("invalid navigation parameters"), nil
	}
	path, err := authorizeFile(ctx, input.FilePath, "read")
	if err != nil {
		return ToolResponse{}, err
	}
	if _, err := readFile(ctx, path); err != nil {
		return ToolResponse{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	clients := lsp.SnapshotClients(n.clients)
	keys := make([]string, 0, len(clients))
	for key := range clients {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var last error
	for _, key := range keys {
		client := clients[key]
		if client == nil {
			continue
		}
		result, err := navigate(ctx, client, n.name, path, input.Line, input.Character)
		if err == nil {
			return result, nil
		}
		last = err
	}
	if last != nil {
		return NewTextErrorResponse("no configured LSP could resolve this navigation request"), nil
	}
	return NewTextErrorResponse("no LSP clients available"), nil
}
func navigate(ctx context.Context, client navigationClient, kind, path string, line, character int) (ToolResponse, error) {
	if err := client.OpenFile(ctx, path); err != nil {
		return ToolResponse{}, err
	}
	params := map[string]any{"textDocument": map[string]any{"uri": protocol.URIFromPath(path)}}
	method := "textDocument/documentSymbol"
	if kind != "symbols" {
		params["position"] = map[string]int{"line": line, "character": character}
		method = "textDocument/" + kind
	}
	if kind == "references" {
		params["context"] = map[string]bool{"includeDeclaration": true}
	}
	var raw json.RawMessage
	if err := client.Call(ctx, method, params, &raw); err != nil {
		return ToolResponse{}, err
	}
	if len(raw) > 2<<20 {
		return ToolResponse{}, fmt.Errorf("LSP result exceeds size limit")
	}
	var items []json.RawMessage
	if string(raw) != "null" {
		if json.Unmarshal(raw, &items) != nil {
			if !json.Valid(raw) {
				return ToolResponse{}, fmt.Errorf("invalid LSP result")
			}
			items = []json.RawMessage{raw}
		}
	}
	truncated := len(items) > 100
	if truncated {
		items = items[:100]
	}
	return jsonToolResult(map[string]any{"items": items, "truncated": truncated}), nil
}
