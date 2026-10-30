package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/diff"
	"github.com/raulaguila/opcode/internal/lsp"
	"github.com/raulaguila/opcode/internal/lsp/protocol"
	"github.com/raulaguila/opcode/internal/permission"
)

type advancedNavigationTool struct {
	name        string
	clients     map[string]*lsp.Client
	permissions permission.Service
}

func NewAdvancedNavigationTools(clients map[string]*lsp.Client, permissions permission.Service) []BaseTool {
	return []BaseTool{&advancedNavigationTool{"lsp_call_hierarchy", clients, permissions}, &advancedNavigationTool{"lsp_rename", clients, permissions}, &advancedNavigationTool{"lsp_replace_symbol", clients, permissions}}
}
func (t *advancedNavigationTool) Info() ToolInfo {
	required := []string{"file_path", "line", "character"}
	if t.name == "lsp_rename" {
		required = append(required, "new_name")
	}
	if t.name == "lsp_replace_symbol" {
		required = append(required, "content")
	}
	return ToolInfo{Name: t.name, Description: "Use configured LSP with zero-based UTF-16 positions. Call hierarchy reads incoming/outgoing callers, capped at 100 entries. Rename and symbol replacement propose protected atomic text edits and require approval including diffs; file operations from a server are rejected. Renames modify at most 128 files and 2 MiB total text.", Required: required, Parameters: map[string]any{"file_path": map[string]any{"type": "string"}, "line": map[string]any{"type": "integer", "minimum": 0}, "character": map[string]any{"type": "integer", "minimum": 0}, "direction": map[string]any{"type": "string", "enum": []string{"incoming", "outgoing"}}, "new_name": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}}}
}

type advancedNavigationParams struct {
	FilePath  string  `json:"file_path"`
	Line      int     `json:"line"`
	Character int     `json:"character"`
	Direction string  `json:"direction"`
	NewName   string  `json:"new_name"`
	Content   *string `json:"content"`
}

func (t *advancedNavigationTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	p, err := parseAdvancedNavigation(call.Input, t.name)
	if err != nil {
		return NewTextErrorResponse(err.Error()), nil
	}
	action := "read"
	if t.name != "lsp_call_hierarchy" {
		action = "write"
	}
	path, err := authorizeFile(ctx, p.FilePath, action)
	if err != nil {
		return ToolResponse{}, err
	}
	if _, err := readFile(ctx, path); err != nil {
		return ToolResponse{}, err
	}
	if err := lsp.EnsureClients(ctx, t.clients, path); err != nil {
		return NewTextErrorResponse(err.Error()), nil
	}
	return t.navigateClients(ctx, path, p)
}
func advancedNavigate(ctx context.Context, client navigationClient, kind, path string, p advancedNavigationParams, permissions permission.Service) (ToolResponse, error) {
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := client.OpenFile(requestCtx, path); err != nil {
		return ToolResponse{}, err
	}
	params := map[string]any{"textDocument": map[string]any{"uri": protocol.URIFromPath(path)}, "position": map[string]int{"line": p.Line, "character": p.Character}}
	call := func(method string, params any) (json.RawMessage, error) {
		var raw json.RawMessage
		err := client.Call(requestCtx, method, params, &raw)
		if err != nil {
			return nil, err
		}
		if len(raw) > 2<<20 || !json.Valid(raw) {
			return nil, fmt.Errorf("invalid or excessive LSP response")
		}
		return raw, nil
	}
	if kind == "lsp_call_hierarchy" {
		return navigateCallHierarchy(call, params, p.Direction)
	}
	changes, err := navigationTextChanges(ctx, call, kind, path, p, params)
	if err != nil {
		return ToolResponse{}, err
	}
	cancel() // Waiting for approval must not consume an LSP request deadline.
	return applyNavigationChanges(ctx, kind, changes, permissions)
}

type documentSymbol struct {
	Name           string             `json:"name"`
	Range          protocol.Range     `json:"range"`
	SelectionRange protocol.Range     `json:"selectionRange"`
	Children       []documentSymbol   `json:"children"`
	Location       *protocol.Location `json:"location"`
}

