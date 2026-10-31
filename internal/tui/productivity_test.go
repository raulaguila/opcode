package tui

import (
	tea "charm.land/bubbletea/v2"
	"errors"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
	"github.com/raulaguila/opcode/internal/tui/page"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/stretchr/testify/require"
	"testing"
)

type modalModelProbe struct {
	dialog.ModelDialog
	keys []string
}

func (p *modalModelProbe) Update(msg tea.Msg) (util.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		p.keys = append(p.keys, k.String())
	}
	return p, nil
}

type modalSessionProbe struct {
	dialog.SessionDialog
	keys []string
}

func (p *modalSessionProbe) Update(msg tea.Msg) (util.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		p.keys = append(p.keys, k.String())
	}
	return p, nil
}

type modalPermissionProbe struct {
	dialog.PermissionDialogCmp
	keys []string
}

func (p *modalPermissionProbe) Update(msg tea.Msg) (util.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		p.keys = append(p.keys, k.String())
	}
	return p, nil
}
func TestModalShortcutsAndApprovalKeepKeyboardFocus(t *testing.T) {
	model := &modalModelProbe{}
	a := appModel{showModelDialog: true, modelDialog: model}
	updated, _ := a.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	a = updated.(appModel)
	require.Equal(t, []string{"ctrl+f"}, model.keys)
	require.False(t, a.showFilepicker)
	sessions := &modalSessionProbe{}
	a = appModel{showSessionDialog: true, sessionDialog: sessions}
	updated, _ = a.Update(tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})
	a = updated.(appModel)
	require.Equal(t, []string{"ctrl+h"}, sessions.keys)
	require.False(t, a.showHelp)
	permission := &modalPermissionProbe{}
	a = appModel{showModelDialog: true, modelDialog: model, showPermissions: true, permissions: permission}
	a.Update(testTextKey(string([]rune("a")), 0))
	require.Equal(t, []string{"a"}, permission.keys)
	require.Len(t, model.keys, 1)
}

type draftPageProbe struct {
	util.Model
	err   error
	calls int
}

func (p *draftPageProbe) PersistDraft() error { p.calls++; return p.err }
func TestConfirmedQuitSavesDraftAndReportsFailureBeforeExit(t *testing.T) {
	for _, failed := range []bool{false, true} {
		drafts := &draftPageProbe{}
		if failed {
			drafts.err = errors.New("private storage unavailable")
		}
		a := appModel{pages: map[page.PageID]util.Model{page.ChatPage: drafts}}
		_, cmd := a.Update(dialog.ConfirmQuitMsg{})
		require.Equal(t, 1, drafts.calls)
		require.NotNil(t, cmd)
		if failed {
			info, ok := cmd().(util.InfoMsg)
			require.True(t, ok)
			require.Contains(t, info.Msg, "storage unavailable")
		} else {
			_, ok := cmd().(tea.QuitMsg)
			require.True(t, ok)
		}
	}
}
