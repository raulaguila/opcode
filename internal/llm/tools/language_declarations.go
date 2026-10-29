package tools

import (
	"context"
	"strconv"
	"strings"

	ts "github.com/odvcencio/gotreesitter"
)

// These are syntax navigation declarations, never binding or reference evidence.
// Keep grammar-specific node kinds separate: e.g. an HCL block is not a CSS block.
var languageDeclarationKinds = map[string]map[string]string{
	"scala":      {"object_definition": "object", "trait_definition": "trait"},
	"erlang":     {"fun_decl": "function"},
	"haskell":    {"bind": "variable", "function": "function", "data_type": "type", "newtype": "type"},
	"ocaml":      {"let_binding": "variable", "type_binding": "type", "module_binding": "module"},
	"perl":       {"subroutine_declaration_statement": "function"},
	"powershell": {"function_statement": "function", "class_statement": "class"},
	"fsharp":     {"named_module": "module", "function_or_value_defn": "variable", "type_definition": "type"},
	"fortran":    {"module": "module", "function": "function", "subroutine": "function"},
	"solidity":   {"contract_declaration": "contract", "event_definition": "event"},
	"proto":      {"message": "message", "enum": "enum", "service": "service", "rpc": "method"},
	"graphql":    {"object_type_definition": "type", "interface_type_definition": "interface", "enum_type_definition": "enum", "input_object_type_definition": "type", "scalar_type_definition": "type"},
	"ruby":       {"class": "class", "module": "module", "method": "method", "singleton_method": "method"},
	"kotlin":     {"object_declaration": "object", "type_alias": "type"},
	"dart":       {"function_signature": "function", "constructor_signature": "constructor", "enum_declaration": "enum"},
	"sql":        {"create_table_statement": "table", "create_view_statement": "view", "create_function_statement": "function", "create_index_statement": "index"},
	"hcl":        {"block": "block", "attribute": "attribute"},
	"yaml":       {"block_mapping_pair": "key", "flow_pair": "key"},
	"toml":       {"table": "section", "table_array_element": "section", "pair": "key"},
	"json":       {"pair": "key"},
	"html":       {"element": "element"},
	"css":        {"rule_set": "selector"},
}

func localDeclarationKind(language, nodeType string) string {
	if kind := languageDeclarationKinds[language][nodeType]; kind != "" {
		return kind
	}
	return declarationKinds[nodeType]
}

func unquoteDeclaration(text string) string {
	text = strings.TrimSpace(text)
	if unquoted, err := strconv.Unquote(text); err == nil {
		return unquoted
	}
	return strings.Trim(text, "'\"")
}

func localDeclarationName(language string, n *ts.Node, lang *ts.Language, raw []byte) string {
	if n.NamedChildCount() == 0 {
		return ""
	}
	switch language {
	case "elixir":
		if n.Type(lang) == "call" && n.NamedChildCount() > 1 {
			args := n.NamedChild(1)
			if args.NamedChildCount() > 0 {
				name := args.NamedChild(0)
				if name.Type(lang) == "call" && name.NamedChildCount() > 0 {
					name = name.NamedChild(0)
				}
				return name.Text(raw)
			}
		}
	case "clojure":
		if n.NamedChildCount() > 1 {
			return n.NamedChild(1).Text(raw)
		}
	case "r":
		if n.Type(lang) == "binary_operator" {
			return n.NamedChild(0).Text(raw)
		}
	case "erlang":
		if n.Type(lang) == "fun_decl" {
			return n.NamedChild(0).NamedChild(0).Text(raw)
		}
	case "haskell", "ocaml", "perl", "powershell":
		return n.NamedChild(0).Text(raw)
	case "julia":
		if n.Type(lang) == "function_definition" {
			sig := n.NamedChild(0)
			if sig.NamedChildCount() > 0 {
				call := sig.NamedChild(0)
				if call.NamedChildCount() > 0 {
					return call.NamedChild(0).Text(raw)
				}
			}
		}
	case "fsharp":
		if n.Type(lang) == "named_module" {
			return n.NamedChild(0).Text(raw)
		}
		if n.Type(lang) == "function_or_value_defn" {
			left := n.NamedChild(0)
			if left.NamedChildCount() > 0 {
				return left.NamedChild(0).Text(raw)
			}
		}
	case "fortran":
		statement := n.NamedChild(0)
		for i := 0; i < statement.NamedChildCount(); i++ {
			child := statement.NamedChild(i)
			if child.Type(lang) == "name" {
				return child.Text(raw)
			}
		}
	case "proto":
		return n.NamedChild(0).Text(raw)
	case "hcl":
		if n.Type(lang) == "block" {
			parts := []string{}
			for i := 0; i < n.NamedChildCount(); i++ {
				c := n.NamedChild(i)
				if c.Type(lang) != "identifier" && c.Type(lang) != "string_lit" {
					break
				}
				parts = append(parts, unquoteDeclaration(c.Text(raw)))
			}
			return strings.Join(parts, ".")
		}
		if n.Type(lang) == "attribute" {
			return n.NamedChild(0).Text(raw)
		}
	case "yaml":
		if n.Type(lang) == "block_mapping_pair" || n.Type(lang) == "flow_pair" {
			// Complex YAML keys remain text-searchable; do not invent a symbol name.
			key := n.NamedChild(0)
			if !strings.ContainsAny(key.Text(raw), "\n\r{}[]") {
				return unquoteDeclaration(key.Text(raw))
			}
		}
	case "toml", "json":
		return unquoteDeclaration(n.NamedChild(0).Text(raw))
	case "css":
		if n.Type(lang) == "rule_set" {
			return n.NamedChild(0).Text(raw)
		}
	case "html":
		if n.Type(lang) == "element" {
			start := n.NamedChild(0)
			if id := markupAttribute(start, lang, raw, "id"); id != "" {
				return "#" + id
			}
		}
	}
	return ""
}

