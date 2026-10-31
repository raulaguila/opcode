package tui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/clipboard"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
	"github.com/raulaguila/opcode/internal/tui/page"
	"github.com/raulaguila/opcode/internal/tui/util"
)

func updateAppModelKeyMsg(a *appModel, cmd *tea.Cmd, msg *tea.KeyPressMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, handled bool) {
	if result0, result1, handled := routeRemotePasteKey(a, msg); handled {
		return result0, result1, true
	}
	if result0, result1, handled := routeQuitOrPermissionKey(a, msg); handled {
		return result0, result1, true
	}
	if result0, result1, handled := routeConnectionOrWorkspaceKey(a, cmd, msg); handled {
		return result0, result1, true
	}
	// In the terminal pane control keys belong to the child, including
	// Ctrl+C and Escape. Permission/modal dialogs always keep precedence.
	if result0, result1, handled := routeTerminalOrSelectionKey(a, cmd, msg); handled {
		return result0, result1, true
	}
	// Modal shortcuts belong to the focused dialog. In particular Ctrl+H
	// shows archived sessions and Ctrl+F filters favorite models.
	if result0, result1, handled := routeFocusedHelpKey(a, msg); handled {
		return result0, result1, true
	}
	if result0, result1, handled := routeSessionOrModelKey(a, msg); handled {
		return result0, result1, true
	}
	// If multi-arguments dialog is open, let it handle the key press first
	if a.showMultiArgumentsDialog {
		args, cmd := a.multiArgumentsDialog.Update((*msg))
		a.multiArgumentsDialog = args.(dialog.MultiArgumentsDialogCmp)
		return (*a), cmd, true
	}

	return updateApplicationShortcut(a, msg)
}

func updateApplicationShortcut(a *appModel, msg *tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {

	switch {

	case key.Matches((*msg), keys.Quit):
		return updateAppModelKeyMsgQuit(a)
	case msg.Keystroke() == config.Shortcut("sessions"):
		return updateAppModelKeyMsgSessions(a)
	case msg.Keystroke() == config.Shortcut("commands"):
		return updateAppModelKeyMsgCommands(a)
	case msg.Keystroke() == config.Shortcut("models"):
		return updateAppModelKeyMsgModels(a)
	case msg.Keystroke() == config.Shortcut("theme"):
		return updateAppModelKeyMsgTheme(a)
	case key.Matches((*msg), returnKey) || key.Matches((*msg)):
		if result0, result1, result2, handled := updateAppModelKeyMsgMatches(a, msg); handled {
			return result0, result1, result2
		}
	case key.Matches((*msg), keys.Logs):
		return (*a), a.moveToPage(page.LogsPage), true
	case key.Matches((*msg), keys.Help):
		if a.showQuit {
			return (*a), nil, true
		}
		a.showHelp = !a.showHelp
		return (*a), nil, true
	case key.Matches((*msg), helpEsc):
		if a.showHelp {
			a.showHelp = false
			return (*a), nil, true
		}
	case msg.Keystroke() == config.Shortcut("files"):
		a.showFilepicker = !a.showFilepicker
		a.filepicker.ToggleFilepicker(a.showFilepicker)
		return (*a), nil, true
	}
	return nil, nil, false
}

func updateAppModelKeyMsgMatches(a *appModel, msg *tea.KeyPressMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool, handled bool) {
	if msg.Keystroke() == quitKey {
		if a.currentPage == page.LogsPage {
			return (*a), a.moveToPage(page.ChatPage), true, true
		}
	} else if result0, result1, result2, result3, handled := updateAppModelKeyMsgMatchesFilepickerIsCWDFocused(a); handled {
		return result0, result1, result2, result3
	}
	return extractedResult0, extractedResult1, extractedResult2, false
}

func updateAppModelKeyMsgQuit(a *appModel) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool) {
	if a.pendingProviderNativeOperation() {
		a.showQuit = false
		return (*a), nativeOperationQuitNotice(), true
	}
	a.showQuit = !a.showQuit
	if a.showHelp {
		a.showHelp = false
	}
	if a.showSessionDialog {
		a.showSessionDialog = false
	}
	if a.showCommandDialog {
		a.showCommandDialog = false
	}
	if a.showFilepicker {
		a.showFilepicker = false
		a.filepicker.ToggleFilepicker(a.showFilepicker)
	}
	if a.showModelDialog {
		a.modelDialog.Close()
		a.showModelDialog = false
	}
	if a.showMultiArgumentsDialog {
		a.showMultiArgumentsDialog = false
	}
	return (*a), nil, true

}

