package styles

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/hinshun/vt10x"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/stretchr/testify/require"
)

func TestPanelHintContrastAcrossThemesAndBackgrounds(t *testing.T) {
	name, dark := theme.CurrentThemeName(), theme.HasDarkBackground()
	t.Cleanup(func() { _ = theme.PreviewTheme(name); theme.SetHasDarkBackground(dark) })
	for _, name := range theme.AvailableThemes() {
		require.NoError(t, theme.PreviewTheme(name))
		for _, dark := range []bool{false, true} {
			theme.SetHasDarkBackground(dark)
			for _, background := range []theme.AdaptiveColor{theme.CurrentTheme().Background(), theme.CurrentTheme().BackgroundSecondary()} {
				hint := HintOn(background)
				require.GreaterOrEqual(t, panelContrast(hint.GetForeground(), hint.GetBackground()), 4.5, "%s dark=%v", name, dark)
				require.GreaterOrEqual(t, panelContrast(ReadableForeground(theme.CurrentTheme().Text(), background), background), 4.5)
				require.False(t, hint.GetBold())
				require.False(t, hint.GetFaint(), "faint can defeat the contrast guarantee")
				require.False(t, hint.GetItalic())
			}
		}
	}
}

func TestHintOnFallsBackForCustomLowContrastColors(t *testing.T) {
	name, dark := theme.CurrentThemeName(), theme.HasDarkBackground()
	original := theme.GetTheme("opcode")
	t.Cleanup(func() {
		theme.RegisterTheme("opcode", original)
		_ = theme.PreviewTheme(name)
		theme.SetHasDarkBackground(dark)
	})
	// Exercise the defensive fallback without writing or bypassing palette validation.
	custom := theme.NewOpcodeTheme()
	custom.TextColor = theme.AdaptiveColor{Dark: "#777777", Light: "#777777"}
	custom.TextMutedColor = custom.TextColor
	custom.BackgroundColor = custom.TextColor
	theme.RegisterTheme("opcode", custom)
	require.NoError(t, theme.PreviewTheme("opcode"))
	for _, dark := range []bool{false, true} {
		theme.SetHasDarkBackground(dark)
		hint := HintOn(custom.Background())
		require.GreaterOrEqual(t, panelContrast(hint.GetForeground(), hint.GetBackground()), 4.5)
	}
}

func TestPanelHintAttributesAndNoColorRemainReadable(t *testing.T) {
	profile, name := theme.ColorProfile(), theme.CurrentThemeName()
	t.Cleanup(func() { theme.SetColorProfile(profile); _ = theme.PreviewTheme(name) })
	require.NoError(t, theme.PreviewTheme("opcode"))
	for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.Ascii} {
		theme.SetColorProfile(profile)
		view := PanelTitle().Render("Title") + "\n" + PanelHint().Render("Enter open · Esc back") + "\n" + BaseStyle().Render("Body")
		output := TerminalOutput(view)
		require.Equal(t, "Title\nEnter open · Esc back\nBody", ansi.Strip(output))
		screen := vt10x.New(vt10x.WithSize(40, 4))
		_, err := screen.Write([]byte(strings.ReplaceAll(output, "\n", "\r\n")))
		require.NoError(t, err)
		for x := range len([]rune("Enter open · Esc back")) {
			cell := screen.Cell(x, 1)
			require.Zero(t, cell.Mode&23, "legend must not inherit bold, italic, underline, or reverse")
			if profile == colorprofile.Ascii {
				require.Equal(t, vt10x.DefaultFG, cell.FG)
				require.Equal(t, vt10x.DefaultBG, cell.BG)
			}
		}
		require.Zero(t, screen.Cell(0, 2).Mode&4, "legend style must not leak into following body")
	}
}
