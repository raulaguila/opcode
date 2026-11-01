package styles

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestFrameCarriesTitleMetaAndHintInBorders(t *testing.T) {
	view := ansi.Strip(Frame("one\ntwo wide line that is too long", 20, 2, 1, "Title", "3 of 9", "Esc closes"))
	lines := strings.Split(view, "\n")
	require.Len(t, lines, 6)
	for _, line := range lines {
		require.Equal(t, 26, ansi.StringWidth(line), "every row spans inner + padding + borders: %q", line)
	}
	require.True(t, strings.HasPrefix(lines[0], "╭─ Title "))
	require.True(t, strings.HasSuffix(lines[0], " 3 of 9 ─╮"))
	require.True(t, strings.HasPrefix(lines[5], "╰─ Esc closes "))
	require.Contains(t, lines[3], "two wide line that …")
	narrow := ansi.Strip(Frame("x", 2, 0, 0, "A very long title", "meta", "hint"))
	for _, line := range strings.Split(narrow, "\n") {
		require.Equal(t, 4, ansi.StringWidth(line), "labels never widen a narrow frame: %q", line)
	}
}
