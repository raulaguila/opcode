package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/raulaguila/opcode/internal/lsp/protocol"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/raulaguila/opcode/internal/lsp"
)

type projectTool struct {
	name    string
	clients map[string]*lsp.Client
}
type codeSymbol struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Start int    `json:"start_line"`
	End   int    `json:"end_line"`
}

func NewProjectTools(clients map[string]*lsp.Client) []BaseTool {
	return []BaseTool{&projectTool{"get_project_map", clients}, &projectTool{"get_file_outline", clients}, &projectTool{"read_symbol", clients}}
}
func (t *projectTool) Info() ToolInfo {
	required := []string{}
	if t.name != "get_project_map" {
		required = append(required, "file_path")
	}
	if t.name == "read_symbol" {
		required = append(required, "symbol")
	}
	return ToolInfo{Name: t.name, Description: "Explore code without dumping whole files. Project map excludes secret, storage, link and dependency paths; bounded to 20000 entries with pagination. Local syntax parsers cover common programming, component and configuration languages; get_language_support inventories tested coverage and optional LSP. read_symbol uses an exact outline declaration name and paginates lines. Outlines are navigation, not edit-read evidence.", Required: required, Parameters: map[string]any{"path": map[string]any{"type": "string"}, "file_path": map[string]any{"type": "string"}, "symbol": map[string]any{"type": "string"}, "expected_hash": map[string]any{"type": "string"}, "offset": map[string]any{"type": "integer", "minimum": 0}, "limit": map[string]any{"type": "integer", "minimum": 1}}}
}
func (t *projectTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var p struct {
		Path          string
		File          string `json:"file_path"`
		Symbol        string
		ExpectedHash  string `json:"expected_hash"`
		Offset, Limit int
	}
	if json.Unmarshal([]byte(call.Input), &p) != nil || p.Offset < 0 || p.Limit < 0 {
		return NewTextErrorResponse("invalid code exploration parameters"), nil
	}
	if t.name == "get_project_map" {
		return projectMap(ctx, p.Path, p.Offset, p.Limit)
	}
	if p.File == "" {
		return NewTextErrorResponse("file_path is required"), nil
	}
	path, err := authorizeFile(ctx, p.File, "read")
	if err != nil {
		return ToolResponse{}, err
	}
	raw, err := readFile(ctx, path)
	if err != nil {
		return ToolResponse{}, err
	}
	if p.ExpectedHash != "" && p.ExpectedHash != indexDigest(string(raw)) {
		return NewTextErrorResponse("file changed; read the current outline again"), nil
	}
	symbols, err := t.outlineSource(ctx, path, raw)
	if err != nil {
		return NewTextErrorResponse(err.Error()), nil
	}
	if t.name == "get_file_outline" {
		return sourceOutlineResponse(path, raw, symbols, p.Offset, p.Limit), nil
	}
	if p.Symbol == "" {
		return NewTextErrorResponse("symbol is required"), nil
	}
	for _, symbol := range symbols {
		if symbol.Name != p.Symbol {
			continue
		}
		return sourceSymbolResponse(ctx, path, raw, symbol, p.Offset, p.Limit)
	}
	return NewTextErrorResponse("declaration not found; inspect get_file_outline for exact names"), nil
}
func goOutline(path string, raw []byte) ([]codeSymbol, error) {
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, path, raw, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("cannot parse Go file: %w", err)
	}
	result := []codeSymbol{}
	for _, decl := range file.Decls {
		switch node := decl.(type) {
		case *ast.FuncDecl:
			goOutlineFuncDecl(set, &result, node)
		case *ast.GenDecl:
			goOutlineGenDecl(set, &result, node)
		}
	}
	return result, nil
}
func projectMap(ctx context.Context, path string, offset, limit int) (ToolResponse, error) {
	return structuralProjectMap(ctx, path, offset, limit)
}

func (t *projectTool) lspOutline(ctx context.Context, path string) ([]codeSymbol, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := lsp.EnsureClients(ctx, t.clients, path); err != nil {
		return nil, err
	}
	clients := lsp.SnapshotClients(t.clients)
	keys := []string{}
	for key := range clients {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		symbols, available, err := clientSourceOutline(ctx, path, clients[key])
		if err != nil {
			return nil, err
		}
		if available {
			return symbols, nil
		}
	}
	return nil, fmt.Errorf("no configured LSP supports document outlines for this file")
}
func clientSourceOutline(ctx context.Context, path string, client *lsp.Client) ([]codeSymbol, bool, error) {
	if client == nil {
		return nil, false, nil
	}
	if client.OpenFile(ctx, path) != nil {
		return nil, false, nil
	}
	var raw json.RawMessage
	if client.Call(ctx, "textDocument/documentSymbol", map[string]any{"textDocument": map[string]any{"uri": protocol.URIFromPath(path)}}, &raw) != nil {
		return nil, false, nil
	}
	if len(raw) > 2<<20 {
		return nil, false, fmt.Errorf("LSP outline exceeds size limit")
	}
	var symbols []documentSymbol
	if json.Unmarshal(raw, &symbols) != nil {
		return nil, false, nil
	}
	walker := sourceOutlineWalker{path: path, result: []codeSymbol{}}
	if err := walker.flatten(symbols, "", 0); err != nil {
		return nil, false, err
	}
	return walker.result, true, nil
}

