package styles

import (
	"bytes"
	"regexp"
	"strings"

	"charm.land/glamour/v2/ansi"
	terminalansi "github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/ui/theme"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Goldmark initializes immutable parser tables once; contexts and ASTs are per parse.
var sharedMarkdownParser = goldmark.New(goldmark.WithExtensions(extension.GFM, extension.DefinitionList), goldmark.WithParserOptions(parser.WithAutoHeadingID())).Parser()

// Parse once so reference links, citations and list numbering survive rendering
// at different widths. Containers with code/tables retain their full structure.
// This changes presentation only; stored and copied source remains untouched.
var (
	inlineMath  = regexp.MustCompile(`\$([^$\n]*\\[a-zA-Z]+[^$\n]*)\$`)
	mathCommand = regexp.MustCompile(`\\[a-zA-Z]+`)
	mathSymbols = map[string]string{`\rightarrow`: "→", `\to`: "→", `\leftarrow`: "←", `\leftrightarrow`: "↔", `\Rightarrow`: "⇒", `\Leftarrow`: "⇐", `\implies`: "⇒", `\times`: "×", `\cdot`: "·", `\leq`: "≤", `\le`: "≤", `\geq`: "≥", `\ge`: "≥", `\neq`: "≠", `\ne`: "≠", `\approx`: "≈", `\infty`: "∞", `\pm`: "±", `\checkmark`: "✓"}
)

// plainInlineMath writes $\rightarrow$ as the arrow models mean by it: terminals
// have no math. Code, and math with a command outside the table, stay as typed.
func plainInlineMath(content string) string {
	if !strings.Contains(content, `$\`) && !strings.Contains(content, `\`) {
		return content
	}
	fences := strings.Split(content, "```")
	for i := 0; i < len(fences); i += 2 {
		spans := strings.Split(fences[i], "`")
		for j := 0; j < len(spans); j += 2 {
			spans[j] = inlineMath.ReplaceAllStringFunc(spans[j], func(math string) string {
				known := true
				plain := mathCommand.ReplaceAllStringFunc(math[1:len(math)-1], func(command string) string {
					symbol, ok := mathSymbols[command]
					known = known && ok
					return symbol
				})
				if !known {
					return math
				}
				return strings.TrimSpace(plain)
			})
		}
		fences[i] = strings.Join(spans, "`")
	}
	return strings.Join(fences, "```")
}

func RenderMarkdownBlocks(content string, width, proseWidth int) string {
	return renderMarkdownBlocks(content, width, proseWidth, nil, false)
}

func renderMarkdownBlocks(content string, width, proseWidth int, cache *MarkdownCache, streaming bool) string {
	content = plainInlineMath(content)
	source := []byte(content)
	doc := sharedMarkdownParser.Parse(text.NewReader(source))
	literalHTML(doc, source)
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

// literalHTML shows HTML-looking text as typed. Terminals never render HTML,
// and sanitizing it would erase prose such as "remove <id>" or Vec<T>.
func literalHTML(doc ast.Node, source []byte) {
	var nodes []ast.Node
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering && (node.Kind() == ast.KindRawHTML || node.Kind() == ast.KindHTMLBlock) {
			nodes = append(nodes, node)
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	for _, node := range nodes {
		parent := node.Parent()
		switch n := node.(type) {
		case *ast.RawHTML:
			for i := 0; i < n.Segments.Len(); i++ {
				parent.InsertBefore(parent, node, ast.NewTextSegment(n.Segments.At(i)))
			}
		case *ast.HTMLBlock:
			paragraph := ast.NewParagraph()
			lines := n.Lines()
			for i := 0; i < lines.Len(); i++ {
				segment := lines.At(i)
				line := ast.NewTextSegment(segment.TrimRightSpace(source))
				line.SetSoftLineBreak(i < lines.Len()-1)
				paragraph.AppendChild(paragraph, line)
			}
			if n.HasClosure() {
				paragraph.AppendChild(paragraph, ast.NewTextSegment(n.ClosureLine.TrimRightSpace(source)))
			}
			parent.InsertBefore(parent, node, paragraph)
		}
		parent.RemoveChild(parent, node)
	}
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
	code := markdownCodeBlock(node)
	renderWidth := markdownNodeRenderWidth(node, b.source, measure)
	key := markdownRendererKey{renderWidth, code}
	r, ok := b.renderers[key]
	if !ok {
		if !b.styled {
			b.style = generateMarkdownStyleConfig()
			b.styled = true
		}
		style := markdownBlockStyle(b.style, code)
		r = renderer.NewRenderer(renderer.WithNodeRenderers(util.Prioritized(ansi.NewRenderer(ansi.Options{WordWrap: renderWidth, Styles: style, ChromaFormatter: "terminal16m"}), 1000)))
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
		if renderWidth == 0 {
			view = padMarkdownCode(view, markdownCodeWidth(measure))
		}
		view = preserveMarkdownCodePadding(view, node, b.source)
		view = renderMarkdownCodeSurface(view, node, b.source, measure)
	} else {
		if renderWidth == 0 {
			view = wrapMarkdownProse(view, measure)
		}
		// Restore the surface after wrapping to avoid reflowing expanded SGR codes.
		view = restoreBackground(view, theme.Surface())
	}
	return view, nil
}

func markdownNodeRenderWidth(node ast.Node, source []byte, measure int) int {
	if markdownCodeBlock(node) {
		width := markdownCodeWidth(measure)
		if markdownCodeFitsWithoutWrap(node, source, width) {
			return 0
		}
		return width
	}
	return markdownProseRenderWidth(node, measure)
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
