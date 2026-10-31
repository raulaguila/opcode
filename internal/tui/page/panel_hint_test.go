package page

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/hinshun/vt10x"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/stretchr/testify/require"
)

func panelHintGlyph(t *testing.T, view, text string) vt10x.Glyph {
	t.Helper()
	screen := vt10x.New(vt10x.WithSize(180, 50))
	_, err := screen.Write([]byte(strings.ReplaceAll(styles.TerminalOutput(view), "\n", "\r\n")))
	require.NoError(t, err)
	for y, line := range strings.Split(ansi.Strip(view), "\n") {
		if before, _, found := strings.Cut(line, text); found {
			return screen.Cell(ansi.StringWidth(before), y)
		}
	}
	t.Fatalf("missing hint %q in %s", text, ansi.Strip(view))
	return vt10x.Glyph{}
}

func TestPanelHintsKeepTheirContrastBackgroundInsideTerminalAndQuestion(t *testing.T) {
	profile, dark, name := theme.ColorProfile(), theme.HasDarkBackground(), theme.CurrentThemeName()
	t.Cleanup(func() { theme.SetColorProfile(profile); theme.SetHasDarkBackground(dark); _ = theme.PreviewTheme(name) })
	require.NoError(t, theme.PreviewTheme("opcode"))
	p := workflowPage(t)
	p.SetSize(140, 24)
	p.workflow.question = interaction.Question{ID: "q", Question: "Choose next step", Choices: []string{"First", "Second"}}
	for _, dark := range []bool{false, true} {
		theme.SetHasDarkBackground(dark)
		for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.Ascii} {
			theme.SetColorProfile(profile)
			reference := panelHintGlyph(t, styles.PanelHint().Render("reference"), "reference")
			for text, view := range map[string]string{"Ctrl+P chat": p.terminalView(true), "↑↓ choose": p.questionPanelView(10)} {
				actual := panelHintGlyph(t, view, text)
				require.Equal(t, reference.FG, actual.FG)
				require.Equal(t, reference.BG, actual.BG, "nested panel rendering preserves its contrast-tested background")
				require.Zero(t, actual.Mode&23)
			}
		}
	}
}
