package styles

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/stretchr/testify/require"
)

func TestLiveMarkdownCacheMatchesEveryFullSnapshot(t *testing.T) {
	profile, dark, name := theme.ColorProfile(), theme.HasDarkBackground(), theme.CurrentThemeName()
	t.Cleanup(func() {
		theme.SetColorProfile(profile)
		theme.SetHasDarkBackground(dark)
		_ = theme.PreviewTheme(name)
	})
	// Monokai's code colors avoid the highlighter's nondeterministic equidistant
	// terminal256 palette choices. Keep the oracle byte-exact for every prefix.
	require.NoError(t, theme.PreviewTheme("monokai"))
	for _, color := range []colorprofile.Profile{colorprofile.Ascii, colorprofile.TrueColor} {
		theme.SetColorProfile(color)
		for _, source := range append(markdownReuseFixtures(), "A paragraph.\n\n```go\nprintln(1)\n```\n\nA tail with [ref].\n\n[ref]: https://example.test/source#L2\n") {
			var cache MarkdownCache
			for end := 1; end <= len(source); end++ {
				assertMarkdownSnapshot(t, &cache, source[:end], 100, 48)
			}
			assertMarkdownSnapshot(t, &cache, source, 60, 40)
			cache.Reset()
			require.Empty(t, cache.entries)
		}
	}
}

func assertMarkdownSnapshot(t *testing.T, cache *MarkdownCache, source string, width, prose int) {
	t.Helper()
	require.Equal(t, RenderMarkdownBlocks(source, width, prose), cache.Render(source, width, prose), "source %q", source)
}

func TestLiveMarkdownCacheBoundedAndChangesTheme(t *testing.T) {
	var cache MarkdownCache
	source := strings.Repeat("A stable paragraph.\n\n", 20) + "tail"
	require.Equal(t, RenderMarkdownBlocks(source, 100, 48), cache.Render(source, 100, 48))
	require.NotEmpty(t, cache.entries)
	name := theme.CurrentThemeName()
	t.Cleanup(func() { _ = theme.PreviewTheme(name) })
	require.NoError(t, theme.PreviewTheme("gruvbox"))
	require.Equal(t, RenderMarkdownBlocks(source, 100, 48), cache.Render(source, 100, 48))
	for i := range 200 {
		cache.Render(fmt.Sprintf("Paragraph %d with **emphasis**.\n\nTail", i), 100, 48)
	}
	require.LessOrEqual(t, cache.bytes, markdownCacheBytes)
	require.LessOrEqual(t, len(cache.entries), markdownCacheEntries)
}
