package dialog

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSessionSearchRenameArchiveAndExportUseSelectedSession(t *testing.T) {
	d := NewSessionDialogCmp().(*sessionDialogCmp)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 25})
	d.SetSessions([]session.Session{{ID: "one", Title: "Primeira"}, {ID: "two", Title: "Correção"}})
	d.SetArchived(map[string]bool{"one": true})
	require.Len(t, d.filtered, 1)
	d.Update(tea.KeyMsg{Type: tea.KeyCtrlH})
	require.Len(t, d.filtered, 2)
	d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("corre")})
	require.Len(t, d.filtered, 1)
	require.Equal(t, "two", d.filtered[0].ID)
	d.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	d.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Novo nome")})
	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	rename := cmd().(SessionActionMsg)
	require.Equal(t, "rename", rename.Action)
	require.Equal(t, "Novo nome", rename.Title)
	require.Equal(t, "two", rename.Session.ID)
	for _, tc := range []struct {
		key    tea.KeyType
		action string
	}{{tea.KeyCtrlA, "archive"}, {tea.KeyCtrlE, "export"}} {
		_, cmd = d.Update(tea.KeyMsg{Type: tc.key})
		action := cmd().(SessionActionMsg)
		require.Equal(t, tc.action, action.Action)
		require.Equal(t, "two", action.Session.ID)
	}
}
func TestSessionMouseSelectionUsesVisibleOffset(t *testing.T) {
	d := NewSessionDialogCmp().(*sessionDialogCmp)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	d.SetSessions([]session.Session{{ID: "one"}, {ID: "two"}, {ID: "three"}})
	d.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	d.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	require.Equal(t, 1, d.offset)
	_, cmd := d.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 4, Y: 4})
	require.Equal(t, "two", cmd().(SessionSelectedMsg).Session.ID)
}