// Dart represents the signature and body as siblings. Include the body so
// read_symbol and index chunks contain the implementation rather than just a name.
func declarationEnd(language string, n *ts.Node, lang *ts.Language) *ts.Node {
	if language != "dart" || n.Type(lang) != "function_signature" && n.Type(lang) != "constructor_signature" {
		return n
	}
	container := n
	if p := n.Parent(); p != nil && p.Type(lang) == "method_signature" {
		container = p
	}
	if parent := container.Parent(); parent != nil {
		for i := 0; i+1 < parent.NamedChildCount(); i++ {
			child := parent.NamedChild(i)
			if child.StartByte() == container.StartByte() && child.EndByte() == container.EndByte() {
				if next := parent.NamedChild(i + 1); next.Type(lang) == "function_body" {
					return next
				}
				break
			}
		}
	}
	return n
}

func markupAttribute(start *ts.Node, lang *ts.Language, raw []byte, name string) string {
	for i := 0; i < start.NamedChildCount(); i++ {
		a := start.NamedChild(i)
		if a.Type(lang) != "attribute" || a.NamedChildCount() < 2 {
			continue
		}
		if strings.EqualFold(a.NamedChild(0).Text(raw), name) {
			return unquoteDeclaration(a.NamedChild(a.NamedChildCount() - 1).Text(raw))
		}
	}
	return ""
}

func embeddedScriptOutline(ctx context.Context, n *ts.Node, lang *ts.Language, raw []byte) ([]codeSymbol, error) {
	language := "javascript"
	for i := 0; i < n.NamedChildCount(); i++ {
		child := n.NamedChild(i)
		if child.Type(lang) != "start_tag" {
			continue
		}
		switch strings.ToLower(markupAttribute(child, lang, raw, "lang")) {
		case "ts", "typescript":
			language = "typescript"
		case "", "js", "javascript":
		default:
			return nil, nil // Unknown embedded languages retain the text fallback.
		}
		if typ := markupAttribute(child, lang, raw, "type"); typ != "" && typ != "module" && typ != "text/javascript" && typ != "application/javascript" {
			return nil, nil
		}
	}
	for i := 0; i < n.NamedChildCount(); i++ {
		child := n.NamedChild(i)
		if child.Type(lang) != "raw_text" {
			continue
		}
		symbols, err := localOutlineLanguage(ctx, language, "script", []byte(child.Text(raw)))
		if err != nil {
			return nil, err
		}
		for i := range symbols {
			symbols[i].Start += int(child.StartPoint().Row)
			symbols[i].End += int(child.StartPoint().Row)
		}
		return symbols, nil
	}
	return nil, nil
}

// Expression-shaped declarations are filtered by syntax, so ordinary calls,
// anonymous functions and arbitrary lists do not become false declarations.
func declarationKindForNode(language string, n *ts.Node, lang *ts.Language, raw []byte) string {
	switch language {
	case "elixir":
		if n.Type(lang) != "call" || n.NamedChildCount() < 2 {
			return ""
		}
		switch n.NamedChild(0).Text(raw) {
		case "defmodule", "defprotocol", "defimpl":
			return "module"
		case "def", "defp", "defmacro", "defmacrop":
			return "function"
		}
		return ""
	case "clojure":
		if n.Type(lang) != "list_lit" || n.NamedChildCount() < 2 {
			return ""
		}
		switch n.NamedChild(0).Text(raw) {
		case "defn", "defn-", "defmacro":
			return "function"
		case "def":
			return "variable"
		case "defrecord", "deftype":
			return "type"
		}
		return ""
	case "r":
		if n.Type(lang) != "binary_operator" || n.NamedChildCount() < 2 {
			return ""
		}
		lhs, rhs := n.NamedChild(0), n.NamedChild(1)
		op := strings.TrimSpace(string(raw[lhs.EndByte():rhs.StartByte()]))
		if lhs.Type(lang) == "identifier" && rhs.Type(lang) == "function_definition" && (op == "<-" || op == "=") {
			return "function"
		}
		return ""
	}
	return localDeclarationKind(language, n.Type(lang))
}
