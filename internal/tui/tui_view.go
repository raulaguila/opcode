package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/raulaguila/opcode/internal/tui/layout"
	"github.com/raulaguila/opcode/internal/tui/page"
	"github.com/raulaguila/opcode/internal/tui/styles"
)

// baseView is shared by rendering and mouse hit testing so both use the same
// bounded page dimensions, including while a page is loading or resizing.
func (a appModel) baseView() string {
	pageView := a.pages[a.currentPage].View()
	if a.width > 0 {
		if a.height == 0 {
			pageView = ""
		} else {
			pageView = lipgloss.NewStyle().Width(a.width).Height(a.height).MaxWidth(a.width).MaxHeight(a.height).Render(pageView)
		}
	}
	components := []string{
		pageView,
	}
	if pageView == "" {
		components = nil
	}

	components = append(components, a.statusView())

	return lipgloss.JoinVertical(lipgloss.Top, components...)
}

func (a appModel) View() tea.View {
	appView := a.baseView()

	if a.showFilepicker {
		overlay := a.filepicker.View()
		col, row := a.dialogOrigin(overlay)
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			false,
		)

	}

	// Show compacting status overlay
	if a.isCompacting {
		style := styles.Panel().Padding(1, 2)

		overlay := style.Render("Summarizing\n" + a.compactingMessage)
		col, row := a.dialogOrigin(overlay)
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			false,
		)
	}

	viewShowHelp(&a, &appView)

	if a.showSessionDialog {
		overlay := a.sessionDialog.View()
		col, row := a.dialogOrigin(overlay)
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			false,
		)
	}

	if a.showModelDialog {
		overlay := a.modelDialog.View()
		col, row := a.modelDialogOrigin()
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			false,
		)
	}

	if a.showCommandDialog {
		overlay := a.commandDialog.View()
		col, row := a.dialogOrigin(overlay)
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			false,
		)
	}

	if a.showInitDialog {
		overlay := a.initDialog.View()
		col, row := a.dialogOrigin(overlay)
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			false,
		)
	}

	if a.showThemeDialog {
		overlay := a.themeDialog.View()
		col, row := a.dialogOrigin(overlay)
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			false,
		)
	}

	if a.showMultiArgumentsDialog {
		overlay := a.multiArgumentsDialog.View()
		col, row := a.dialogOrigin(overlay)
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			false,
		)
	}

	if a.showProviderDialog {
		overlay := a.providerDialog.View()
		col, row := a.dialogOrigin(overlay)
		appView = layout.PlaceOverlay(col, row, overlay, appView, false)
	}
	if a.showIntegrationDialog {
		overlay := a.integrationDialog.View()
		col, row := a.dialogOrigin(overlay)
		appView = layout.PlaceOverlay(col, row, overlay, appView, false)
	}
	// Authorization always appears above settings/probes that requested it.
	if a.showPermissions {
		overlay := a.permissions.View()
		col, row := a.dialogOrigin(overlay)
		appView = layout.PlaceOverlay(col, row, overlay, appView, false)
	}

	if a.showQuit {
		overlay := a.quit.View()
		col, row := a.dialogOrigin(overlay)
		appView = layout.PlaceOverlay(col, row, overlay, appView, false)
	}
	if a.width > 0 {
		appView = lipgloss.NewStyle().MaxWidth(a.width).MaxHeight(a.height + 1).Render(appView)
	}
	view := tea.NewView(styles.TerminalOutput(appView))
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	view.ReportFocus = true
	return view
}

func (a appModel) modelDialogOrigin() (int, int) {
	return a.dialogOrigin(a.modelDialog.View())
}

func viewShowHelp(a *appModel, appView *string) {
	if a.showHelp {
		bindings := layout.KeyMapToSlice(configuredKeys())
		if p, ok := a.pages[a.currentPage].(layout.Bindings); ok {
			bindings = append(bindings, p.BindingKeys()...)
		}
		if a.showPermissions {
			bindings = append(bindings, a.permissions.BindingKeys()...)
		}
		if a.currentPage == page.LogsPage {
			bindings = append(bindings, logsKeyReturnKey)
		}
		bindings = append(bindings, helpEsc)
		a.help.SetBindings(bindings)

		overlay := a.help.View()
		col, row := a.dialogOrigin(overlay)
		(*appView) = layout.PlaceOverlay(
			col,
			row,
			overlay,
			(*appView),
			false,
		)
	}
}
