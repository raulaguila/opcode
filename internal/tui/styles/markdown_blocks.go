package styles

import (
	"bytes"
	"strings"

	"charm.land/glamour/v2/ansi"
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
	return renderMarkdownBlocks(content, width, proseWidth, nil, false)
}

func renderMarkdownBlocks(content string, width, proseWidth int, cache *MarkdownCache, streaming bool) string {
	source := []byte(content)
	md := goldmark.New(goldmark.WithExtensions(extension.GFM, extension.DefinitionList), goldmark.WithParserOptions(parser.WithAutoHeadingID()))
	doc := md.Parser().Parse(text.NewReader(source))
	blocks := markdownBlockRenderer{content: content, source: source, width: width, prose: proseWidth, cache: cache, streaming: streaming, renderers: map[markdownRendererKey]renderer.Renderer{}}
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
	return TerminalOutput(strings.Join(output, "\n\n"))
}

type markdownBlockRenderer struct {
	content      string
	source       []byte
	width, prose int
	cache        *MarkdownCache
	style        ansi.StyleConfig
	styled       bool
	streaming    bool
	renderers    map[markdownRendererKey]renderer.Renderer
}

type markdownRendererKey struct {
	width int
	code  bool
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
	if b.streaming {
		if text, pending := pendingStreamingEmphasis(node, b.source); pending {
			return restrainedStreamingText(text, markdownBlockWidth(node, b.width, b.prose)), nil
		}
	}
	if list, ok := node.(*ast.List); ok {
		return b.renderList(list)
	}
	measure := markdownBlockWidth(node, b.width, b.prose)
	renderWidth := markdownProseRenderWidth(node, measure)
	code := markdownCodeBlock(node)
	if code {
		renderWidth = markdownCodeWidth(measure)
	}
	key := markdownRendererKey{renderWidth, code}
	r, ok := b.renderers[key]
	if !ok {
		if !b.styled {
			b.style = generateMarkdownStyleConfig()
			b.styled = true
		}
		style := markdownBlockStyle(b.style, code)
		r = renderer.NewRenderer(renderer.WithNodeRenderers(util.Prioritized(ansi.NewRenderer(ansi.Options{WordWrap: renderWidth, Styles: style}), 1000)))
		b.renderers[key] = r
	}
	block := ast.NewDocument()
	block.AppendChild(block, node)
	var out bytes.Buffer
	if err := r.Render(&out, b.source, block); err != nil {
		return "", err
	}
	view := trimMarkdownBlockPadding(out.String())
	if code {
		view = preserveMarkdownCodePadding(view, node, b.source)
		view = renderMarkdownCodeSurface(view, node, b.source, measure)
	} else if renderWidth == 0 {
		view = wrapMarkdownProse(view, measure)
	}
	return view, nil
}

func markdownProseRenderWidth(node ast.Node, width int) int {
	switch node.Kind() {
	case ast.KindParagraph, ast.KindTextBlock, ast.KindHeading:
		return 0
	default:
		return width
	}
}

// Wrap prose once after styling. Glamour's nested margin wrapping otherwise
// breaks decimal commas and reapplies indentation to an already wrapped line.
func wrapMarkdownProse(view string, width int) string {
	if view == "" {
		return ""
	}
	margin := strings.Repeat(" ", defaultMargin)
	body := removeMarkdownListMargin(view)
	wrapped := terminalansi.Wrap(body, max(1, width-defaultMargin), " ")
	return margin + strings.ReplaceAll(wrapped, "\n", "\n"+margin)
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
