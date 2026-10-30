package tools

import (
	"context"
	"fmt"
	"sync/atomic"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars/bash"
	cgrammar "github.com/odvcencio/gotreesitter/grammars/c"
	csharp "github.com/odvcencio/gotreesitter/grammars/c_sharp"
	"github.com/odvcencio/gotreesitter/grammars/clojure"
	"github.com/odvcencio/gotreesitter/grammars/cpp"
	"github.com/odvcencio/gotreesitter/grammars/css"
	"github.com/odvcencio/gotreesitter/grammars/dart"
	"github.com/odvcencio/gotreesitter/grammars/elixir"
	"github.com/odvcencio/gotreesitter/grammars/erlang"
	"github.com/odvcencio/gotreesitter/grammars/fortran"
	"github.com/odvcencio/gotreesitter/grammars/fsharp"
	"github.com/odvcencio/gotreesitter/grammars/graphql"
	"github.com/odvcencio/gotreesitter/grammars/haskell"
	"github.com/odvcencio/gotreesitter/grammars/hcl"
	"github.com/odvcencio/gotreesitter/grammars/html"
	"github.com/odvcencio/gotreesitter/grammars/java"
	"github.com/odvcencio/gotreesitter/grammars/javascript"
	"github.com/odvcencio/gotreesitter/grammars/json"
	"github.com/odvcencio/gotreesitter/grammars/julia"
	"github.com/odvcencio/gotreesitter/grammars/kotlin"
	"github.com/odvcencio/gotreesitter/grammars/lua"
	"github.com/odvcencio/gotreesitter/grammars/ocaml"
	"github.com/odvcencio/gotreesitter/grammars/perl"
	"github.com/odvcencio/gotreesitter/grammars/php"
	"github.com/odvcencio/gotreesitter/grammars/powershell"
	"github.com/odvcencio/gotreesitter/grammars/proto"
	"github.com/odvcencio/gotreesitter/grammars/python"
	"github.com/odvcencio/gotreesitter/grammars/r"
	"github.com/odvcencio/gotreesitter/grammars/ruby"
	"github.com/odvcencio/gotreesitter/grammars/rust"
	"github.com/odvcencio/gotreesitter/grammars/scala"
	"github.com/odvcencio/gotreesitter/grammars/solidity"
	"github.com/odvcencio/gotreesitter/grammars/sql"
	"github.com/odvcencio/gotreesitter/grammars/svelte"
	"github.com/odvcencio/gotreesitter/grammars/swift"
	"github.com/odvcencio/gotreesitter/grammars/toml"
	"github.com/odvcencio/gotreesitter/grammars/tsx"
	"github.com/odvcencio/gotreesitter/grammars/typescript"
	"github.com/odvcencio/gotreesitter/grammars/vue"
	"github.com/odvcencio/gotreesitter/grammars/yaml"
	"github.com/odvcencio/gotreesitter/grammars/zig"
)

var localParsers = map[string]func() *ts.Language{
	"scala":      scala.Language,
	"elixir":     elixir.Language,
	"erlang":     erlang.Language,
	"haskell":    haskell.Language,
	"ocaml":      ocaml.Language,
	"r":          r.Language,
	"julia":      julia.Language,
	"perl":       perl.Language,
	"powershell": powershell.Language,
	"clojure":    clojure.Language,
	"fsharp":     fsharp.Language,
	"fortran":    fortran.Language,
	"zig":        zig.Language,
	"solidity":   solidity.Language,
	"proto":      proto.Language,
	"graphql":    graphql.Language,

	"python": python.Language, "javascript": javascript.Language, "typescript": typescript.Language, "tsx": tsx.Language,
	"rust": rust.Language, "java": java.Language, "c": cgrammar.Language, "cpp": cpp.Language, "csharp": csharp.Language,
	"ruby": ruby.Language, "php": php.Language, "kotlin": kotlin.Language, "swift": swift.Language,
	"sql": sql.Language, "bash": bash.Language, "lua": lua.Language, "dart": dart.Language,
	"vue": vue.Language, "svelte": svelte.Language, "hcl": hcl.Language, "yaml": yaml.Language,
	"toml": toml.Language, "json": json.Language, "html": html.Language, "css": css.Language,
}
var declarationKinds = map[string]string{
	"function_definition": "function", "function_declaration": "function", "generator_function_declaration": "function", "function_item": "function",
	"method_definition": "method", "method_declaration": "method", "constructor_declaration": "constructor", "class_definition": "class", "class_declaration": "class",
	"struct_item": "struct", "struct_specifier": "struct", "class_specifier": "class", "interface_declaration": "interface", "enum_declaration": "enum", "enum_item": "enum", "enum_specifier": "enum",
	"trait_item": "trait", "impl_item": "impl", "type_alias_declaration": "type", "type_item": "type", "namespace_definition": "namespace", "namespace_declaration": "namespace", "mod_item": "module",
	"lexical_declaration": "variable", "variable_declaration": "variable", "const_item": "const", "static_item": "variable",
}

