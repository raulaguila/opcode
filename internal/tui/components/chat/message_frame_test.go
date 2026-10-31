package chat

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/hinshun/vt10x"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/stretchr/testify/require"
)

func TestMessageFrameFastPathPreservesEveryRenderedCell(t *testing.T) {
	if cfg := config.Get(); cfg != nil {
		previous := cfg.TUI.ProseWidth
		cfg.TUI.ProseWidth = 0
		t.Cleanup(func() { cfg.TUI.ProseWidth = previous })
	}
	profile, dark, name := theme.ColorProfile(), theme.HasDarkBackground(), theme.CurrentThemeName()
	t.Cleanup(func() {
		theme.SetColorProfile(profile)
		theme.SetHasDarkBackground(dark)
		_ = theme.DiscardCustomPreview(name)
	})
	for _, name := range []string{"opcode", "tokyonight", "onedark", "custom"} {
		if name == "custom" {
			palette, err := theme.PaletteFor("opcode")
			require.NoError(t, err)
			palette.Background = theme.AdaptiveColor{Dark: "#101020", Light: "#ffffef"}
			require.NoError(t, theme.PreviewCustomPalette(palette))
		} else {
			require.NoError(t, theme.PreviewTheme(name))
		}
		for _, dark := range []bool{false, true} {
			theme.SetHasDarkBackground(dark)
			for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.Ascii} {
				theme.SetColorProfile(profile)
				for _, width := range []int{12, 40, 120} {
					t.Run(fmt.Sprintf("%s/dark=%v/profile=%d/width=%d", name, dark, profile, width), func(t *testing.T) {
						assertMessageFrameCells(t, width)
					})
				}
			}
		}
	}
}

func assertMessageFrameCells(t *testing.T, width int) {
	t.Helper()
	palette := theme.CurrentTheme()
	for _, source := range []string{
		"Plain text with **strong**, `code`, and after.\n\nLast paragraph.",
		"1. **List** with `file.go`.\n   - Nested ação 漢字.\n\n```go\nif ready { fmt.Println(\"ok\") }\n```\n\nAfter code.",
		"## Heading\n\n" + strings.Repeat("Wide prose wraps with spaces. ", 8) + "\n\n| A | B |\n|---|---|\n| x | y |",
		"An [explicit source](https://example.invalid/path) and wrapped **" + strings.Repeat("strong text ", 10) + "** remain linked and styled.",
	} {
		markdown := strings.TrimSuffix(styles.RenderMarkdownBlocks(source, max(1, width-1), max(1, width-1)), "\n")
		for _, info := range [][]string{nil, {"model name"}, {"model\nsecond line", styles.PanelHint().Render("metadata")}, {""}} {
			parts := append([]string{markdown}, info...)
			for _, user := range []bool{false, true} {
				pipe := palette.Primary()
				if user {
					pipe = palette.Secondary()
				}
				frame := styles.BaseStyle().Width(width).BorderLeft(true).Foreground(palette.TextMuted()).BorderForeground(pipe).BorderStyle(lipgloss.NormalBorder())
				before := frame.Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
				after := renderMessage(source, user, false, width, info...)
				require.Equal(t, lipgloss.Width(before), lipgloss.Width(after))
				require.Equal(t, lipgloss.Height(before), lipgloss.Height(after))
				assertMessageTerminalCells(t, before, after)
			}
		}
	}
}

func TestMessageFrameFastPathDefersSpecialNativeLayout(t *testing.T) {
	for _, tc := range []struct {
		text  string
		width int
	}{{"plain", 1}, {"tab\tcolumn", 40}, {"line\rreturn", 40}, {"bell\a", 40}, {"漢", 2}} {
		_, ok := renderLaidOutMessage(tc.text, tc.width, theme.CurrentTheme().Primary())
		require.False(t, ok, "special layout must use the native frame")
	}
}

func assertMessageTerminalCells(t *testing.T, before, after string) {
	t.Helper()
	width, height := max(1, lipgloss.Width(before)+6), lipgloss.Height(before)+3
	a := vt10x.New(vt10x.WithSize(width, height))
	b := vt10x.New(vt10x.WithSize(width, height))
	_, err := a.Write([]byte(styles.TerminalOutput(strings.ReplaceAll(before, "\n", "\r\n") + "\r\nPROBE")))
	require.NoError(t, err)
	_, err = b.Write([]byte(styles.TerminalOutput(strings.ReplaceAll(after, "\n", "\r\n") + "\r\nPROBE")))
	require.NoError(t, err)
	for y := range height {
		for x := range width {
			if want, got := a.Cell(x, y), b.Cell(x, y); want != got {
				t.Fatalf("cell (%d,%d): reference=%+v newline=%+v", x, y, want, got)
			}
		}
	}
}