func positionLess(a, b protocol.Position) bool {
	return a.Line < b.Line || a.Line == b.Line && a.Character < b.Character
}
func symbolAt(symbols []documentSymbol, p protocol.Position) *documentSymbol {
	for i := range symbols {
		symbol := &symbols[i]
		if symbol.Location != nil {
			symbol.Range = symbol.Location.Range
		}
		if !positionLess(p, symbol.Range.Start) && positionLess(p, symbol.Range.End) {
			if inner := symbolAt(symbol.Children, p); inner != nil {
				return inner
			}
			return symbol
		}
	}
	return nil
}
func localURI(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" || (u.Host != "" && u.Host != "localhost") || u.RawQuery != "" || u.Fragment != "" || !filepath.IsAbs(u.Path) {
		return "", fmt.Errorf("LSP edit requires a local file URI")
	}
	return filepath.FromSlash(u.Path), nil
}
func workspaceTextChanges(ctx context.Context, raw []byte) ([]FileChange, error) {
	all, err := workspaceTextEditMap(raw)
	if err != nil {
		return nil, err
	}
	if len(all) > 128 {
		return nil, fmt.Errorf("rename exceeds 128 files")
	}
	result := []FileChange{}
	total := 0
	for uri, edits := range all {
		path, err := localURI(uri)
		if err != nil {
			return nil, err
		}
		path, err = authorizeFile(ctx, path, "write")
		if err != nil {
			return nil, err
		}
		raw, err := readFile(ctx, path)
		if err != nil {
			return nil, err
		}
		total += len(raw)
		if total > 2<<20 {
			return nil, fmt.Errorf("rename exceeds 2 MiB total text")
		}
		old := string(raw)
		next, err := applyUTF16Edits(old, edits)
		if err != nil {
			return nil, err
		}
		if next != old {
			result = append(result, FileChange{Path: path, Before: &old, After: &next})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}
func utf16Offset(text string, p protocol.Position) (int, error) {
	offset := 0
	for line := uint32(0); line < p.Line; line++ {
		at := strings.IndexByte(text[offset:], '\n')
		if at < 0 {
			return 0, fmt.Errorf("position line exceeds document")
		}
		offset += at + 1
	}
	line := text[offset:]
	if at := strings.IndexByte(line, '\n'); at >= 0 {
		line = line[:at]
	}
	line = strings.TrimSuffix(line, "\r")
	units := uint32(0)
	for pos, r := range line {
		if units == p.Character {
			return offset + pos, nil
		}
		size := uint32(1)
		if r > 0xffff {
			size = 2
		}
		units += size
		if units > p.Character {
			return 0, fmt.Errorf("position splits a UTF-16 surrogate pair")
		}
	}
	if units == p.Character {
		return offset + len(line), nil
	}
	return 0, fmt.Errorf("position character exceeds line")
}
func applyUTF16Edits(text string, edits []protocol.TextEdit) (string, error) {
	if !utf8.ValidString(text) || len(edits) > 10000 {
		return "", fmt.Errorf("invalid text or too many edits")
	}
	pending, err := utf16Replacements(text, edits)
	if err != nil {
		return "", err
	}
	sort.Slice(pending, func(i, j int) bool {
		if pending[i].start == pending[j].start {
			return pending[i].end < pending[j].end
		}
		return pending[i].start < pending[j].start
	})
	for i := 1; i < len(pending); i++ {
		if pending[i].start < pending[i-1].end || pending[i].start == pending[i-1].start {
			return "", fmt.Errorf("overlapping LSP edits")
		}
	}
	for _, edit := range slices.Backward(pending) {

		text = text[:edit.start] + edit.text + text[edit.end:]
		if len(text) > 2<<20 {
			return "", fmt.Errorf("edited text exceeds 2 MiB")
		}
	}
	return text, nil
}

func parseAdvancedNavigation(raw, kind string) (advancedNavigationParams, error) {
	var p advancedNavigationParams
	if json.Unmarshal([]byte(raw), &p) != nil || p.FilePath == "" || p.Line < 0 || p.Character < 0 || len(p.NewName) > 500 || len(p.NewName) == 0 && kind == "lsp_rename" || p.Content == nil && kind == "lsp_replace_symbol" {
		return p, fmt.Errorf("invalid LSP parameters")
	}
	if p.Content != nil && len(*p.Content) > 1<<20 {
		return p, fmt.Errorf("replacement exceeds 1 MiB")
	}
	if p.Direction == "" {
		p.Direction = "incoming"
	}
	if p.Direction != "incoming" && p.Direction != "outgoing" {
		return p, fmt.Errorf("direction must be incoming or outgoing")
	}

	return p, nil
}

func (t *advancedNavigationTool) navigateClients(ctx context.Context, path string, p advancedNavigationParams) (ToolResponse, error) {
	clients := lsp.SnapshotClients(t.clients)
	keys := []string{}
	for key := range clients {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if clients[key] == nil {
			continue
		}
		result, err := advancedNavigate(ctx, clients[key], t.name, path, p, t.permissions)
		if err == nil {
			return result, nil
		}
		if ctx.Err() != nil {
			return ToolResponse{}, ctx.Err()
		}
		if err == permission.ErrorPermissionDenied {
			return ToolResponse{}, err
		}
		if strings.Contains(err.Error(), "commit:") {
			return ToolResponse{}, err
		}
	}
	return NewTextErrorResponse("no configured LSP supports this request; no files changed"), nil
}

type navigationCall func(string, any) (json.RawMessage, error)

func navigateCallHierarchy(call navigationCall, params map[string]any, direction string) (ToolResponse, error) {

	raw, err := call("textDocument/prepareCallHierarchy", params)
	if err != nil {
		return ToolResponse{}, err
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return ToolResponse{}, fmt.Errorf("invalid call hierarchy")
	}
	result := []json.RawMessage{}
	outputBytes := 0
	truncated := len(items) > 10
outer:
	for _, item := range items[:min(10, len(items))] {
		raw, err := call("callHierarchy/"+direction+"Calls", map[string]any{"item": item})
		if err != nil {
			return ToolResponse{}, err
		}
		var children []json.RawMessage
		if string(raw) != "null" && json.Unmarshal(raw, &children) != nil {
			return ToolResponse{}, fmt.Errorf("invalid hierarchy calls")
		}
		for _, child := range children {
			if len(result) >= 100 || outputBytes+len(child) > 256<<10 {
				truncated = true
				break outer
			}
			outputBytes += len(child)
			result = append(result, child)
		}
	}
	return jsonToolResult(map[string]any{"direction": direction, "items": result, "truncated": truncated}), nil
}

func navigationTextChanges(ctx context.Context, call navigationCall, kind, path string, p advancedNavigationParams, params map[string]any) ([]FileChange, error) {
	var changes []FileChange
	if kind == "lsp_rename" {
		return renameNavigationTextChanges(ctx, call, params, p.NewName)
	} else {
		raw, err := call("textDocument/documentSymbol", map[string]any{"textDocument": params["textDocument"]})
		if err != nil {
			return nil, err
		}
		var symbols []documentSymbol
		if json.Unmarshal(raw, &symbols) != nil {
			return nil, fmt.Errorf("invalid document symbols")
		}
		symbol := symbolAt(symbols, protocol.Position{Line: uint32(p.Line), Character: uint32(p.Character)})
		if symbol == nil {
			return nil, fmt.Errorf("no symbol contains position")
		}
		before, err := readFile(ctx, path)
		if err != nil {
			return nil, err
		}
		after, err := applyUTF16Edits(string(before), []protocol.TextEdit{{Range: symbol.Range, NewText: *p.Content}})
		if err != nil {
			return nil, err
		}
		old := string(before)
		changes = []FileChange{{Path: path, Before: &old, After: &after}}
	}

	return changes, nil
}

func applyNavigationChanges(ctx context.Context, kind string, changes []FileChange, permissions permission.Service) (ToolResponse, error) {
	if len(changes) == 0 {
		return jsonToolResult(map[string]any{"changed_files": 0}), nil
	}
	session, _ := GetContextValues(ctx)
	if permissions == nil || session == "" {
		return ToolResponse{}, permission.ErrorPermissionDenied
	}
	type proposed struct {
		Path string `json:"path"`
		Diff string `json:"diff"`
	}
	proposals := []proposed{}
	for _, change := range changes {
		patch, _, _ := diff.GenerateDiff(*change.Before, *change.After, change.Path)
		if len(patch) > 2<<20 {
			return ToolResponse{}, fmt.Errorf("edit diff exceeds approval limit")
		}
		proposals = append(proposals, proposed{change.Path, redact(patch)})
	}
	if !permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: session, ToolName: kind, Action: "write", Path: config.WorkingDirectory(), Description: "Apply LSP text edits after reviewing the per-file diffs", Params: proposals}) {
		return ToolResponse{}, permission.ErrorPermissionDenied
	}
	if err := ApplyFileChanges(ctx, changes); err != nil {
		return ToolResponse{}, fmt.Errorf("commit: %w", err)
	}
	for _, change := range changes {
		recordFileWrite(change.Path)
	}
	return jsonToolResult(map[string]any{"changed_files": len(changes), "files": proposals}), nil
}

