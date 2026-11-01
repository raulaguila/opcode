package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
	"github.com/raulaguila/opcode/internal/tui/page"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/stretchr/testify/require"
)

func TestApprovalPaletteEntryOpensExistingChooserAtSelectedPolicy(t *testing.T) {
	a := appModel{
		currentPage: page.ChatPage,
		pages: map[page.PageID]util.Model{page.ChatPage: &approvalFooterPage{state: util.FooterState{
			Mode: "ask", ApprovalPolicy: string(permission.ApprovalProjectEdits),
		}}},
		commandDialog: dialog.NewCommandDialogCmp(),
		commands:      []dialog.Command{{ID: "mode-ask"}, {ID: "mode-plan"}, {ID: "mode-execute"}},
	}
	newCommandsBuiltins(&a)
	var entry dialog.Command
	for _, command := range a.contextualCommands() {
		if command.ID == "approval" {
			entry = command
		}
	}
	require.NotNil(t, entry.Handler)
	require.Equal(t, page.WorkspaceCommandMsg{Command: "approval"}, entry.Handler(entry)())
	cmd, handled := a.productivityMessage(page.OpenApprovalChooserMsg{})
	require.True(t, handled)
	require.Nil(t, cmd, "opening a chooser must not apply consent")
	require.True(t, a.showCommandDialog)
	_, selected := a.commandDialog.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, "approval-project_edits", selected().(dialog.CommandSelectedMsg).Command.ID)
	require.Equal(t, "ask", a.footerState().Mode)
}
