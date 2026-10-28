package dialog

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/require"
)

func TestCommandSearchFindsAdvancedActionsAndResetsWhenReopened(t *testing.T) {
	m := NewCommandDialogCmp().(*commandDialogCmp)
	items := []Command{{ID: "models", Title: "Models"}, {ID: "queue", Title: "Pending instructions"}, {ID: "permissions", Title: "Session permissions"}}
	m.SetCommands(items)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("permissions")})
	require.Len(t, m.listView.GetItems(), 1)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, "permissions", cmd().(CommandSelectedMsg).Command.ID)
	m.SetCommands(items)
	require.Empty(t, m.search.Value())
	require.Len(t, m.listView.GetItems(), 3)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	require.Equal(t, "j", m.search.Value(), "letters must search rather than navigate")
	require.Empty(t, m.listView.GetItems())
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
