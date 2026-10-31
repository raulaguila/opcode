package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/tui/page"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/stretchr/testify/require"
)

type selectionPriorityPage struct{}

func (*selectionPriorityPage) Init() tea.Cmd                          { return nil }
func (*selectionPriorityPage) View() string                           { return "selected response" }
func (p *selectionPriorityPage) Update(tea.Msg) (util.Model, tea.Cmd) { return p, nil }
func (*selectionPriorityPage) SelectionKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	return util.CmdHandler(util.CopyTextMsg{Text: "selection"}), msg.Keystroke() == "ctrl+c"
}

func TestSelectionCopyPrecedesGlobalQuit(t *testing.T) {
	a := appModel{currentPage: page.ChatPage, pages: map[page.PageID]util.Model{page.ChatPage: &selectionPriorityPage{}}}
	updated, cmd := a.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	require.False(t, updated.(appModel).showQuit)
	require.Equal(t, "selection", cmd().(util.CopyTextMsg).Text)
}
