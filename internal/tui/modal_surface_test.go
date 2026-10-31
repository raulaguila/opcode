package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/stretchr/testify/require"
)

func TestModalAttenuationKeepsGeometrySelectionAndColorFreeAttributes(t *testing.T) {
	old := theme.ColorProfile()
	t.Cleanup(func() { theme.SetColorProfile(old) })
	content := "\x1b[1;38;2;7;20;30mHeading\x1b[0m\n\x1b[7;38;2;200;40;50mselected\x1b[0m 漢字"
	for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.Ascii, colorprofile.NoTTY} {
		theme.SetColorProfile(profile)
		dim := attenuateModalBackground(content)
		require.Equal(t, strings.Fields(ansi.Strip(content)), strings.Fields(ansi.Strip(dim)))
		require.Equal(t, lipgloss.Width(content), lipgloss.Width(dim))
		require.Equal(t, lipgloss.Height(content), lipgloss.Height(dim))
		if profile != colorprofile.NoTTY {
			require.Contains(t, dim, "\x1b[7m", "selection survives; RGB channel 7 must not become a selection")
		}
		if profile == colorprofile.Ascii {
			output := styles.TerminalOutput(dim)
			require.Contains(t, output, "\x1b[2m")
			require.Contains(t, output, "7m")
			require.NotContains(t, output, "38;")
			require.NotContains(t, output, "48;")
		}
	}
	plain := modalBackgroundText(content)
	require.Equal(t, 1, strings.Count(plain, "\x1b[7m"))
}
