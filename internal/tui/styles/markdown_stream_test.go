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

func TestStreamingLeadingEmphasisIsRestrainedUntilClosed(t *testing.T) {
	previous := theme.ColorProfile()
	t.Cleanup(func() { theme.SetColorProfile(previous) })
	theme.SetColorProfile(colorprofile.TrueColor)
	var cache MarkdownCache
	const prefix = "Earlier evidence README.md:1-2.\n\n"
	text := prefix + "**"
	for _, r := range "Recomendação ação 漢字" {
		text += string(r)
		view := cache.RenderStreaming(text, 80, 80)
		require.Contains(t, ansi.Strip(view), "README.md:1-2")
		require.NotContains(t, ansi.Strip(view), "**")
	}
	screen := vt10x.New(vt10x.WithSize(80, 12))
	_, err := screen.Write([]byte(strings.ReplaceAll(cache.RenderStreaming(text, 80, 80), "\n", "\r\n")))
	require.NoError(t, err)
	assertMarkdownBold(t, screen, "Recomendação", false)
	text += "**"
	view := cache.RenderStreaming(text, 80, 80)
	require.Equal(t, RenderMarkdownBlocks(text, 80, 80), view)
	screen = vt10x.New(vt10x.WithSize(80, 12))
	_, err = screen.Write([]byte(strings.ReplaceAll(view, "\n", "\r\n")))
	require.NoError(t, err)
	assertMarkdownBold(t, screen, "Recomendação", true)
}

func TestStreamingDoesNotRepairFinalMarkdownOrFencedSource(t *testing.T) {
	var cache MarkdownCache
	const unfinished = "**Recomendação"
	require.NotContains(t, ansi.Strip(cache.RenderStreaming(unfinished, 80, 80)), "**")
	require.Contains(t, ansi.Strip(RenderMarkdownBlocks(unfinished, 80, 80)), "**", "final source keeps literal unmatched syntax")
	for _, source := range []string{"```md\n**literal\n", "`**literal`", "Earlier **completed** text.\n\nA tail.", "- **Open heading"} {
		view := cache.RenderStreaming(source, 80, 80)
		if strings.HasPrefix(source, "- ") {
			require.Contains(t, ansi.Strip(view), "Open heading")
			require.NotContains(t, ansi.Strip(view), "**")
			continue
		}
		require.Equal(t, RenderMarkdownBlocks(source, 80, 80), view)
	}
}
