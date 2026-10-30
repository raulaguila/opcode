package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
	"github.com/raulaguila/opcode/internal/tui/page"
)

func (a appModel) mouseModal() (tea.Model, bool) {
	switch {
	case a.showQuit:
		return a.quit, true
	case a.showCommandDialog:
		return a.commandDialog, true
	case a.showThemeDialog:
		return a.themeDialog, true
	case a.showFilepicker:
		return a.filepicker, true
	case a.showMultiArgumentsDialog:
		return a.multiArgumentsDialog, true
	case a.showInitDialog:
		return a.initDialog, true
	}
	return nil, false
}

func updateAppModelMouseMsg(a *appModel, msg *tea.MouseMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, handled bool) {
	if a.showPermissions && a.permissions != nil {
		col, row := a.dialogOrigin(a.permissions.View())
		msg.X -= col
		msg.Y -= row
		updated, c := a.permissions.Update((*msg))
		a.permissions = updated.(dialog.PermissionDialogCmp)
		return (*a), c, true
	}
	if a.showHelp && a.help != nil {
		col, row := a.dialogOrigin(a.help.View())
		msg.X -= col
		msg.Y -= row
		updated, c := a.help.Update((*msg))
		a.help = updated.(dialog.HelpCmp)
		return (*a), c, true
	}
	if result0, result1, result2, handled := updateAppModelMouseMsgMouseModal(a, msg); handled {
		return result0, result1, result2
	}
	// Capturing the wheel must not scroll the conversation underneath a
	// modal dialog. The model picker handles its own mouse navigation.
	if result0, result1, result2, handled := updateAppModelMouseMsgShowPermissionsShowHelpShowQuit(a); handled {
		return result0, result1, result2
	}
	if a.showSessionDialog {
		if a.sessionDialog == nil {
			return (*a), nil, true
		}
		col, row := a.dialogOrigin(a.sessionDialog.View())
		msg.X -= col
		msg.Y -= row
		updated, c := a.sessionDialog.Update((*msg))
		a.sessionDialog = updated.(dialog.SessionDialog)
		return (*a), c, true
	}
	if a.showProviderDialog {
		col, row := a.dialogOrigin(a.providerDialog.View())
		msg.X -= col
		msg.Y -= row
		d, cmd := a.providerDialog.Update((*msg))
		a.providerDialog = d.(dialog.ProviderDialog)
		return (*a), cmd, true
	}
	if a.showIntegrationDialog {
		col, row := a.dialogOrigin(a.integrationDialog.View())
		msg.X -= col
		msg.Y -= row
		d, cmd := a.integrationDialog.Update((*msg))
		a.integrationDialog = d.(dialog.IntegrationDialog)
		return (*a), cmd, true
	}
	if a.showModelDialog {
		if a.modelDialog == nil {
			return (*a), nil, true
		}
		col, row := a.modelDialogOrigin()
		msg.X -= col
		msg.Y -= row
		d, cmd := a.modelDialog.Update((*msg))
		a.modelDialog = d.(dialog.ModelDialog)
		return (*a), cmd, true
	}
	if result0, result1, result2, handled := updateAppModelMouseMsgButtonTeaMouseButtonLeft(a, msg); handled {
		return result0, result1, result2
	}
	return extractedResult0, extractedResult1, false
}

func updateAppModelMouseMsgMouseModal(a *appModel, msg *tea.MouseMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool, handled bool) {
	if m, ok := a.mouseModal(); ok && m != nil {
		if a.pages[a.currentPage] == nil || a.status == nil {
			return (*a), nil, true, true
		}
		x, y := a.dialogOrigin(m.View())
		msg.X -= x
		msg.Y -= y
		if msg.X < 0 || msg.Y < 0 || msg.X >= lipgloss.Width(m.View()) || msg.Y >= lipgloss.Height(m.View()) {
			return (*a), nil, true, true
		}
		updated, c := m.Update((*msg))
		switch {
		case a.showQuit:
			a.quit = updated.(dialog.QuitDialog)
		case a.showCommandDialog:
			a.commandDialog = updated.(dialog.CommandDialog)
		case a.showThemeDialog:
			a.themeDialog = updated.(dialog.ThemeDialog)
		case a.showFilepicker:
			a.filepicker = updated.(dialog.FilepickerCmp)
		case a.showMultiArgumentsDialog:
			a.multiArgumentsDialog = updated.(dialog.MultiArgumentsDialogCmp)
		case a.showInitDialog:
			a.initDialog = updated.(dialog.InitDialogCmp)
		}
		return (*a), c, true, true
	}
	return extractedResult0, extractedResult1, extractedResult2, false
}

func updateAppModelMouseMsgShowPermissionsShowHelpShowQuit(a *appModel) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool, handled bool) {
	if a.showPermissions || a.showHelp || a.showQuit ||
		a.showCommandDialog || a.showInitDialog ||
		a.showFilepicker || a.showThemeDialog || a.showMultiArgumentsDialog || a.isCompacting {
		return (*a), nil, true, true
	}
	return extractedResult0, extractedResult1, extractedResult2, false
}

func updateAppModelMouseMsgButtonTeaMouseButtonLeft(a *appModel, msg *tea.MouseMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool, handled bool) {
	if a.currentPage == page.ChatPage && msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft &&
		msg.Y == lipgloss.Height(a.pages[a.currentPage].View()) {
		cmd := a.footerClick(msg.X)
		return (*a), cmd, true, true
	}
	return extractedResult0, extractedResult1, extractedResult2, false
}
