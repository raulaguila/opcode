package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/tui/components/core"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
	"github.com/raulaguila/opcode/internal/tui/page"
	"github.com/stretchr/testify/require"
)

func TestFooterModeClickOpensOnlyModesAndKeepsExistingCommandHandler(t *testing.T) {
	status, _ := core.NewStatusCmp(nil).Update(tea.WindowSizeMsg{Width: 80})
	a := appModel{currentPage: page.ChatPage, pages: map[page.PageID]tea.Model{page.ChatPage: &inputPage{}}, status: status.(core.StatusCmp), commandDialog: dialog.NewCommandDialogCmp()}
	a.commands = []dialog.Command{
		{ID: "settings", Title: "Settings"},
		{ID: "mode-ask", Title: "Ask", Handler: func(dialog.Command) tea.Cmd {
			return func() tea.Msg { return page.WorkflowCommandMsg{Command: "mode", Value: "ask"} }
		}},
		{ID: "mode-plan", Title: "Plan"},
		{ID: "mode-execute", Title: "Execute"},
	}
	updated, cmd := a.Update(tea.MouseMsg{X: 1, Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	a = updated.(appModel)
	require.Nil(t, cmd, "opening the selector must not change mode")
	require.True(t, a.showCommandDialog)
	require.Contains(t, a.commandDialog.View(), "Ask")
	require.NotContains(t, a.commandDialog.View(), "Settings")
	_, selected := a.commandDialog.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, handler := a.Update(selected())
	msg := handler().(page.WorkflowCommandMsg)
	require.Equal(t, "ask", msg.Value)
	require.Equal(t, "mode", msg.Command)
}
