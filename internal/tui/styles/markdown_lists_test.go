package styles

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"
)

func TestMarkdownListsHangWrappedTextAfterMarker(t *testing.T) {
	profile := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	for _, color := range []termenv.Profile{termenv.Ascii, termenv.TrueColor} {
		lipgloss.SetColorProfile(color)
		for _, source := range []string{
			"- " + strings.Repeat("wrapped words ", 8),
			"9. " + strings.Repeat("wrapped words ", 8) + "\n10. Final item",
			"4) " + strings.Repeat("wrapped words ", 8),
		} {
			view := ansi.Strip(RenderMarkdownBlocks(source, 48, 48))
			indent := ansi.StringWidth(view[:strings.Index(view, "wrapped")])
			require.Greater(t, indent, 1, view)
			for line := range strings.SplitSeq(view, "\n") {
				if strings.TrimSpace(line) == "" {
					continue
				}
				require.LessOrEqual(t, ansi.StringWidth(line), 48, view)
				if strings.Contains(line, "wrapped") && !strings.Contains(line, "•") && !strings.ContainsAny(line, ".)") {
					require.Equal(t, indent, len(line)-len(strings.TrimLeft(line, " ")), view)
				}
			}
		}
	}
}

func TestMarkdownListNumberingNestingAndTaskState(t *testing.T) {
	view := ansi.Strip(RenderMarkdownBlocks("9. First\n10. Second\n    - Nested\n      - Deep\n11. Last\n\n- [x] Done\n- [ ] Pending", 100, 80))
	for _, text := range []string{"9. First", "10. Second", "11. Last", "[✓] Done", "[ ] Pending"} {
		require.Contains(t, view, text)
	}
	require.Equal(t, 1, strings.Count(view, "[✓]"))
	require.Equal(t, 1, strings.Count(view, "[ ]"))
	var nested, deep int
	for line := range strings.SplitSeq(view, "\n") {
		if strings.Contains(line, "Nested") {
			nested = strings.Index(line, "•")
		}
		if strings.Contains(line, "Deep") {
			deep = strings.Index(line, "•")
		}
	}
	// Bullet is one display column but three UTF-8 bytes; prefixes here are ASCII.
	require.GreaterOrEqual(t, nested, 4)
	require.Greater(t, deep, nested)
}

func TestMarkdownListsKeepCodeBlankLinesTablesAndReferences(t *testing.T) {
	source := "1. Read [source][ref].\n\n   Second paragraph.\n\n   ```text\n   first\n\n\n     indented\n   ```\n\n   | A | B |\n   | --- | --- |\n   | one | two |\n\n2. Finish.\n\n[ref]: https://example.test/file.go#L2\n"
	view := ansi.Strip(RenderMarkdownBlocks(source, 100, 48))
	for _, text := range []string{"1. Read", "https://example.test/file.go#L2", "Second paragraph.", "first", "indented", "one", "two", "2. Finish."} {
		require.Contains(t, view, text)
	}
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "first" {
			require.Empty(t, strings.TrimSpace(lines[i+1]))
			require.Empty(t, strings.TrimSpace(lines[i+2]))
			require.Contains(t, lines[i+3], "  indented")
		}
	}
	require.NotContains(t, view, "[ref]")
	for line := range strings.SplitSeq(view, "\n") {
		require.LessOrEqual(t, ansi.StringWidth(line), 100)
	}
}

func TestMarkdownListsKeepShortItemsCompactAndSeparateLongItems(t *testing.T) {
	short := ansi.Strip(RenderMarkdownBlocks("- First\n- Second", 60, 60))
	require.NotContains(t, short, "\n\n")
	long := ansi.Strip(RenderMarkdownBlocks("- "+strings.Repeat("wrapped words ", 8)+"\n- Second", 40, 40))
	lines := strings.Split(long, "\n")
	for i, line := range lines {
		if strings.Contains(line, "Second") {
			require.Greater(t, i, 0)
			require.Empty(t, strings.TrimSpace(lines[i-1]))
		}
	}
}

func TestMarkdownListMarginRetainsInternalWhitespace(t *testing.T) {
	require.Equal(t, "\n  code\n\n", removeMarkdownListMargin(" \n   code\n \n "))
}

func TestMarkdownListCodeRetainsSourceBlankEdges(t *testing.T) {
	profile := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	for _, color := range []termenv.Profile{termenv.Ascii, termenv.TrueColor} {
		lipgloss.SetColorProfile(color)
		for _, code := range []string{
			"\n      \n   first\n\n     \n   last\n    \n\n",
			"\n    \n\n",
			"    \n",
		} {
			assertMarkdownListCodeBlankEdges(t, code)
		}
	}
}

func assertMarkdownListCodeBlankEdges(t *testing.T, code string) {
	t.Helper()
	source := "1. Before.\n\n   ```text\n" + code + "   ```\n\n   After.\n"
	view := ansi.Strip(RenderMarkdownBlocks(source, 80, 80))
	lines := strings.Split(view, "\n")
	positions := map[string]int{}
	for i, line := range lines {
		for _, word := range []string{"Before.", "first", "last", "After."} {
			if strings.Contains(line, word) {
				positions[word] = i
			}
		}
	}
	if strings.Contains(code, "first") {
		require.Equal(t, 3, positions["first"]-positions["Before."]-1, view)
		require.Equal(t, 2, positions["last"]-positions["first"]-1, view)
		require.Equal(t, 3, positions["After."]-positions["last"]-1, view)
	} else {
		blankSourceLines := strings.Count(code, "\n")
		require.Equal(t, blankSourceLines+2, positions["After."]-positions["Before."]-1, view)
	}
	var cache MarkdownCache
	for end := 1; end <= len(source); end++ {
		require.Equal(t, RenderMarkdownBlocks(source[:end], 80, 80), cache.Render(source[:end], 80, 80))
	}
}
