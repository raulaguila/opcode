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
	"ruby":   {"class": "class", "module": "module", "method": "method", "singleton_method": "method"},
	"kotlin": {"object_declaration": "object", "type_alias": "type"},
	"dart":   {"function_signature": "function", "constructor_signature": "constructor", "enum_declaration": "enum"},
	"sql":    {"create_table_statement": "table", "create_view_statement": "view", "create_function_statement": "function", "create_index_statement": "index"},
	"hcl":    {"block": "block", "attribute": "attribute"},
	"yaml":   {"block_mapping_pair": "key", "flow_pair": "key"},
	"toml":   {"table": "section", "table_array_element": "section", "pair": "key"},
	"json":   {"pair": "key"},
	"html":   {"element": "element"},
	"css":    {"rule_set": "selector"},
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
