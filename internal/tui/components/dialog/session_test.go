package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/stretchr/testify/require"
)

func TestSessionSearchRenameArchiveAndExportUseSelectedSession(t *testing.T) {
	d := NewSessionDialogCmp().(*sessionDialogCmp)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 25})
	d.SetSessions([]session.Session{{ID: "one", Title: "Primeira"}, {ID: "two", Title: "Correção"}})
	d.SetArchived(map[string]bool{"one": true})
	require.Len(t, d.filtered, 1)
	d.Update(tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})
	require.Len(t, d.filtered, 2)
	d.Update(tea.KeyPressMsg{Text: "corre"})
	require.Len(t, d.filtered, 1)
	require.Equal(t, "two", d.filtered[0].ID)
	d.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	d.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	d.Update(tea.KeyPressMsg{Text: "Novo nome"})
	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	rename := cmd().(SessionActionMsg)
	require.Equal(t, "rename", rename.Action)
	require.Equal(t, "Novo nome", rename.Title)
	require.Equal(t, "two", rename.Session.ID)
	for _, tc := range []struct {
		key    tea.KeyPressMsg
		action string
	}{{tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}, "archive"}, {tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl}, "export"}} {
		_, cmd = d.Update(tc.key)
		action := cmd().(SessionActionMsg)
		require.Equal(t, tc.action, action.Action)
		require.Equal(t, "two", action.Session.ID)
	}
}
func TestSessionMouseSelectionUsesVisibleOffset(t *testing.T) {
	d := NewSessionDialogCmp().(*sessionDialogCmp)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	d.SetSessions([]session.Session{{ID: "one"}, {ID: "two"}, {ID: "three"}})
	d.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	d.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	require.Equal(t, 1, d.offset)
	_, cmd := d.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 4, Y: 4})
	require.Equal(t, "two", cmd().(SessionPreviewRequestMsg).ID)
	_, cmd = d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, "two", cmd().(SessionSelectedMsg).Session.ID)
}

func TestSessionRecencyPreviewAndCompactLayout(t *testing.T) {
	d := NewSessionDialogCmp().(*sessionDialogCmp)
	d.SetSessions([]session.Session{{ID: "older", Title: "Older", UpdatedAt: 100}, {ID: "recent", Title: "Recent", UpdatedAt: 200}})
	require.Equal(t, "recent", d.filtered[0].ID)
	require.Equal(t, "recent", d.Init()().(SessionPreviewRequestMsg).ID)
	d.Update(SessionPreviewLoadedMsg{ID: "recent", Text: "Last message: investigate scroll"})
	for _, size := range [][2]int{{60, 20}, {80, 24}, {120, 36}} {
		d.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		require.Contains(t, d.View(), "investigate scroll")
		require.LessOrEqual(t, lipgloss.Width(d.View()), size[0])
		require.LessOrEqual(t, lipgloss.Height(d.View()), size[1])
	}
	d.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	d.Update(SessionPreviewLoadedMsg{ID: "recent", Text: "STALE"})
	require.NotContains(t, d.View(), "STALE")
	d.Update(SessionPreviewLoadedMsg{ID: "older", Text: "Older preview"})
	require.Contains(t, d.View(), "Older preview")
}
