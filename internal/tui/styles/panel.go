package styles

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/raulaguila/opcode/internal/tui/theme"
)

func Panel() lipgloss.Style {
	t := theme.CurrentTheme()
	return BaseStyle().Background(t.BackgroundSecondary()).
		Border(lipgloss.NormalBorder()).BorderForeground(t.TextMuted()).
		BorderBackground(t.BackgroundSecondary())
}

func PanelTitle() lipgloss.Style {
	t := theme.CurrentTheme()
	return BaseStyle().Background(t.BackgroundSecondary()).Foreground(t.Primary()).Bold(true)
}

func SelectedRow() lipgloss.Style {
	t := theme.CurrentTheme()
	return BaseStyle().Background(t.BorderDim()).Foreground(t.Text())
}

func PanelDivider(width int) string {
	t := theme.CurrentTheme()
	return BaseStyle().Background(t.BackgroundSecondary()).Foreground(t.TextMuted()).
		Render(strings.Repeat("─", max(0, width)))
}
