package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestCommandSearchFindsAdvancedActionsAndResetsWhenReopened(t *testing.T) {
	m := NewCommandDialogCmp().(*commandDialogCmp)
	items := []Command{{ID: "models", Title: "Models"}, {ID: "queue", Title: "Pending instructions"}, {ID: "permissions", Title: "Session permissions"}}
	m.SetCommands(items)
	m.Update(tea.KeyPressMsg{Text: "permissions"})
	require.Len(t, m.listView.GetItems(), 1)
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, "permissions", cmd().(CommandSelectedMsg).Command.ID)
	m.SetCommands(items)
	require.Empty(t, m.search.Value())
	require.Len(t, m.listView.GetItems(), 3)
	m.Update(tea.KeyPressMsg{Text: "j"})
	require.Equal(t, "j", m.search.Value(), "letters must search rather than navigate")
	require.Empty(t, m.listView.GetItems())
}

func TestModeChooserDistinguishesActiveModeAndResetsTitleOnPalette(t *testing.T) {
	m := NewCommandDialogCmp().(*commandDialogCmp)
	items := []Command{{ID: "mode-ask", Title: "Ask"}, {ID: "mode-plan", Title: "Plan"}, {ID: "mode-execute", Title: "Execute"}}
	m.SetCommands(items)
	m.SetContext("Work mode", "mode-execute")
	view := ansi.Strip(m.View())
	require.Contains(t, view, "Work mode")
	require.Contains(t, view, "✓ Execute")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, "mode-execute", cmd().(CommandSelectedMsg).Command.ID, "opening highlights the current mode")
	m.SetCommands(items)
	require.Empty(t, m.title)
	require.Empty(t, m.activeID)
	require.NotContains(t, ansi.Strip(m.View()), "✓ Execute")
}

func TestCommandPickerFitsNarrowAndShortTerminals(t *testing.T) {
	m := NewCommandDialogCmp().(*commandDialogCmp)
	for _, size := range []tea.WindowSizeMsg{{Width: 36, Height: 14}, {Width: 80, Height: 24}} {
		m.Update(size)
		m.SetCommands([]Command{{ID: "long", Title: "A very long title that must stay inside a narrow terminal"}})
		require.LessOrEqual(t, lipgloss.Width(m.View()), size.Width)
		require.LessOrEqual(t, lipgloss.Height(m.View()), size.Height)
	}
}
