package dialog

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestWideSettingsShowSectionNavAndSwitchByClick(t *testing.T) {
	m := settingsDialog(t)
	m.Update(tea.WindowSizeMsg{Width: 130, Height: 30})
	view := ansi.Strip(m.View())
	require.Contains(t, view, "› Connections")
	require.Contains(t, view, "Preferences")
	require.LessOrEqual(t, lipgloss.Width(m.View()), 130)
	m.Update(tea.MouseClickMsg{X: 4, Y: 3, Button: tea.MouseLeft})
	require.NotEqual(t, "list", m.screen, "the Preferences section opens from the nav")
	require.True(t, strings.Contains(ansi.Strip(m.View()), "› Preferences"), m.screen)
	m.Update(tea.MouseClickMsg{X: 4, Y: 2, Button: tea.MouseLeft})
	require.Equal(t, "list", m.screen)
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	require.NotContains(t, ansi.Strip(m.View()), "› Connections", "narrow terminals keep the single column")
}
