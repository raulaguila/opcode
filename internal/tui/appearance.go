package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/tui/util"
)

func (a *appModel) appearanceMessage(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		theme.SetHasDarkBackground(msg.IsDark())
	case tea.ColorProfileMsg:
		theme.SetColorProfile(msg.Profile)
	case tea.KeyboardEnhancementsMsg:
		return a.deliverEvent(util.EnhancedKeyboardMsg{Supported: msg.SupportsKeyDisambiguation()}, nil), true
	default:
		return nil, false
	}
	return a.deliverEvent(dialog.ThemeChangedMsg{ThemeName: theme.CurrentThemeName(), Preview: true}, nil), true
}

// Paste is text input, never a global shortcut or an implicit permission answer.
func (a *appModel) pasteMessage(msg tea.PasteMsg) tea.Cmd {
	if a.showPermissions || a.showQuit || a.showHelp || a.isCompacting {
		return nil
	}
	routes := []struct {
		show  bool
		route dialogRoute
	}{
		{a.showProviderDialog, routeProvider}, {a.showIntegrationDialog, routeIntegration},
		{a.showFilepicker, routeFilepicker}, {a.showSessionDialog, routeSessions},
		{a.showCommandDialog, routeCommands}, {a.showModelDialog, routeModels},
		{a.showInitDialog, routeInit}, {a.showThemeDialog, routeTheme},
	}
	for _, target := range routes {
		if target.show {
			return a.updateDialog(target.route, msg)
		}
	}
	if a.showMultiArgumentsDialog {
		next, cmd := a.multiArgumentsDialog.Update(msg)
		a.multiArgumentsDialog = next.(dialog.MultiArgumentsDialogCmp)
		return cmd
	}
	next, cmd := a.pages[a.currentPage].Update(msg)
	a.pages[a.currentPage] = next
	return cmd
}