func updateAppModelKeyMsgCommands(a *appModel) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool) {
	if a.currentPage == page.ChatPage && !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showThemeDialog && !a.showFilepicker {
		// Show commands dialog
		if len(a.commands) == 0 {
			return (*a), util.ReportWarn(config.Text("Nenhum comando disponível", "No commands available")), true
		}
		a.commandDialog.SetCommands(a.contextualCommands())
		a.showCommandDialog = true
		return (*a), nil, true
	}
	return (*a), nil, true

}

func updateAppModelKeyMsgModels(a *appModel) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool) {
	if a.showModelDialog {
		a.modelDialog.Close()
		a.showModelDialog = false
		return (*a), nil, true
	}
	if a.currentPage == page.ChatPage && !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showCommandDialog {
		a.showModelDialog = true
		return (*a), a.openModelCatalog("", false), true
	}
	return (*a), nil, true

}

func updateAppModelKeyMsgSessions(a *appModel) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool) {
	if a.currentPage == page.ChatPage && !a.showQuit && !a.showPermissions && !a.showCommandDialog {
		return (*a), a.openSessions(), true
	}
	return (*a), nil, true

}

func updateAppModelKeyMsgTheme(a *appModel) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool) {
	if !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showCommandDialog {
		// Show theme switcher dialog
		a.showThemeDialog = true
		// Theme list is dynamically loaded by the dialog component
		return (*a), a.themeDialog.Init(), true
	}
	return (*a), nil, true

}

func routeTerminalOrSelectionKey(a *appModel, cmd *tea.Cmd, msg *tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	if !a.showPermissions && !a.showHelp && !a.showQuit && !a.showSessionDialog &&
		!a.showCommandDialog && !a.showModelDialog && !a.showInitDialog && !a.showFilepicker &&
		!a.showThemeDialog && !a.showMultiArgumentsDialog && !a.showProviderDialog && !a.showIntegrationDialog && !a.isCompacting {
		if focused, ok := a.pages[a.currentPage].(interface{ TerminalFocused() bool }); ok && focused.TerminalFocused() {
			a.pages[a.currentPage], (*cmd) = a.pages[a.currentPage].Update((*msg))
			return (*a), (*cmd), true
		}
		if selection, ok := a.pages[a.currentPage].(interface {
			SelectionKey(tea.KeyPressMsg) (tea.Cmd, bool)
		}); ok {
			if cmd, handled := selection.SelectionKey((*msg)); handled {
				return (*a), cmd, true
			}
		}
	}
	return nil, nil, false
}

func routeConnectionOrWorkspaceKey(a *appModel, cmd *tea.Cmd, msg *tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	if msg.Keystroke() != "ctrl+c" && !a.showPermissions && !a.showHelp && !a.showQuit && !a.showSessionDialog && !a.showCommandDialog && !a.showModelDialog && !a.showInitDialog && !a.showFilepicker && !a.showThemeDialog && !a.showMultiArgumentsDialog {
		if a.showProviderDialog {
			d, c := a.providerDialog.Update((*msg))
			a.providerDialog = d.(dialog.ProviderDialog)
			return (*a), c, true
		}
		if a.showIntegrationDialog {
			d, c := a.integrationDialog.Update((*msg))
			a.integrationDialog = d.(dialog.IntegrationDialog)
			return (*a), c, true
		}
		if focused, ok := a.pages[a.currentPage].(interface{ WorkspaceFocused() bool }); ok && focused.WorkspaceFocused() {
			a.pages[a.currentPage], (*cmd) = a.pages[a.currentPage].Update((*msg))
			return (*a), (*cmd), true
		}
	}
	return nil, nil, false
}