func workspaceTextEditMap(raw []byte) (map[string][]protocol.TextEdit, error) {
	var edit struct {
		Changes         map[string][]protocol.TextEdit `json:"changes"`
		DocumentChanges []json.RawMessage              `json:"documentChanges"`
	}
	if json.Unmarshal(raw, &edit) != nil {
		return nil, fmt.Errorf("invalid workspace edit")
	}
	all := map[string][]protocol.TextEdit{}
	maps.Copy(all, edit.Changes)
	for _, change := range edit.DocumentChanges {
		var d struct {
			Kind         string `json:"kind"`
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Edits []protocol.TextEdit `json:"edits"`
		}
		if json.Unmarshal(change, &d) != nil || d.Kind != "" || d.TextDocument.URI == "" {
			return nil, fmt.Errorf("LSP resource operations are unsupported; use explicit file tools")
		}
		all[d.TextDocument.URI] = append(all[d.TextDocument.URI], d.Edits...)
	}

	return all, nil
}

type utf16Replacement struct {
	start, end int
	text       string
}

func utf16Replacements(text string, edits []protocol.TextEdit) ([]utf16Replacement, error) {
	var index *utf16LineIndex
	if len(edits) > 1 {
		index = newUTF16LineIndex(text, edits)
	}
	pending := make([]utf16Replacement, 0, len(edits))
	for _, edit := range edits {
		start, err := utf16EditOffset(text, index, edit.Range.Start)
		if err != nil {
			return nil, err
		}
		end, err := utf16EditOffset(text, index, edit.Range.End)
		if err != nil || end < start {
			return nil, fmt.Errorf("invalid edit range")
		}
		pending = append(pending, utf16Replacement{start, end, edit.NewText})
	}

	return pending, nil
}

