package styles

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/stretchr/testify/require"
)

func TestTerminalOutputPreservesTextAndAppliesCapabilityProfile(t *testing.T) {
	previous := theme.ColorProfile()
	t.Cleanup(func() { theme.SetColorProfile(previous) })
	const text = "ação 漢字 README.md:1-2"
	value := "\x1b[1;7;38;2;140;197;207m" + text + "\x1b[0m"
	for _, profile := range []colorprofile.Profile{colorprofile.NoTTY, colorprofile.Ascii, colorprofile.ANSI, colorprofile.ANSI256, colorprofile.TrueColor} {
		theme.SetColorProfile(profile)
		view := TerminalOutput(value)
		require.Equal(t, text, ansi.Strip(view))
		if profile <= colorprofile.NoTTY {
			require.Equal(t, text, view)
		} else if profile == colorprofile.Ascii {
			require.Contains(t, view, "\x1b[1;7m", "NO_COLOR retains bold and reverse-video selection")
			require.NotContains(t, view, "38;", "NO_COLOR removes color without removing semantic attributes")
		} else if profile == colorprofile.TrueColor {
			require.Equal(t, value, view)
		} else {
			require.False(t, strings.Contains(view, "38;2;"), "colors must be downsampled")
		}
	}
}
