package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/tui/components/core"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
	"github.com/raulaguila/opcode/internal/tui/page"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/stretchr/testify/require"
	"testing"
)

type nativeSaveCompletedFixture struct{}
type nativeSaveQuitProbe struct {
	dialog.ProviderDialog
	pending   bool
	completed int
}

func (p *nativeSaveQuitProbe) PendingNativeOperation() bool { return p.pending }
func (p *nativeSaveQuitProbe) Update(msg tea.Msg) (util.Model, tea.Cmd) {
	if _, ok := msg.(nativeSaveCompletedFixture); ok {
		p.pending = false
		p.completed++
	}
	return p, nil
}

type nativeQuitPage struct{ draftPageProbe }

func (p *nativeQuitPage) Update(tea.Msg) (util.Model, tea.Cmd) { return p, nil }

type nativeQuitFilepicker struct{ dialog.FilepickerCmp }

func (p *nativeQuitFilepicker) Update(tea.Msg) (util.Model, tea.Cmd) { return p, nil }

func TestNativeVaultOperationBlocksGlobalQuitUntilResult(t *testing.T) {
	for _, confirmedFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "ctrl-c", true: "existing-confirmation"}[confirmedFirst], func(t *testing.T) {
			provider := &nativeSaveQuitProbe{pending: true}
			draft := &nativeQuitPage{}
			a := appModel{providerDialog: provider, showProviderDialog: true, currentPage: page.ChatPage, pages: map[page.PageID]util.Model{page.ChatPage: draft}, status: core.NewStatusCmp(nil), filepicker: &nativeQuitFilepicker{}}
			var msg tea.Msg = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
			if confirmedFirst {
				a.showQuit = true
				msg = dialog.ConfirmQuitMsg{}
			}
			updated, command := a.Update(msg)
			a = updated.(appModel)
			require.NotNil(t, command)
			notice, ok := command().(util.InfoMsg)
			require.True(t, ok, "pending save must never return a quit command")
			require.Contains(t, notice.Msg, "vault")
			require.False(t, a.showQuit)
			require.True(t, a.showProviderDialog)
			require.True(t, provider.pending)
			require.Zero(t, draft.calls, "draft persistence must wait, too")
			// A confirmation emitted before the asynchronous save is equally blocked.
			updated, command = a.Update(dialog.ConfirmQuitMsg{})
			a = updated.(appModel)
			require.IsType(t, util.InfoMsg{}, command())
			require.True(t, provider.pending)
			updated, _ = a.Update(nativeSaveCompletedFixture{})
			a = updated.(appModel)
			require.Equal(t, 1, provider.completed, "native completion still reaches its owner")
			require.False(t, provider.pending)
			updated, command = a.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
			a = updated.(appModel)
			require.True(t, a.showQuit, "normal Ctrl+C behavior resumes after completion")
			require.Nil(t, command)
			_, command = a.Update(dialog.ConfirmQuitMsg{})
			require.IsType(t, tea.QuitMsg{}, command())
			require.Equal(t, 1, draft.calls)
		})
	}
}
