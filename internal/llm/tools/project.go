package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/raulaguila/opcode/internal/lsp/protocol"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools/shell"
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
	return ToolInfo{Name: t.name, Description: "Explore code without dumping whole files. Project map excludes secret, storage, link and dependency paths; bounded to 20000 entries with pagination. Go outlines use the parser; other languages use configured LSP document symbols. read_symbol uses an exact outline declaration name and paginates lines. Outlines are navigation, not edit-read evidence.", Required: required, Parameters: map[string]any{"path": map[string]any{"type": "string"}, "file_path": map[string]any{"type": "string"}, "symbol": map[string]any{"type": "string"}, "offset": map[string]any{"type": "integer", "minimum": 0}, "limit": map[string]any{"type": "integer", "minimum": 1}}}
}
func (t *projectTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var p struct {
		Path          string
		File          string `json:"file_path"`
		Symbol        string
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
	var symbols []codeSymbol
	if filepath.Ext(path) == ".go" {
		symbols, err = goOutline(path, raw)
	} else {
		symbols, err = t.lspOutline(ctx, path)
	}
	if err != nil {
		return NewTextErrorResponse(err.Error()), nil
	}
	if t.name == "get_file_outline" {
		limit := p.Limit
		if limit <= 0 {
			limit = 100
		}
		limit = min(limit, 500)
		start := min(p.Offset, len(symbols))
		end := min(start+limit, len(symbols))
		return jsonToolResult(map[string]any{"file_path": path, "symbols": symbols[start:end], "next_offset": end, "more": end < len(symbols), "total": len(symbols)}), nil
	}
	if p.Symbol == "" {
		return NewTextErrorResponse("symbol is required"), nil
	}
	for _, symbol := range symbols {
		if symbol.Name != p.Symbol {
			continue
		}
		limit := p.Limit
		if limit <= 0 {
			limit = 100
		}
		limit = min(limit, 500)
		start := symbol.Start - 1 + p.Offset
		if start >= symbol.End {
			return NewTextErrorResponse("offset exceeds symbol range"), nil
		}
		content, _, err := readTextFile(ctx, path, start, min(limit, symbol.End-start))
		if err != nil {
			return ToolResponse{}, err
		}
		return jsonToolResult(map[string]any{"file_path": path, "symbol": symbol, "content": addLineNumbers(content, start+1), "next_offset": p.Offset + len(strings.Split(content, "\n")), "more": start+len(strings.Split(content, "\n")) < symbol.End}), nil
	}
	return NewTextErrorResponse("declaration not found; inspect get_file_outline for exact names"), nil
}
func goOutline(path string, raw []byte) ([]codeSymbol, error) {
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, path, raw, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("Go file cannot be parsed: %w", err)
	}
	result := []codeSymbol{}
	for _, decl := range file.Decls {
		switch node := decl.(type) {
		case *ast.FuncDecl:
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
			result = append(result, codeSymbol{name, kind, set.Position(node.Pos()).Line, set.Position(node.End()).Line})
		case *ast.GenDecl:
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
					result = append(result, codeSymbol{s.Name.Name, kind, set.Position(s.Pos()).Line, set.Position(s.End()).Line})
				case *ast.ValueSpec:
					for _, name := range s.Names {
						result = append(result, codeSymbol{name.Name, node.Tok.String(), set.Position(s.Pos()).Line, set.Position(s.End()).Line})
					}
				}
			}
		}
	}
	return result, nil
}
func projectMap(ctx context.Context, path string, offset, limit int) (ToolResponse, error) {
	path, err := authorizeFile(ctx, path, "read")
	if err != nil {
		return ToolResponse{}, err
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return ToolResponse{}, err
	}
	defer root.Close()
	type item struct {
		Path     string `json:"path"`
		Bytes    int64  `json:"bytes"`
		Language string `json:"language,omitempty"`
	}
	items := []item{}
	truncated := false
	stop := fmt.Errorf("map limit")
	visited := 0
	err = fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if e := ctx.Err(); e != nil {
			return e
		}
		visited++
		if visited > 20000 {
			truncated = true
			return stop
		}
		full := filepath.Join(path, filepath.FromSlash(name))
		skip := shell.SensitivePath(full) || d.Type()&os.ModeSymlink != 0
		if cfg := config.Get(); cfg != nil && cfg.Data.Directory != "" && inside(absolutePath(cfg.Data.Directory), full) {
			skip = true
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", ".venv", "venv", "__pycache__", "target", "dist", "build", ".cache", "coverage":
				skip = true
			}
			if skip && name != "." {
				return fs.SkipDir
			}
			return nil
		}
		if skip {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		items = append(items, item{Path: name, Bytes: info.Size(), Language: strings.TrimPrefix(filepath.Ext(name), ".")})
		return nil
	})
	if err != nil && err != stop {
		return ToolResponse{}, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	if limit <= 0 {
		limit = 100
	}
	limit = min(limit, 500)
	start := min(offset, len(items))
	end := min(start+limit, len(items))
	return jsonToolResult(map[string]any{"root": path, "files": items[start:end], "total": len(items), "next_offset": end, "more": end < len(items), "truncated": truncated}), nil
}

func (t *projectTool) lspOutline(ctx context.Context, path string) ([]codeSymbol, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	clients := lsp.SnapshotClients(t.clients)
	keys := []string{}
	for key := range clients {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		client := clients[key]
		if client == nil {
			continue
		}
		if client.OpenFile(ctx, path) != nil {
			continue
		}
		var raw json.RawMessage
		if client.Call(ctx, "textDocument/documentSymbol", map[string]any{"textDocument": map[string]any{"uri": protocol.URIFromPath(path)}}, &raw) != nil {
			continue
		}
		if len(raw) > 2<<20 {
			return nil, fmt.Errorf("LSP outline exceeds size limit")
		}
		var symbols []documentSymbol
		if json.Unmarshal(raw, &symbols) != nil {
			continue
		}
		result := []codeSymbol{}
		var flatten func([]documentSymbol, string, int) error
		flatten = func(nodes []documentSymbol, prefix string, depth int) error {
			if depth > 64 {
				return fmt.Errorf("LSP outline exceeds nesting limit")
			}
			for _, node := range nodes {
				if len(result) >= 5000 {
					return fmt.Errorf("LSP outline exceeds 5000 symbols")
				}
				if node.Location != nil {
					local, err := localURI(string(node.Location.URI))
					if err != nil || filepath.Clean(local) != filepath.Clean(path) {
						continue
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
				result = append(result, codeSymbol{Name: name, Kind: "symbol", Start: int(node.Range.Start.Line) + 1, End: end})
				if err := flatten(node.Children, name, depth+1); err != nil {
					return err
				}
			}
			return nil
		}
		if err := flatten(symbols, "", 0); err != nil {
			return nil, err
		}
		return result, nil
	}
	return nil, fmt.Errorf("no configured LSP supports document outlines for this file")
}
