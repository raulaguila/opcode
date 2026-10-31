package core

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/hinshun/vt10x"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/stretchr/testify/require"
)

func footerGlyph(t *testing.T, view, text string) vt10x.Glyph {
	t.Helper()
	before, _, found := strings.Cut(ansi.Strip(view), text)
	require.True(t, found, "missing text %q", text)
	screen := vt10x.New(vt10x.WithSize(180, 2))
	_, err := screen.Write([]byte(styles.TerminalOutput(view)))
	require.NoError(t, err)
	return screen.Cell(ansi.StringWidth(before), 0)
}

func TestFooterHintKeepsPanelBackgroundAndSemanticWarnings(t *testing.T) {
	profile, dark, name := theme.ColorProfile(), theme.HasDarkBackground(), theme.CurrentThemeName()
	t.Cleanup(func() { theme.SetColorProfile(profile); theme.SetHasDarkBackground(dark); _ = theme.PreviewTheme(name) })
	require.NoError(t, theme.PreviewTheme("opcode"))
	for _, dark := range []bool{false, true} {
		theme.SetHasDarkBackground(dark)
		theme.SetColorProfile(colorprofile.TrueColor)
		m := statusCmp{width: 160}
		reference := footerGlyph(t, styles.PanelHint().Render("hint"), "hint")
		actual := footerGlyph(t, m.View(), "/ commands")
		require.Equal(t, reference.FG, actual.FG)
		require.Equal(t, reference.BG, actual.BG, "outer footer rendering preserves the contrast-tested hint background")
		m.info = util.InfoMsg{Type: util.InfoTypeWarn, Msg: "fixture"}
		warning := footerGlyph(t, m.View(), "Warning:")
		want := footerGlyph(t, styles.BaseStyle().Background(theme.CurrentTheme().BackgroundSecondary()).Foreground(theme.CurrentTheme().Warning()).Render("warning"), "warning")
		require.Equal(t, want.FG, warning.FG)
		require.NotEqual(t, reference.FG, warning.FG)
	}
}
