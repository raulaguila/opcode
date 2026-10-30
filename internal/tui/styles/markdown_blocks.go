package styles

import (
	"bytes"
	"strings"

	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/lipgloss"
	terminalansi "github.com/charmbracelet/x/ansi"
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
	return renderMarkdownBlocks(content, width, proseWidth, nil)
}

func renderMarkdownBlocks(content string, width, proseWidth int, cache *MarkdownCache) string {
	source := []byte(content)
	md := goldmark.New(goldmark.WithExtensions(extension.GFM, extension.DefinitionList), goldmark.WithParserOptions(parser.WithAutoHeadingID()))
	doc := md.Parser().Parse(text.NewReader(source))
	blocks := markdownBlockRenderer{content: content, source: source, width: width, prose: proseWidth, cache: cache, renderers: map[int]renderer.Renderer{}}
	var output []string
	for node := doc.FirstChild(); node != nil; {
		next := node.NextSibling()
		rendered, err := blocks.render(node)
		if err != nil {
			fallback := GetMarkdownRenderer(width)
			result, _ := fallback.Render(content)
			return result
		}
		if rendered != "" {
			output = append(output, rendered)
		}
		node = next
	}
	return strings.Join(output, "\n\n")
}

type markdownBlockRenderer struct {
	content      string
	source       []byte
	width, prose int
	cache        *MarkdownCache
	style        ansi.StyleConfig
	styled       bool
	renderers    map[int]renderer.Renderer
}

func (b *markdownBlockRenderer) render(node ast.Node) (string, error) {
	key, stable := "", false
	if b.cache != nil {
		key, stable = stableMarkdownKey(node, b.content)
	}
	if stable {
		if rendered, ok := b.cache.entries[key]; ok {
			return rendered, nil
		}
	}
	rendered, err := b.renderFresh(node)
	if err == nil && rendered != "" && stable {
		b.cache.store(key, rendered)
	}
	return rendered, err
}
func (b *markdownBlockRenderer) renderFresh(node ast.Node) (string, error) {
	measure := markdownBlockWidth(node, b.width, b.prose)
	r, ok := b.renderers[measure]
	if !ok {
		if !b.styled {
			b.style = generateMarkdownStyleConfig()
			b.styled = true
		}
		r = renderer.NewRenderer(renderer.WithNodeRenderers(util.Prioritized(ansi.NewRenderer(ansi.Options{WordWrap: measure, Styles: b.style, ColorProfile: lipgloss.ColorProfile()}), 1000)))
		b.renderers[measure] = r
	}
	block := ast.NewDocument()
	block.AppendChild(block, node)
	var out bytes.Buffer
	if err := r.Render(&out, b.source, block); err != nil {
		return "", err
	}
	return trimMarkdownBlockPadding(out.String()), nil
}

// Glamour's padding may contain spaces or color resets, not just newlines.
// Only block boundaries are trimmed; blank lines inside code/lists stay intact.
func trimMarkdownBlockPadding(rendered string) string {
	for rendered != "" {
		line, rest, found := strings.Cut(rendered, "\n")
		if strings.TrimSpace(terminalansi.Strip(line)) != "" {
			break
		}
		rendered = rest
		if !found {
			return ""
		}
	}
	for rendered != "" {
		end := strings.LastIndexByte(rendered, '\n')
		if strings.TrimSpace(terminalansi.Strip(rendered[end+1:])) != "" {
			break
		}
		if end < 0 {
			return ""
		}
		rendered = rendered[:end]
	}
	return rendered
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
