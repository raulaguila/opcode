package styles

import (
	"charm.land/lipgloss/v2"
	"github.com/raulaguila/opcode/internal/tui/theme"
)

var (
	ImageBakcground = "#212121"
)

// Style generation functions that use the current theme

// BaseStyle returns the base style with background and foreground colors
func BaseStyle() lipgloss.Style {
	t := theme.CurrentTheme()
	return lipgloss.NewStyle().
		Background(t.Background()).
		Foreground(t.Text())
}