type sourceOutlineWalker struct {
	path   string
	result []codeSymbol
}

func (w *sourceOutlineWalker) flatten(nodes []documentSymbol, prefix string, depth int) error {
	if depth > 64 {
		return fmt.Errorf("LSP outline exceeds nesting limit")
	}
	for _, node := range nodes {
		if len(w.result) >= 5000 {
			return fmt.Errorf("LSP outline exceeds 5000 symbols")
		}
		symbol, ok := w.symbol(node, prefix)
		if !ok {
			continue
		}
		w.result = append(w.result, symbol)
		if err := w.flatten(node.Children, symbol.Name, depth+1); err != nil {
			return err
		}
	}
	return nil
}
func (w *sourceOutlineWalker) symbol(node documentSymbol, prefix string) (codeSymbol, bool) {
	if node.Location != nil {
		local, err := localURI(string(node.Location.URI))
		if err != nil || filepath.Clean(local) != filepath.Clean(w.path) {
			return codeSymbol{}, false
		}
		node.Range = node.Location.Range
	}
	name := node.Name
	if prefix != "" {
		name = prefix + "." + name
	}
	end := int(node.Range.End.Line) + 1
	if node.Range.End.Character == 0 && node.Range.End.Line > node.Range.Start.Line {
		end--
	}
	return codeSymbol{Name: name, Kind: "symbol", Start: int(node.Range.Start.Line) + 1, End: end}, true
}

func goOutlineFuncDecl(set *token.FileSet, result *[]codeSymbol, node *ast.FuncDecl) {
	name := node.Name.Name
	kind := "function"
	if node.Recv != nil && len(node.Recv.List) > 0 {
		kind = "method"
		recv := node.Recv.List[0].Type
		if star, ok := recv.(*ast.StarExpr); ok {
			recv = star.X
		}
		if ident, ok := recv.(*ast.Ident); ok {
			name = ident.Name + "." + name
		}
	}
	(*result) = append((*result), codeSymbol{name, kind, set.Position(node.Pos()).Line, set.Position(node.End()).Line})
}

func sourceOutlineResponse(path string, raw []byte, symbols []codeSymbol, offset, limit int) ToolResponse {
	if limit <= 0 {
		limit = 100
	}
	limit = min(limit, 500)
	start := min(offset, len(symbols))
	end := min(start+limit, len(symbols))
	return jsonToolResult(map[string]any{"file_hash": indexDigest(string(raw)), "file_path": path, "symbols": symbols[start:end], "next_offset": end, "more": end < len(symbols), "total": len(symbols)})
}

func sourceSymbolResponse(ctx context.Context, path string, raw []byte, symbol codeSymbol, offset, limit int) (ToolResponse, error) {
	if limit <= 0 {
		limit = 100
	}
	limit = min(limit, 500)
	start := symbol.Start - 1 + offset
	if start >= symbol.End {
		return NewTextErrorResponse("offset exceeds symbol range"), nil
	}
	content, _, err := readTextReader(ctx, bytes.NewReader(raw), start, min(limit, symbol.End-start))
	if err != nil {
		return ToolResponse{}, err
	}
	lineCount := strings.Count(content, "\n") + 1
	recordFileRead(path)
	response := jsonToolResult(map[string]any{"file_hash": indexDigest(string(raw)), "file_path": path, "symbol": symbol, "content": addLineNumbers(content, start+1), "next_offset": offset + lineCount, "more": start+lineCount < symbol.End})
	if content != "" {
		response = sourceReadMetadata(response, path, raw, start+1, start+lineCount)
	}
	return response, nil
}

func goOutlineGenDecl(set *token.FileSet, result *[]codeSymbol, node *ast.GenDecl) {
	for _, spec := range node.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			kind := "type"
			switch s.Type.(type) {
			case *ast.StructType:
				kind = "struct"
			case *ast.InterfaceType:
				kind = "interface"
			}
			*result = append(*result, codeSymbol{s.Name.Name, kind, set.Position(s.Pos()).Line, set.Position(s.End()).Line})
		case *ast.ValueSpec:
			for _, name := range s.Names {
				*result = append(*result, codeSymbol{name.Name, node.Tok.String(), set.Position(s.Pos()).Line, set.Position(s.End()).Line})
			}
		}
	}
}

func (t *projectTool) outlineSource(ctx context.Context, path string, raw []byte) ([]codeSymbol, error) {
	language := sourceLanguage(path)
	if language == "go" || localParsers[language] != nil {
		return localOutline(ctx, path, raw)
	}
	return t.lspOutline(ctx, path)
}
