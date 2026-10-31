package styles

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/hinshun/vt10x"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/stretchr/testify/require"
)

func TestMarkdownPreservesHeadingAndInlineAttributesWithoutLeaking(t *testing.T) {
	profile := theme.ColorProfile()
	t.Cleanup(func() { theme.SetColorProfile(profile) })
	theme.SetColorProfile(colorprofile.TrueColor)
	source := "## Diagnostico\n\nO projeto coleta sensores em SQLite.\n\n- Corpo simples com **forte** e final normal.\n\nUltimo paragrafo."
	var cache MarkdownCache
	cache.Render("## Diagnostico\n\nO projeto coleta sensores em SQLite.\n\n", 100, 100)
	for _, view := range []string{RenderMarkdownBlocks(source, 100, 100), cache.Render(source, 100, 100), ForceReplaceBackgroundWithLipgloss(RenderMarkdownBlocks(source, 100, 100), theme.CurrentTheme().Background())} {
		screen := vt10x.New(vt10x.WithSize(100, 30))
		_, err := screen.Write([]byte(strings.ReplaceAll(view, "\n", "\r\n")))
		require.NoError(t, err)
		for text, bold := range map[string]bool{"Diagnostico": true, "O projeto coleta sensores": false, "Corpo simples": false, "forte": true, "final normal": false, "Ultimo paragrafo": false} {
			assertMarkdownBold(t, screen, text, bold)
		}
	}
}

func TestProposalMarkdownBodyStaysRegularAfterBackgroundReplacement(t *testing.T) {
	profile := theme.ColorProfile()
	t.Cleanup(func() { theme.SetColorProfile(profile) })
	theme.SetColorProfile(colorprofile.TrueColor)
	const source = "## DIAGNOSIS: Fluxo de dados\n\nO projeto coleta sensores em SQLite.\n\n1. **Coleta de sensores**\n   O projeto reúne dados de sensores para armazenamento em SQLite, conforme a descrição que foi lida no repositório.\n\n   `README.md:1-2`\n\n2. **Armazenamento**\n   SQLite é o armazenamento descrito no projeto.\n\n   `README.md:1-2`\n\n### Recomendação\n\nRevise o diagnóstico antes de decidir o próximo passo."
	view := ForceReplaceBackgroundWithLipgloss(RenderMarkdownBlocks(source, 120, 120), theme.CurrentTheme().Background())
	screen := vt10x.New(vt10x.WithSize(120, 40))
	_, err := screen.Write([]byte(strings.ReplaceAll(view, "\n", "\r\n")))
	require.NoError(t, err)
	for text, bold := range map[string]bool{"DIAGNOSIS: Fluxo de dados": true, "Coleta de sensores": true, "Armazenamento": true, "Recomendação": true, "O projeto coleta sensores": false, "O projeto reúne dados": false, "SQLite é o armazenamento": false, "Revise o diagnóstico": false} {
		assertMarkdownBold(t, screen, text, bold)
	}
}

func assertMarkdownBold(t *testing.T, screen vt10x.Terminal, text string, bold bool) {
	t.Helper()
	cols, rows := screen.Size()
	for y := range rows {
		var line strings.Builder
		for x := range cols {
			line.WriteRune(screen.Cell(x, y).Char)
		}
		if index := strings.Index(line.String(), text); index >= 0 {
			start := len([]rune(line.String()[:index]))
			for x := range len([]rune(text)) {
				require.Equal(t, bold, screen.Cell(start+x, y).Mode&4 != 0, "text %q column %d", text, x)
			}
			return
		}
	}
	t.Fatalf("rendered terminal text %q was not found", text)
}

func assertMarkdownDefaultColors(t *testing.T, view string) {
	t.Helper()
	screen := vt10x.New(vt10x.WithSize(140, 80))
	_, err := screen.Write([]byte(strings.ReplaceAll(view, "\n", "\r\n")))
	require.NoError(t, err)
	cols, rows := screen.Size()
	for y := range rows {
		for x := range cols {
			cell := screen.Cell(x, y)
			require.Equal(t, vt10x.DefaultFG, cell.FG, "NO_COLOR foreground at %d,%d", x, y)
			require.Equal(t, vt10x.DefaultBG, cell.BG, "NO_COLOR background at %d,%d", x, y)
		}
	}
}

func TestBackgroundRewritePreservesImplicitResets(t *testing.T) {
	for _, reset := range []string{"\x1b[m", "\x1b[;31m", "\x1b[1;;31m", "\x1b[1;m"} {
		view := ForceReplaceBackgroundWithLipgloss("\x1b[1mbefore"+reset+"normal", theme.CurrentTheme().Background())
		screen := vt10x.New(vt10x.WithSize(30, 2))
		_, err := screen.Write([]byte(view))
		require.NoError(t, err)
		assertMarkdownBold(t, screen, "before", true)
		assertMarkdownBold(t, screen, "normal", false)
	}
}