func localOutline(ctx context.Context, path string, raw []byte) ([]codeSymbol, error) {
	return localOutlineLanguage(ctx, sourceLanguage(path), path, raw)
}

func localOutlineLanguage(ctx context.Context, language, path string, raw []byte) ([]codeSymbol, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(raw) > 256*1024 {
		return nil, fmt.Errorf("structural parsing limit is 256 KiB")
	}
	if language == "go" {
		return goOutline(path, raw)
	}
	load := localParsers[language]
	if load == nil {
		return nil, fmt.Errorf("no local structural parser for %s; use a configured LSP or text search", language)
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
	walker := outlineWalker{ctx: ctx, language: language, lang: lang, raw: raw, result: []codeSymbol{}}
	if err := walker.visit(root, "", 0); err != nil {
		return nil, err
	}
	return walker.result, nil
}

// outlineWalker owns one bounded traversal; parser and tree lifetime stay with
// localOutlineLanguage. It never outlives the source snapshot.
type outlineWalker struct {
	ctx                    context.Context
	language               string
	lang                   *ts.Language
	raw                    []byte
	result                 []codeSymbol
	visited, embeddedCount int
}

func (w *outlineWalker) visit(n *ts.Node, parent string, depth int) error {
	w.visited++
	if w.visited > 60000 || depth > 128 || len(w.result) > 5000 {
		return fmt.Errorf("structural outline limit reached")
	}
	if err := w.ctx.Err(); err != nil {
		return err
	}
	prefix := w.addDeclaration(n, parent)
	if err := w.addEmbeddedScript(n); err != nil {
		return err
	}
	for i := 0; i < n.NamedChildCount(); i++ {
		if err := w.visit(n.NamedChild(i), prefix, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (w *outlineWalker) addDeclaration(n *ts.Node, parent string) string {
	kind := declarationKindForNode(w.language, n, w.lang, w.raw)
	if kind == "" {
		return parent
	}
	name := localDeclarationName(w.language, n, w.lang, w.raw)
	if name == "" {
		name = w.findName(n, 0)
	}
	if name == "" || len(name) > 240 {
		return parent
	}
	qualified := name
	if parent != "" {
		qualified = parent + "." + name
	}
	last := declarationEnd(w.language, n, w.lang)
	end := int(last.EndPoint().Row) + 1
	if last.EndPoint().Column == 0 && end > int(n.StartPoint().Row)+1 {
		end--
	}
	w.result = append(w.result, codeSymbol{qualified, kind, int(n.StartPoint().Row) + 1, end})
	if kind == "variable" || kind == "const" || kind == "attribute" {
		return parent
	}
	return qualified
}

func (w *outlineWalker) addEmbeddedScript(n *ts.Node) error {
	if w.language != "vue" && w.language != "svelte" && w.language != "html" {
		return nil
	}
	if n.Type(w.lang) != "script_element" {
		return nil
	}
	w.embeddedCount++
	if w.embeddedCount > 8 {
		return fmt.Errorf("embedded script outline limit reached; use text search or LSP")
	}
	embedded, err := embeddedScriptOutline(w.ctx, n, w.lang, w.raw)
	if err != nil {
		return err
	}
	w.result = append(w.result, embedded...)
	return nil
}

func (w *outlineWalker) findName(n *ts.Node, depth int) string {
	if n == nil || depth > 8 {
		return ""
	}
	if name := n.ChildByFieldName("name", w.lang); name != nil {
		return name.Text(w.raw)
	}
	switch n.Type(w.lang) {
	case "identifier", "type_identifier", "field_identifier", "namespace_identifier", "simple_identifier", "name", "constant":
		return n.Text(w.raw)
	}
	for _, field := range []string{"declarator", "type"} {
		if name := w.findName(n.ChildByFieldName(field, w.lang), depth+1); name != "" {
			return name
		}
	}
	return w.findWrappedName(n, depth)
}

// Arrow functions and C declarators expose identifiers below a wrapper.
func (w *outlineWalker) findWrappedName(n *ts.Node, depth int) string {
	for i := 0; i < n.NamedChildCount(); i++ {
		child := n.NamedChild(i)
		switch child.Type(w.lang) {
		case "variable_declarator", "function_declarator", "pointer_declarator", "identifier", "type_identifier", "simple_identifier", "name", "constant":
			if name := w.findName(child, depth+1); name != "" {
				return name
			}
		}
	}
	return ""
}
