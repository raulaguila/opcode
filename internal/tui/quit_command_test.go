package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
	"github.com/raulaguila/opcode/internal/tui/page"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/stretchr/testify/require"
)

func TestQuitCommandOpensExistingConfirmationAndPreservesNativeSaveGuard(t *testing.T) {
	for _, pending := range []bool{false, true} {
		draft := &draftPageProbe{}
		a := appModel{pages: map[page.PageID]tea.Model{page.ChatPage: draft}, providerDialog: &nativeSaveQuitProbe{pending: pending}}
		updated, cmd := a.Update(dialog.OpenQuitMsg{})
		a = updated.(appModel)
		require.Equal(t, !pending, a.showQuit)
		require.Zero(t, draft.calls)
		if pending {
			require.NotNil(t, cmd)
			require.IsType(t, util.InfoMsg{}, cmd())
		} else {
			require.Nil(t, cmd)
			_, cmd = a.Update(dialog.ConfirmQuitMsg{})
			require.IsType(t, tea.QuitMsg{}, cmd())
			require.Equal(t, 1, draft.calls)
		}
	}
}
