package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/tui/page"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/stretchr/testify/require"
)

type selectionPriorityPage struct{}

func (*selectionPriorityPage) Init() tea.Cmd                         { return nil }
func (*selectionPriorityPage) View() string                          { return "selected response" }
func (p *selectionPriorityPage) Update(tea.Msg) (tea.Model, tea.Cmd) { return p, nil }
func (*selectionPriorityPage) SelectionKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	return util.CmdHandler(util.CopyTextMsg{Text: "selection"}), msg.Type == tea.KeyCtrlC
}

func TestSelectionCopyPrecedesGlobalQuit(t *testing.T) {
	a := appModel{currentPage: page.ChatPage, pages: map[page.PageID]tea.Model{page.ChatPage: &selectionPriorityPage{}}}
	updated, cmd := a.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	require.False(t, updated.(appModel).showQuit)
	require.Equal(t, "selection", cmd().(util.CopyTextMsg).Text)
}
