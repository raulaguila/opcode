package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/tui/page"
)

func (a appModel) modalOpen() bool {
	return a.showPermissions || a.showHelp || a.showQuit || a.showSessionDialog || a.showCommandDialog || a.showModelDialog || a.showProviderDialog || a.showIntegrationDialog || a.showInitDialog || a.showFilepicker || a.showThemeDialog || a.showMultiArgumentsDialog || a.isCompacting
}

func (a appModel) syncInputFocus() tea.Cmd {
	if current, ok := a.pages[page.ChatPage].(interface{ SetInputFocused(bool) tea.Cmd }); ok {
		return current.SetInputFocused(a.currentPage == page.ChatPage && !a.modalOpen() && !a.unfocused)
	}
	return nil
}