func routeRemotePasteKey(a *appModel, msg *tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	if msg.Keystroke() == "ctrl+v" && clipboard.Remote() {
		terminal := false
		if p, ok := a.pages[a.currentPage].(interface{ TerminalFocused() bool }); ok {
			terminal = p.TerminalFocused()
		}
		_, modal := a.mouseModal()
		if !terminal || modal || a.showProviderDialog || a.showIntegrationDialog || a.showModelDialog || a.showSessionDialog || a.showPermissions || a.showHelp {
			return (*a), util.ReportInfo(config.Text("Use a colagem do terminal (por exemplo Ctrl+Shift+V). O clipboard do servidor SSH não será lido.", "Use terminal paste (for example Ctrl+Shift+V). The SSH server clipboard will not be read.")), true
		}
	}
	return nil, nil, false
}

func routeSessionOrModelKey(a *appModel, msg *tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	if msg.Keystroke() != "ctrl+c" && !a.showPermissions && !a.showQuit && !a.showHelp {
		if a.showSessionDialog {
			updated, c := a.sessionDialog.Update((*msg))
			a.sessionDialog = updated.(dialog.SessionDialog)
			return (*a), c, true
		}
		if a.showModelDialog && msg.Keystroke() != config.Shortcut("models") {
			updated, c := a.modelDialog.Update((*msg))
			a.modelDialog = updated.(dialog.ModelDialog)
			return (*a), c, true
		}
	}
	return nil, nil, false
}

func routeFocusedHelpKey(a *appModel, msg *tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	if a.showHelp && msg.Keystroke() != "ctrl+c" && msg.Keystroke() != "esc" && !key.Matches((*msg), helpEsc) && !key.Matches((*msg), keys.Help) {
		updated, c := a.help.Update((*msg))
		a.help = updated.(dialog.HelpCmp)
		return (*a), c, true
	}
	return nil, nil, false
}

func routeQuitOrPermissionKey(a *appModel, msg *tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	if msg.Keystroke() != "ctrl+c" {
		if a.showQuit {
			updated, c := a.quit.Update((*msg))
			a.quit = updated.(dialog.QuitDialog)
			return (*a), c, true
		}
		if a.showPermissions {
			updated, c := a.permissions.Update((*msg))
			a.permissions = updated.(dialog.PermissionDialogCmp)
			return (*a), c, true
		}
	}
	return nil, nil, false
}

func updateAppModelKeyMsgMatchesFilepickerIsCWDFocused(a *appModel) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool, extractedResult3 bool, handled bool) {
	if !a.filepicker.IsCWDFocused() {
		if a.showQuit {
			a.showQuit = !a.showQuit
			return (*a), nil, true, true, true
		}
		if a.showHelp {
			a.showHelp = !a.showHelp
			return (*a), nil, true, true, true
		}
		if a.showInitDialog {
			a.showInitDialog = false
			// Mark the project as initialized without running the command
			if err := config.MarkProjectInitialized(); err != nil {
				return (*a), util.ReportError(err), true, true, true
			}
			return (*a), nil, true, true, true
		}
		if a.showFilepicker {
			a.showFilepicker = false
			a.filepicker.ToggleFilepicker(a.showFilepicker)
			return (*a), nil, true, true, true
		}
		if a.currentPage == page.LogsPage {
			return (*a), a.moveToPage(page.ChatPage), true, true, true
		}
	}
	return extractedResult0, extractedResult1, extractedResult2, extractedResult3, false
}
