package tui

import (
	"github.com/raulaguila/opcode/internal/tui/util"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/permission"
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

type approvalFooterPage struct {
	inputPage
	state util.FooterState
}

func (p *approvalFooterPage) FooterState() util.FooterState { return p.state }

func TestFooterApprovalClickFocusesSelectedPolicyWithoutChangingMode(t *testing.T) {
	for _, width := range []int{60, 180} {
		for _, policy := range []permission.ApprovalPolicy{permission.ApprovalPrompt, permission.ApprovalProjectEdits} {
			state := util.FooterState{Mode: "execute", ApprovalPolicy: string(policy)}
			status, _ := core.NewStatusCmp(nil).Update(tea.WindowSizeMsg{Width: width})
			a := appModel{currentPage: page.ChatPage, pages: map[page.PageID]util.Model{page.ChatPage: &approvalFooterPage{state: state}}, status: status.(core.StatusCmp), commandDialog: dialog.NewCommandDialogCmp()}
			a.commands = []dialog.Command{{ID: "mode-ask"}, {ID: "mode-plan"}, {ID: "mode-execute"}}
			before, _, found := strings.Cut(ansi.Strip(a.statusView()), "▾")
			require.True(t, found)
			require.Nil(t, a.footerClick(ansi.StringWidth(before)+2), "opening a chooser does not change consent")
			require.True(t, a.showCommandDialog)
			require.Contains(t, a.commandDialog.View(), "✓ Execute")
			_, selected := a.commandDialog.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			command := selected().(dialog.CommandSelectedMsg).Command
			require.Equal(t, "approval-"+string(policy), command.ID)
			require.Equal(t, policy, command.Handler(command)().(page.ApprovalPolicyMsg).Policy)
			require.Equal(t, "execute", a.footerState().Mode)
		}
	}
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
