//go:build integration

package tui

import (
	"strings"
	"testing"

	"github.com/hinshun/vt10x"
	"github.com/stretchr/testify/require"
)

func TestPTYResizeExpandsTabStopsWithoutMovingCursorOrScrollingFooter(t *testing.T) {
	screen := vt10x.New(vt10x.WithSize(80, 24))
	_, err := screen.Write([]byte("\x1b[2;5H\x1b7\x1b[20;70H\x1b[32m"))
	require.NoError(t, err)
	cursor := screen.Cursor()
	resizePTYUIScreen(screen, 180, 40)
	require.Equal(t, cursor, screen.Cursor(), "resize repair preserves the actual cursor and attributes")
	_, err = screen.Write([]byte("\x1b[40;1HAsk ▾\x1b[40;170Hmodel ▾\x1b[40;145H99\t2"))
	require.NoError(t, err)
	require.Equal(t, '2', screen.Cell(152, 39).Char, "HT beyond the old width advances to the next eight-cell stop")
	lines := strings.Split(screen.String(), "\n")
	require.Equal(t, 2, strings.Count(lines[39], "▾"), "the footer remains on its row without wrapping and scrolling")
	require.NotContains(t, lines[38], "Ask ▾")
	_, err = screen.Write([]byte("\x1b8"))
	require.NoError(t, err)
	require.Equal(t, 4, screen.Cursor().X, "repair must not overwrite DEC's saved cursor")
	require.Equal(t, 1, screen.Cursor().Y)
	resizePTYUIScreen(screen, 60, 20)
	resizePTYUIScreen(screen, 180, 40)
	_, err = screen.Write([]byte("\x1b[40;145H99\t3"))
	require.NoError(t, err)
	require.Equal(t, '3', screen.Cell(152, 39).Char, "shrink then expand reinstates newly exposed stops")
	screen = vt10x.New(vt10x.WithSize(80, 24))
	_, err = screen.Write([]byte("\x1b[3g\x1b[1;6H\x1bH\x1b[1;1H"))
	require.NoError(t, err)
	resizePTYUIScreen(screen, 180, 40)
	_, err = screen.Write([]byte("\tX"))
	require.NoError(t, err)
	require.Equal(t, 'X', screen.Cell(5, 0).Char, "existing custom stops are not replaced by defaults")
}
