package styles

import (
	"strings"
	"unicode"

	markdownansi "charm.land/glamour/v2/ansi"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/yuin/goldmark/ast"
)

func markdownCodeBlock(node ast.Node) bool {
	return node.Kind() == ast.KindCodeBlock || node.Kind() == ast.KindFencedCodeBlock
}

func markdownCodeFrame(width int) lipgloss.Style {
	t := theme.CurrentTheme()
	return BaseStyle().Background(t.BackgroundSecondary()).BorderLeft(true).
		BorderStyle(lipgloss.NormalBorder()).BorderForeground(t.BorderNormal()).
		Padding(0, 1).Width(max(1, width-defaultMargin))
}

func markdownCodeWidth(width int) int {
	frame := markdownCodeFrame(width)
	return max(1, width-defaultMargin-frame.GetHorizontalFrameSize())
}

func markdownBlockStyle(style markdownansi.StyleConfig, code bool) markdownansi.StyleConfig {
	if code {
		style.Document.Margin = uintPtr(0)
		style.CodeBlock.Margin = uintPtr(0)
		style.CodeBlock.Prefix = ""
	}
	return style
}

func renderMarkdownCodeSurface(view string, node ast.Node, source []byte, width int) string {
	t := theme.CurrentTheme()
	frame := markdownCodeFrame(width)
	label := markdownCodeLanguage(node, source)
	header := BaseStyle().Background(t.BackgroundSecondary()).Foreground(t.TextMuted()).
		Render(ansi.Truncate(label, max(1, width-defaultMargin-frame.GetHorizontalFrameSize()), "…"))
	body := ForceReplaceBackgroundWithLipgloss(view, t.BackgroundSecondary())
	surface := frame.Render(header + "\n" + body)
	margin := strings.Repeat(" ", defaultMargin)
	return margin + strings.ReplaceAll(surface, "\n", "\n"+margin)
}

func markdownCodeLanguage(node ast.Node, source []byte) string {
	if fenced, ok := node.(*ast.FencedCodeBlock); ok {
		language := strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return ' '
			}
			return r
		}, ansi.Strip(string(fenced.Language(source))))
		if words := strings.Fields(language); len(words) > 0 {
			return ansi.Truncate(words[0], 32, "…")
		}
	}
	return config.Text("Código", "Code")
}
