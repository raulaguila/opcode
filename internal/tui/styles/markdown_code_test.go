package styles

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/hinshun/vt10x"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

func TestMarkdownCodeBlocksHaveHeaderAndDistinctSurface(t *testing.T) {
	profile := theme.ColorProfile()
	t.Cleanup(func() { theme.SetColorProfile(profile) })
	theme.SetColorProfile(colorprofile.TrueColor)
	const source = "Ordinary prose.\n\n```bash\nprintf 'ação 漢字'\n```\n\nMore prose."
	view := RenderMarkdownBlocks(source, 80, 60)
	plain := ansi.Strip(view)
	require.Contains(t, plain, "bash")
	require.Contains(t, plain, "│")
	require.Contains(t, plain, "printf 'ação 漢字'")
	screen := vt10x.New(vt10x.WithSize(80, 20))
	_, err := screen.Write([]byte(strings.ReplaceAll(view, "\n", "\r\n")))
	require.NoError(t, err)
	prose := markdownCellForText(t, screen, "Ordinary")
	code := markdownCellForText(t, screen, "printf")
	require.NotEqual(t, prose.BG, code.BG)
	for _, p := range []colorprofile.Profile{colorprofile.Ascii, colorprofile.NoTTY} {
		theme.SetColorProfile(p)
		plain = ansi.Strip(RenderMarkdownBlocks(source, 80, 60))
		require.Contains(t, plain, "bash")
		require.Contains(t, plain, "│")
	}
}

func TestMarkdownUnknownAndIndentedCodeUseNeutralHeader(t *testing.T) {
	for _, source := range []string{"```\n  literal\n```", "    literal\n"} {
		view := ansi.Strip(RenderMarkdownBlocks(source, 60, 40))
		require.Contains(t, view, "Code")
		require.Contains(t, view, "literal")
		require.NotContains(t, view, "bash")
	}
}

func TestMarkdownCodeLanguageIsOneBoundedPlainToken(t *testing.T) {
	for _, raw := range []string{"bash\tmetadata", "bash\x1b[2J", "bash\nFAKE HEADER", strings.Repeat("x", 100), "\x1b[2J"} {
		source := []byte(raw)
		node := ast.NewFencedCodeBlock(ast.NewTextSegment(text.NewSegment(0, len(source))))
		label := markdownCodeLanguage(node, source)
		require.Equal(t, ansi.Strip(label), label)
		require.NotContains(t, label, "\n")
		require.LessOrEqual(t, ansi.StringWidth(label), 32)
		require.NotContains(t, label, "metadata")
		require.NotContains(t, label, "FAKE")
	}
}

func markdownCellForText(t *testing.T, screen vt10x.Terminal, text string) vt10x.Glyph {
	t.Helper()
	cols, rows := screen.Size()
	for y := range rows {
		var line strings.Builder
		for x := range cols {
			line.WriteRune(screen.Cell(x, y).Char)
		}
		if i := strings.Index(line.String(), text); i >= 0 {
			return screen.Cell(len([]rune(line.String()[:i])), y)
		}
	}
	t.Fatalf("text %q missing from rendered code surface", text)
	return vt10x.Glyph{}
}

// Source-whitespace assertions exclude only the code card's header/left padding.
func markdownWithoutCodeChrome(view string) string {
	var lines []string
	inCode := false
	for line := range strings.SplitSeq(view, "\n") {
		if i := strings.Index(line, "│"); i >= 0 {
			if !inCode {
				inCode = true
				continue
			}
			line = strings.TrimPrefix(line[i+len("│"):], " ")
		} else {
			inCode = false
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
