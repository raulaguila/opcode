package tui

import (
	"github.com/raulaguila/opcode/internal/tui/util"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/tui/components/core"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
	"github.com/raulaguila/opcode/internal/tui/page"
	"github.com/stretchr/testify/require"
)

func TestFooterClickUsesClippedPageHeight(t *testing.T) {
	p := &focusPageProbe{content: strings.Repeat("oversized\n", 100)}
	status, _ := core.NewStatusCmp(nil).Update(tea.WindowSizeMsg{Width: 80})
	a := appModel{width: 80, height: 23, currentPage: page.ChatPage,
		pages: map[page.PageID]util.Model{page.ChatPage: p}, status: status.(core.StatusCmp),
		commandDialog: dialog.NewCommandDialogCmp()}
	updated, _ := a.Update(tea.MouseClickMsg{X: 1, Y: 23, Button: tea.MouseLeft})
	require.True(t, updated.(appModel).showCommandDialog, "rendered footer remains clickable when page content overflows")
}

func TestFooterModeClickOpensOnlyModesAndKeepsExistingCommandHandler(t *testing.T) {
	status, _ := core.NewStatusCmp(nil).Update(tea.WindowSizeMsg{Width: 80})
	a := appModel{currentPage: page.ChatPage, pages: map[page.PageID]util.Model{page.ChatPage: &inputPage{}}, status: status.(core.StatusCmp), commandDialog: dialog.NewCommandDialogCmp()}
	a.commands = []dialog.Command{
		{ID: "settings", Title: "Settings"},
		{ID: "mode-ask", Title: "Ask", Handler: func(dialog.Command) tea.Cmd {
			return func() tea.Msg { return page.WorkflowCommandMsg{Command: "mode", Value: "ask"} }
		}},
		{ID: "mode-plan", Title: "Plan"},
		{ID: "mode-execute", Title: "Execute"},
	}
	updated, cmd := a.Update(tea.MouseClickMsg{X: 1, Y: 0, Button: tea.MouseLeft})
	a = updated.(appModel)
	require.Nil(t, cmd, "opening the selector must not change mode")
	require.True(t, a.showCommandDialog)
	require.Contains(t, a.commandDialog.View(), "Ask")
	require.NotContains(t, a.commandDialog.View(), "Settings")
	_, selected := a.commandDialog.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: 0})
	_, handler := a.Update(selected())
	msg := handler().(page.WorkflowCommandMsg)
	require.Equal(t, "ask", msg.Value)
	require.Equal(t, "mode", msg.Command)
}
