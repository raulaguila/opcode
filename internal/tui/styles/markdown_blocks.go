package styles

import (
	"bytes"
	"strings"

	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/lipgloss"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Parse once so reference links, citations and list numbering survive rendering
// at different widths. Containers with code/tables retain their full structure.
// This changes presentation only; stored and copied source remains untouched.
func RenderMarkdownBlocks(content string, width, proseWidth int) string {
	source := []byte(content)
	md := goldmark.New(goldmark.WithExtensions(extension.GFM, extension.DefinitionList), goldmark.WithParserOptions(parser.WithAutoHeadingID()))
	doc := md.Parser().Parse(text.NewReader(source))
	style := generateMarkdownStyleConfig()
	renderers := make(map[int]renderer.Renderer, 2)
	var blocks []string
	for node := doc.FirstChild(); node != nil; {
		next := node.NextSibling()
		measure := markdownBlockWidth(node, width, proseWidth)
		r, ok := renderers[measure]
		if !ok {
			r = renderer.NewRenderer(renderer.WithNodeRenderers(util.Prioritized(ansi.NewRenderer(ansi.Options{
				WordWrap: measure, Styles: style, ColorProfile: lipgloss.ColorProfile(),
			}), 1000)))
			renderers[measure] = r
		}
		block := ast.NewDocument()
		block.AppendChild(block, node)
		var out bytes.Buffer
		if err := r.Render(&out, source, block); err != nil {
			fallback := GetMarkdownRenderer(width)
			result, _ := fallback.Render(content)
			return result
		}
		if rendered := strings.Trim(out.String(), "\n"); rendered != "" {
			blocks = append(blocks, rendered)
		}
		node = next
	}
	return strings.Join(blocks, "\n\n")
}

func markdownBlockWidth(node ast.Node, width, proseWidth int) int {
	wide := false
	_ = ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering && (n.Kind() == ast.KindFencedCodeBlock || n.Kind() == ast.KindCodeBlock || n.Kind() == extast.KindTable) {
			wide = true
		}
		return ast.WalkContinue, nil
	})
	if wide {
		return max(1, width)
	}
	return max(1, min(width, proseWidth))
}
