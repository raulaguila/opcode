package styles

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

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
	before := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	defer lipgloss.SetColorProfile(before)
	source := "4. first\n5. second\n\n| Column A | Column B |\n| --- | --- |\n| " + strings.Repeat("left ", 10) + " | " + strings.Repeat("right ", 8) + " |\n\n```go\nfmt.Println(\"streaming\")\n"
	view := RenderMarkdownBlocks(source, 120, 48)
	require.Equal(t, ansi.Strip(view), view)
	require.Contains(t, view, "4.")
	require.Contains(t, view, "5.")
	require.Contains(t, view, "streaming")
	require.Contains(t, view, "Column A")
	wide := false
	for line := range strings.SplitSeq(view, "\n") {
		if strings.Contains(line, "│") && ansi.StringWidth(line) > 60 {
			wide = true
		}
	}
	require.True(t, wide, "table should use the terminal width")
}
