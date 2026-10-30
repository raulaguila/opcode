package layout

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/stretchr/testify/require"
)

func TestOverlayKeepsCoordinatesAndWideCells(t *testing.T) {
	for _, test := range []struct {
		foreground, background, want string
		x, y                         int
	}{
		{"XY", "abcdef\nghijkl\nmnopqr", "abcdef\ngXYjkl\nmnopqr", 1, 1},
		{"界", "abcd\nefgh", "a界d\nefgh", 1, 0},
		{"e\u0301", "abcd\nefgh", "ae\u0301cd\nefgh", 1, 0},
		{"long\nwide\nview", "ab\ncd", "long\nwide\nview", 10, 10},
	} {
		require.Equal(t, test.want, PlaceOverlay(test.x, test.y, test.foreground, test.background, false))
	}
}

func TestOverlayShadowPreservesStyledRows(t *testing.T) {
	for _, size := range [][2]int{{0, 1}, {1, 1}, {14, 5}, {70, 30}} {
		require.Equal(t, legacyOverlayShadow(size[0], size[1]), overlayShadow(size[0], size[1]))
	}
}

// This retained reference isolates the previous quadratic construction so its
// output and allocation cost can be compared without changing product behavior.
func legacyOverlayShadow(width, height int) string {
	t := theme.CurrentTheme()
	blank := styles.BaseStyle().Render(" ")
	shadow := lipgloss.NewStyle().Background(t.BackgroundDarker()).Foreground(t.Background()).Render("░")
	var result string
	for row := 0; row <= height; row++ {
		if row == 0 {
			result += blank + strings.Repeat(blank, width) + "\n"
			continue
		}
		result += blank + strings.Repeat(shadow, width)
		if row < height {
			result += "\n"
		}
	}
	return result
}

var overlayBenchmarkResult string

func BenchmarkOverlayShadow(b *testing.B) {
	for _, test := range []struct {
		name  string
		build func(int, int) string
	}{{"legacy", legacyOverlayShadow}, {"linear", overlayShadow}} {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				overlayBenchmarkResult = test.build(120, 40)
			}
		})
	}
}
