package styles

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	markdownansi "charm.land/glamour/v2/ansi"
	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

func TestMarkdownCodeNoWrapEligibility(t *testing.T) {
	for _, tc := range []struct {
		source string
		width  int
		want   bool
	}{
		{"short\n", 5, true}, {"ação 漢字\n", 9, true}, {"longer\n", 5, false},
		{"\tshort\n", 40, false}, {"x\x1b[0m\n", 40, false}, {"x\r\n", 40, false},
		{"x\x00\n", 40, false}, {"x\xff\n", 40, false}, {"\n", 1, true},
	} {
		source := []byte(tc.source)
		node := ast.NewCodeBlock()
		node.Lines().Append(text.NewSegment(0, len(source)))
		require.Equal(t, tc.want, markdownCodeFitsWithoutWrap(node, source, tc.width), "source %q width %d", tc.source, tc.width)
	}
}

func TestMarkdownCodeBulkPaddingMatchesNativeCells(t *testing.T) {
	profile, dark := theme.ColorProfile(), theme.HasDarkBackground()
	t.Cleanup(func() { theme.SetColorProfile(profile); theme.SetHasDarkBackground(dark) })
	for _, background := range []bool{false, true} {
		theme.SetHasDarkBackground(background)
		for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.Ascii, colorprofile.NoTTY} {
			theme.SetColorProfile(profile)
			for _, width := range []int{24, 40, 80, 120} {
				for name, source := range markdownCodePaddingFixtures() {
					t.Run(fmt.Sprintf("%s/%d/%v/%t", name, width, profile, background), func(t *testing.T) {
						want := markdownCodePaddingSurface(t, source, width, false)
						got := markdownCodePaddingSurface(t, source, width, true)
						require.Equal(t, ansi.Strip(want), ansi.Strip(got), "code geometry/content")
						require.Equal(t, uv.NewStyledString(want).Lines(ansi.GraphemeWidth), uv.NewStyledString(got).Lines(ansi.GraphemeWidth), "all glyph foreground/background/attributes, including padding")
					})
				}
			}
		}
	}
}

func markdownCodePaddingFixtures() map[string]string {
	return map[string]string{
		"short":       "```go\nfmt.Println(1)\n```",
		"unicode":     "```text\nação 漢字 👩‍💻\n```",
		"punctuation": "```go\n" + strings.Repeat("foo.bar(a,b)+", 30) + "\n```",
		"longword":    "```text\n" + strings.Repeat("a", 240) + "\n```",
		"tabs":        "```go\n\tif x {\n\t\tfmt.Println(1)\n\t}\n```",
		"blankedges":  "```text\n\n  first\n\n\n  last\n\n```",
		"blank":       "```text\n\n\n```", "empty": "```\n```",
		"unclosed": "```go\nfmt.Println(1)",
		"indented": "    first\n\n    second\n",
		"list":     "1. Before.\n\n   ```go\n   fmt.Println(\"ação 漢字\")\n   ```",
		"nested":   "- Outer\n  - Inner\n\n    ```text\n      literal\n\n      next\n    ```",
	}
}

// Render both policies through fresh native renderers so the oracle never
// shares the optimized block renderer, including code nested inside lists.
func markdownCodePaddingSurface(t *testing.T, content string, width int, optimized bool) string {
	t.Helper()
	source := []byte(content)
	doc := goldmark.New().Parser().Parse(text.NewReader(source))
	var node ast.Node
	require.NoError(t, ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering && markdownCodeBlock(n) {
			node = n
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	}))
	require.NotNil(t, node)
	inner := markdownCodeWidth(width)
	fast := optimized && markdownCodeFitsWithoutWrap(node, source, inner)
	wrap := inner
	if fast {
		wrap = 0
	}
	block := ast.NewDocument()
	block.AppendChild(block, node)
	render := renderer.NewRenderer(renderer.WithNodeRenderers(util.Prioritized(markdownansi.NewRenderer(markdownansi.Options{
		WordWrap: wrap, Styles: markdownBlockStyle(generateMarkdownStyleConfig(), true), ChromaFormatter: "terminal16m",
	}), 1000)))
	var out bytes.Buffer
	require.NoError(t, render.Render(&out, source, block))
	view := trimMarkdownBlockPadding(out.String())
	if fast {
		view = padMarkdownCode(view, inner)
	}
	return TerminalOutput(renderMarkdownCodeSurface(preserveMarkdownCodePadding(view, node, source), node, source, width))
}
