package styles

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Fresh renderers with the current block padding policy: exact bytes cover
// renderer state leaking, in addition to the width/citation assertions.
func markdownBlocksReference(content string, width, proseWidth int) string {
	source := []byte(content)
	md := goldmark.New(goldmark.WithExtensions(extension.GFM, extension.DefinitionList), goldmark.WithParserOptions(parser.WithAutoHeadingID()))
	doc := md.Parser().Parse(text.NewReader(source))
	var blocks []string
	for node := doc.FirstChild(); node != nil; {
		next := node.NextSibling()
		wide := false
		_ = ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
			if entering && (n.Kind() == ast.KindFencedCodeBlock || n.Kind() == ast.KindCodeBlock || n.Kind() == extast.KindTable) {
				wide = true
			}
			return ast.WalkContinue, nil
		})
		measure := max(1, min(width, proseWidth))
		if wide {
			measure = max(1, width)
		}
		block := ast.NewDocument()
		block.AppendChild(block, node)
		r := renderer.NewRenderer(renderer.WithNodeRenderers(util.Prioritized(ansi.NewRenderer(ansi.Options{
			WordWrap: measure, Styles: generateMarkdownStyleConfig(), ColorProfile: lipgloss.ColorProfile(),
		}), 1000)))
		var out bytes.Buffer
		if err := r.Render(&out, source, block); err != nil {
			fallback := GetMarkdownRenderer(width)
			result, _ := fallback.Render(content)
			return result
		}
		if rendered := trimMarkdownBlockPadding(out.String()); rendered != "" {
			blocks = append(blocks, rendered)
		}
		node = next
	}
	return strings.Join(blocks, "\n\n")
}

func TestMarkdownRendererReuseMatchesFreshBlockRenderers(t *testing.T) {
	profile, dark := lipgloss.ColorProfile(), lipgloss.HasDarkBackground()
	t.Cleanup(func() { lipgloss.SetColorProfile(profile); lipgloss.SetHasDarkBackground(dark) })
	for _, color := range []termenv.Profile{termenv.Ascii, termenv.TrueColor} {
		lipgloss.SetColorProfile(color)
		for _, background := range []bool{false, true} {
			lipgloss.SetHasDarkBackground(background)
			for _, source := range markdownReuseFixtures() {
				require.Equal(t, markdownBlocksReference(source, 120, 48), RenderMarkdownBlocks(source, 120, 48))
			}
		}
	}
}

func markdownReuseFixtures() []string {
	return []string{
		strings.Repeat("# Heading\n\nProse with [source][ref] and `code`, ação 漢字, [^1].\n\n4. first\n5. second\n\n", 4) + "[ref]: https://example.test/file#L1\n\n[^1]: Citation evidence.\n",
		"> Quote containing a list\n>\n> 1. source\n> 2. source with **bold**\n\n```go\nfmt.Println(\"wide code\")\n```\n\nFurther prose with a [link](https://example.test).\n\n    indented code\n",
		"| First | Second |\n| --- | --- |\n| A | B |\n\nParagraph between tables.\n\n| Third | Fourth |\n| --- | --- |\n| C | D |\n\nAfter tables.\n",
	}
}

func TestMarkdownReuseReadsCurrentThemeForEachCall(t *testing.T) {
	name, profile := theme.CurrentThemeName(), lipgloss.ColorProfile()
	t.Cleanup(func() { _ = theme.PreviewTheme(name); lipgloss.SetColorProfile(profile) })
	lipgloss.SetColorProfile(termenv.TrueColor)
	source := "## Theme\n\nA [source](https://example.test) with **emphasis**.\n"
	for _, name := range []string{"gruvbox", "catppuccin"} {
		require.NoError(t, theme.PreviewTheme(name))
		require.Equal(t, markdownBlocksReference(source, 100, 48), RenderMarkdownBlocks(source, 100, 48))
	}
}
