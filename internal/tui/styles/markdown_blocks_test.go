package styles

import (
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestMarkdownBlockSpacingHasOneBlankLine(t *testing.T) {
	profile := theme.ColorProfile()
	t.Cleanup(func() { theme.SetColorProfile(profile) })
	for _, color := range []colorprofile.Profile{colorprofile.Ascii, colorprofile.TrueColor} {
		theme.SetColorProfile(color)
		for _, source := range []string{
			"# Title\n\nText.",
			"First paragraph.\n\nSecond paragraph.",
			"- Item\n\nText.",
			"> Quote\n\nText.",
			"```text\ncode\n```\n\nText.",
			"| A | B |\n| --- | --- |\n| a | b |\n\nText.",
			"---\n\nText.",
		} {
			view := ansi.Strip(RenderMarkdownBlocks(source, 100, 80))
			require.NotEmpty(t, strings.TrimSpace(strings.Split(view, "\n")[0]), "no leading padding")
			require.NotEmpty(t, strings.TrimSpace(view[strings.LastIndexByte(view, '\n')+1:]), "no trailing padding")
			blanks := 0
			for line := range strings.SplitSeq(strings.TrimSpace(view), "\n") {
				if strings.TrimSpace(line) == "" {
					blanks++
					require.LessOrEqual(t, blanks, 1, "source %q => %q", source, view)
				} else {
					blanks = 0
				}
			}
		}
	}
}

func TestMarkdownCompactSpacingPreservesContent(t *testing.T) {
	view := ansi.Strip(RenderMarkdownBlocks("First\nsecond\n\n```text\n  indented\n\n\n  after\n```\n\n- outer\n  - nested\n\nSee [source][ref].\n\n[ref]: https://example.test/file#L2\n", 100, 80))
	require.Contains(t, view, "First")
	require.Contains(t, view, "second")
	require.Contains(t, view, "  indented")
	require.Contains(t, view, "  after")
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		if strings.Contains(line, "indented") {
			require.Empty(t, strings.TrimSpace(lines[i+1]))
			require.Empty(t, strings.TrimSpace(lines[i+2]))
			require.Contains(t, lines[i+3], "after")
		}
	}
	require.Contains(t, view, "outer")
	require.Contains(t, view, "nested")
	require.Contains(t, view, "https://example.test/file#L2")
}

func TestMixedMarkdownKeepsProseNarrowAndCodeWide(t *testing.T) {
	source := strings.Repeat("Narrative explanation ", 20) + "\n\n```text\nREADME.md:1-5 " + strings.Repeat("x", 80) + "\n```\n\n" + strings.Repeat("Further explanation ", 20) + "\n\nSee [source][ref].\n\n[ref]: https://example.test/README.md#L1\n"
	view := ansi.Strip(RenderMarkdownBlocks(source, 120, 48))
	codeFound := false
	for line := range strings.SplitSeq(view, "\n") {
		if strings.Contains(line, "README.md:1-5") {
			require.Greater(t, ansi.StringWidth(line), 70)
			codeFound = true
			continue
		}
		if strings.Contains(line, "Narrative") || strings.Contains(line, "Further") {
			require.LessOrEqual(t, ansi.StringWidth(strings.TrimSpace(line)), 48)
		}
	}
	require.True(t, codeFound)
	require.Contains(t, view, "https://example.test/README.md#L1")
	require.NotContains(t, view, "[ref]")
}

func TestMarkdownContainersTablesAndNoColor(t *testing.T) {
	before := theme.ColorProfile()
	theme.SetColorProfile(colorprofile.Ascii)
	defer theme.SetColorProfile(before)
	source := "4. first\n5. second\n\n| Column A | Column B |\n| --- | --- |\n| " + strings.Repeat("left ", 10) + " | " + strings.Repeat("right ", 8) + " |\n\n```go\nfmt.Println(\"streaming\")\n"
	view := RenderMarkdownBlocks(source, 120, 48)
	assertMarkdownDefaultColors(t, view)
	plain := ansi.Strip(view)
	require.Contains(t, plain, "4.")
	require.Contains(t, plain, "5.")
	require.Contains(t, plain, "streaming")
	require.Contains(t, plain, "Column A")
	wide := false
	for line := range strings.SplitSeq(view, "\n") {
		if strings.Contains(line, "│") && ansi.StringWidth(line) > 60 {
			wide = true
		}
	}
	require.True(t, wide, "table should use the terminal width")
}
