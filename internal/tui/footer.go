package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/tui/page"
	"github.com/raulaguila/opcode/internal/tui/util"
)

func (a appModel) footerState() util.FooterState {
	if current, ok := a.pages[a.currentPage].(interface{ FooterState() util.FooterState }); ok {
		return current.FooterState()
	}
	return util.FooterState{}
}

func (a appModel) statusView() string { return a.status.ViewFor(a.footerState()) }

func (a *appModel) footerClick(x int) tea.Cmd {
	switch action := a.status.ActionAt(x, a.footerState()); action {
	case "models":
		a.showModelDialog = true
		return a.modelDialog.Open()
	case "mode", "commands":
		items := a.commands
		if action == "mode" {
			items = nil
			for _, command := range a.commands {
				if strings.HasPrefix(command.ID, "mode-") {
					items = append(items, command)
				}
			}
		}
		a.commandDialog.SetCommands(items)
		a.showCommandDialog = true
	case "queue":
		return util.CmdHandler(page.WorkspaceCommandMsg{Command: "queue"})
	}
	return nil
}
