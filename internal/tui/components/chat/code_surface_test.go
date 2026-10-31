package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/hinshun/vt10x"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/stretchr/testify/require"
)

func TestCodeSurfaceSurvivesMessageFrameAndPreservesRawCopy(t *testing.T) {
	name, dark, profile := theme.CurrentThemeName(), theme.HasDarkBackground(), theme.ColorProfile()
	t.Cleanup(func() { _ = theme.PreviewTheme(name); theme.SetHasDarkBackground(dark); theme.SetColorProfile(profile) })
	theme.SetColorProfile(colorprofile.TrueColor)
	const source = "Normal prose.\n\n```bash\nprintf 'ação 漢字'\n```\n\nFollowing prose."
	for _, name := range []string{"catppuccin", "gruvbox"} {
		require.NoError(t, theme.PreviewTheme(name))
		for _, dark := range []bool{false, true} {
			theme.SetHasDarkBackground(dark)
			view := renderMessage(source, false, false, 80)
			screen := vt10x.New(vt10x.WithSize(80, 20))
			_, err := screen.Write([]byte(strings.ReplaceAll(view, "\n", "\r\n")))
			require.NoError(t, err)
			prose := chatCellForText(t, screen, "Normal prose")
			code := chatCellForText(t, screen, "printf")
			following := chatCellForText(t, screen, "Following prose")
			r, g, b, _ := theme.CurrentTheme().Background().RGBA()
			require.Equal(t, vt10x.Color((r>>8)<<16|(g>>8)<<8|(b>>8)), prose.BG)
			r, g, b, _ = theme.CurrentTheme().BackgroundSecondary().RGBA()
			require.Equal(t, vt10x.Color((r>>8)<<16|(g>>8)<<8|(b>>8)), code.BG)
			require.NotEqual(t, prose.BG, code.BG)
			require.Equal(t, prose.BG, following.BG)
		}
	}
	m := renderFixture(0)
	m.messages = []message.Message{{ID: "response", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: source}}}}
	m.renderView()
	require.Equal(t, source, m.LatestResponse())
}

func chatCellForText(t *testing.T, screen vt10x.Terminal, text string) vt10x.Glyph {
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
	t.Fatalf("missing rendered text %q", text)
	return vt10x.Glyph{}
}