// A workspace edit may refer to the same long document thousands of times.
// Index only the required line starts once; preserve the existing UTF-16 and
// surrogate validation within each line. Single edits keep the direct path.
type utf16LineIndex struct {
	text   string
	starts []int
}

func newUTF16LineIndex(text string, edits []protocol.TextEdit) *utf16LineIndex {
	var last uint32
	for _, edit := range edits {
		last = max(last, edit.Range.Start.Line, edit.Range.End.Line)
	}
	capacity := min(uint64(last)+1, uint64(len(text))+1, 4096)
	index := &utf16LineIndex{text: text, starts: make([]int, 1, capacity)}
	for at := 0; at < len(text) && uint32(len(index.starts)-1) < last; {
		next := strings.IndexByte(text[at:], '\n')
		if next < 0 {
			break
		}
		at += next + 1
		index.starts = append(index.starts, at)
	}
	return index
}

func utf16EditOffset(text string, index *utf16LineIndex, position protocol.Position) (int, error) {
	if index == nil {
		return utf16Offset(text, position)
	}
	if uint64(position.Line) >= uint64(len(index.starts)) {
		return 0, fmt.Errorf("position line exceeds document")
	}
	start := index.starts[position.Line]
	offset, err := utf16Offset(index.text[start:], protocol.Position{Character: position.Character})
	if err != nil {
		return 0, err
	}
	return start + offset, nil
}

func renameNavigationTextChanges(ctx context.Context, call navigationCall, params map[string]any, newName string) ([]FileChange, error) {
	params["newName"] = newName
	raw, err := call("textDocument/rename", params)
	if err != nil {
		return nil, err
	}
	changes, err := workspaceTextChanges(ctx, raw)
	if err != nil {
		return nil, err
	}
	return changes, nil
}
