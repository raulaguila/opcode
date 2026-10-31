package styles

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/stretchr/testify/require"
)

func TestMarkdownProposalRendersHeadingsWithoutSourceMarkers(t *testing.T) {
	name, profile, dark := theme.CurrentThemeName(), theme.ColorProfile(), theme.HasDarkBackground()
	t.Cleanup(func() {
		_ = theme.PreviewTheme(name)
		theme.SetColorProfile(profile)
		theme.SetHasDarkBackground(dark)
	})
	require.NoError(t, theme.PreviewTheme("opcode"))
	for _, color := range []colorprofile.Profile{colorprofile.Ascii, colorprofile.TrueColor} {
		theme.SetColorProfile(color)
		for _, background := range []bool{false, true} {
			theme.SetHasDarkBackground(background)
			for level := 1; level <= 6; level++ {
				source := strings.Repeat("#", level) + " Fluxo de permissões\n\n**Texto importante** e `Request()`.\n\n```text\n## literal code\n```"
				view := ansi.Strip(RenderMarkdownBlocks(source, 100, 100))
				require.Contains(t, view, "Fluxo de permissões")
				require.NotContains(t, view, "# Fluxo")
				require.Contains(t, view, "Texto importante")
				require.Contains(t, view, "Request()")
				require.Contains(t, view, "## literal code", "code is not interpreted as a heading")
			}
		}
	}
}
