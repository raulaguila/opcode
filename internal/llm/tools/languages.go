package tools

import (
	"context"
	"fmt"
	"github.com/raulaguila/opcode/internal/lsp"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync/atomic"

	ts "github.com/odvcencio/gotreesitter"
	cgrammar "github.com/odvcencio/gotreesitter/grammars/c"
	csharp "github.com/odvcencio/gotreesitter/grammars/c_sharp"
	"github.com/odvcencio/gotreesitter/grammars/cpp"
	"github.com/odvcencio/gotreesitter/grammars/java"
	"github.com/odvcencio/gotreesitter/grammars/javascript"
	"github.com/odvcencio/gotreesitter/grammars/python"
	"github.com/odvcencio/gotreesitter/grammars/rust"
	"github.com/odvcencio/gotreesitter/grammars/tsx"
	"github.com/odvcencio/gotreesitter/grammars/typescript"
	"github.com/raulaguila/opcode/internal/config"
)

var localParsers = map[string]func() *ts.Language{
	"python": python.Language, "javascript": javascript.Language, "typescript": typescript.Language, "tsx": tsx.Language,
	"rust": rust.Language, "java": java.Language, "c": cgrammar.Language, "cpp": cpp.Language, "csharp": csharp.Language,
}
var declarationKinds = map[string]string{
	"function_definition": "function", "function_declaration": "function", "generator_function_declaration": "function", "function_item": "function",
	"method_definition": "method", "method_declaration": "method", "constructor_declaration": "constructor", "class_definition": "class", "class_declaration": "class",
	"struct_item": "struct", "struct_specifier": "struct", "class_specifier": "class", "interface_declaration": "interface", "enum_declaration": "enum", "enum_item": "enum", "enum_specifier": "enum",
	"trait_item": "trait", "impl_item": "impl", "type_alias_declaration": "type", "type_item": "type", "namespace_definition": "namespace", "namespace_declaration": "namespace", "mod_item": "module",
	"lexical_declaration": "variable", "variable_declaration": "variable", "const_item": "const", "static_item": "variable",
}

func localOutline(ctx context.Context, path string, raw []byte) ([]codeSymbol, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(raw) > 256*1024 {
		return nil, fmt.Errorf("structural parsing limit is 256 KiB")
	}
	language := sourceLanguage(path)
	if language == "go" {
		return goOutline(path, raw)
	}
	load := localParsers[language]
	if load == nil {
		return nil, fmt.Errorf("no local structural parser for %s; use a configured LSP or text search", language)
	}
	if len(raw) > 256*1024 {
		return nil, fmt.Errorf("structural parsing limit is 256 KiB")
	}
	lang := load()
	p := ts.NewParser(lang)
	p.SetTimeoutMicros(200000)
	p.SetMemoryBudgetBytes(int64(min(32, indexLimits().MemoryMiB)) * 1024 * 1024)
	var cancelled uint32
	p.SetCancellationFlag(&cancelled)
	stop := context.AfterFunc(ctx, func() { atomic.StoreUint32(&cancelled, 1) })
	defer stop()
	tree, err := p.ParseStrict(raw)
	if tree != nil {
		defer tree.Release()
	}
	if err != nil {
		return nil, fmt.Errorf("%s parse incomplete: %w", language, err)
	}
	root := tree.RootNode()
	if root == nil || root.HasError() {
		return nil, fmt.Errorf("%s source contains syntax errors; use text search or LSP", language)
	}
	result := []codeSymbol{}
	visited := 0
	var visit func(*ts.Node, string, int) error
	var findName func(*ts.Node, int) string
	findName = func(n *ts.Node, depth int) string {
		if n == nil || depth > 8 {
			return ""
		}
		if name := n.ChildByFieldName("name", lang); name != nil {
			return name.Text(raw)
		}
		switch n.Type(lang) {
		case "identifier", "type_identifier", "field_identifier", "namespace_identifier":
			return n.Text(raw)
		}
		for _, field := range []string{"declarator", "type"} {
			if name := findName(n.ChildByFieldName(field, lang), depth+1); name != "" {
				return name
			}
		}
		// Arrow functions and C declarators expose their identifier below a wrapper.
		for i := 0; i < n.NamedChildCount(); i++ {
			child := n.NamedChild(i)
			switch child.Type(lang) {
			case "variable_declarator", "function_declarator", "pointer_declarator", "identifier", "type_identifier":
				if name := findName(child, depth+1); name != "" {
					return name
				}
			}
		}
		return ""
	}
	visit = func(n *ts.Node, parent string, depth int) error {
		visited++
		if visited > 60000 || depth > 128 || len(result) > 5000 {
			return fmt.Errorf("structural outline limit reached")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		prefix := parent
		if kind := declarationKinds[n.Type(lang)]; kind != "" {
			name := findName(n, 0)
			if name != "" && len(name) <= 240 {
				qualified := name
				if parent != "" {
					qualified = parent + "." + name
				}
				end := int(n.EndPoint().Row) + 1
				if n.EndPoint().Column == 0 && end > int(n.StartPoint().Row)+1 {
					end--
				}
				result = append(result, codeSymbol{qualified, kind, int(n.StartPoint().Row) + 1, end})
				if kind != "variable" && kind != "const" {
					prefix = qualified
				}
			}
		}
		for i := 0; i < n.NamedChildCount(); i++ {
			if err := visit(n.NamedChild(i), prefix, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(root, "", 0); err != nil {
		return nil, err
	}
	return result, nil
}

type languageSupportTool struct{}

func NewLanguageSupportTool() BaseTool { return &languageSupportTool{} }
func (*languageSupportTool) Info() ToolInfo {
	return ToolInfo{Name: "get_language_support", Description: "Inventory local structural parsers and fixture coverage separately from configured LSP/checks. Recognition is not semantic resolution; no server or install is started.", Parameters: map[string]any{}}
}
func (*languageSupportTool) Run(ctx context.Context, _ ToolCall) (ToolResponse, error) {
	if err := ctx.Err(); err != nil {
		return ToolResponse{}, err
	}
	languages := []string{"go"}
	for name := range localParsers {
		languages = append(languages, name)
	}
	sort.Strings(languages)
	items := []map[string]any{}
	for _, name := range languages {
		lspNames := []map[string]any{}
		checks := []string{}
		if cfg := config.Get(); cfg != nil {
			for key, entry := range cfg.LSP {
				if !entry.Disabled {
					types := entry.FileTypes
					if len(types) == 0 {
						types = lsp.Presets[filepath.Base(entry.Command)].Languages
					}
					languageID := name
					if name == "tsx" {
						languageID = "typescriptreact"
					}
					if slices.Contains(types, languageID) {
						_, err := exec.LookPath(entry.Command)
						lspNames = append(lspNames, map[string]any{"name": key, "executable_available": err == nil, "started_by_inventory": false})
					}
				}
			}
			for key := range cfg.Checks {
				checks = append(checks, key)
			}
		}
		sort.Slice(lspNames, func(i, j int) bool { return fmt.Sprint(lspNames[i]["name"]) < fmt.Sprint(lspNames[j]["name"]) })
		sort.Strings(checks)
		items = append(items, map[string]any{"language": name, "recognition": true, "local_parser": true, "parser_fixture": true, "semantic_references": "configured LSP only", "configured_servers": lspNames, "configured_checks": checks})
	}
	return jsonToolResult(map[string]any{"languages": items, "other_languages": "extension recognition, text search and optional configured LSP", "parser": "Go standard parser; selected gotreesitter grammars (pure Go)", "note": strings.TrimSpace("Configured checks/servers do not establish language coverage; consult server file types and check commands.")}), nil
}
