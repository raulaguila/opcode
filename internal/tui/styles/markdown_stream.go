package styles

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/yuin/goldmark/ast"
)

// Only an unfinished leading emphasis marker in the current tail is provisional.
// Fences, inline code and completed blocks keep their normal Markdown meaning.
func pendingStreamingEmphasis(node ast.Node, source []byte) (string, bool) {
	if node.NextSibling() != nil || node.Lines().Len() == 0 {
		return "", false
	}
	switch node.Kind() {
	case ast.KindParagraph, ast.KindTextBlock, ast.KindHeading:
	default:
		return "", false
	}
	lines := node.Lines()
	text := strings.TrimSpace(string(source[lines.At(0).Start:lines.At(lines.Len()-1).Stop]))
	for _, marker := range []string{"**", "__"} {
		if strings.HasPrefix(text, marker) && !strings.Contains(text[len(marker):], marker) {
			return strings.TrimPrefix(text, marker), true
		}
	}
	return "", false
}

func restrainedStreamingText(text string, width int) string {
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, ansi.Strip(text))
	if text == "" {
		return ""
	}
	margin := strings.Repeat(" ", defaultMargin)
	wrapped := ansi.Wrap(text, max(1, width-defaultMargin), " ")
	return BaseStyle().Foreground(theme.CurrentTheme().TextMuted()).Render(margin + strings.ReplaceAll(wrapped, "\n", "\n"+margin))
}
