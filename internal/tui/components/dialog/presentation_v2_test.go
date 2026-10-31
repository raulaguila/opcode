package dialog

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/stretchr/testify/require"
)

func TestDialogCardsShareSquareFrameAndSeparatedControls(t *testing.T) {
	provider := settingsDialog(t)
	model := catalogDialog(t)
	command := NewCommandDialogCmp()
	command.SetCommands([]Command{{ID: "models", Title: "Models"}})
	integration := NewIntegrationDialogCmp(nil)
	integration.Open()
	sessions := NewSessionDialogCmp()
	sessions.SetSessions([]session.Session{{ID: "s", Title: "Review output"}})
	themes := NewThemeDialogCmp()
	themes.Init()
	for _, item := range []struct {
		name  string
		model util.Model
	}{{"commands", command}, {"models", model}, {"providers", provider}, {"integrations", integration}, {"sessions", sessions}, {"themes", themes}, {"help", NewHelpCmp()}} {
		item.model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		view := ansi.Strip(item.model.View())
		require.Contains(t, view, "┌", item.name)
		require.Contains(t, view, "┘", item.name)
		require.GreaterOrEqual(t, strings.Count(view, strings.Repeat("─", 32)), 3, "top, footer divider and bottom: %s", item.name)
		require.LessOrEqual(t, lipgloss.Width(view), 80, item.name)
		require.LessOrEqual(t, lipgloss.Height(view), 24, item.name)
	}
}

func TestNativePasteEditsFormsAndInvalidatesOnlyCurrentProbe(t *testing.T) {
	provider := settingsDialog(t)
	provider.edit(nil)
	provider.fields[providerNameField].SetValue("")
	provider.testing = true
	provider.Update(tea.PasteMsg{Content: "Named connection"})
	require.Equal(t, "Named connection", provider.fields[providerNameField].Value())
	require.False(t, provider.testing, "native paste invalidates an obsolete probe")
	provider.focus = providerActiveField
	provider.fields[providerActiveField].SetValue("false")
	provider.Update(tea.PasteMsg{Content: "true"})
	require.Equal(t, "false", provider.fields[providerActiveField].Value(), "enum choices remain controlled by selection keys")

	integration := NewIntegrationDialogCmp(nil).(*integrationDialogCmp)
	integration.edit(config.Integration{Kind: "lsp"})
	integration.testing = true
	integration.Update(tea.PasteMsg{Content: "gopls"})
	require.Equal(t, "gopls", integration.fields[0].Value())
	require.False(t, integration.testing)
}

func TestNativeSessionPasteIsTextAndDoesNotTriggerActions(t *testing.T) {
	dialog := NewSessionDialogCmp().(*sessionDialogCmp)
	dialog.SetSessions([]session.Session{{ID: "s", Title: "Review ctrl+r"}})
	dialog.Update(tea.PasteMsg{Content: "ctrl+r"})
	require.False(t, dialog.editing)
	require.Equal(t, "ctrl+r", dialog.query)
	dialog.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	dialog.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	dialog.Update(tea.PasteMsg{Content: "Revisão\nsegura"})
	require.Equal(t, "Revisãosegura", dialog.rename)
}

func TestQuitCardKeepsQuestionButtonsAndDividerOnSingleRows(t *testing.T) {
	settingsDialog(t)
	previous := config.Get().TUI.Language
	t.Cleanup(func() { config.Get().TUI.Language = previous })
	for _, language := range []string{"en", "pt"} {
		t.Run(language, func(t *testing.T) {
			config.Get().TUI.Language = language
			quit := NewQuitCmp().(*quitDialogCmp)
			view := ansi.Strip(quit.View())
			questionText := config.Text("Deseja sair?", question)
			require.Contains(t, view, questionText)
			buttonRow := ""
			for _, line := range strings.Split(view, "\n") {
				if strings.Contains(line, config.Text("Sair", "Quit")) {
					buttonRow = line
				}
			}
			require.Contains(t, buttonRow, config.Text("Continuar", "Continue"), "both choices stay on one row")
			innerWidth := lipgloss.Width(view) - 6
			require.Contains(t, view, strings.Repeat("─", innerWidth), "divider fits inside border and padding")
			require.Equal(t, 7, lipgloss.Height(view), "three content rows, two padding rows, two borders")
			_, cmd := quit.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			require.IsType(t, CloseQuitMsg{}, cmd(), "continue remains the default")
		})
	}
}

func TestDialogFramesDoNotWrapFullWidthFooterDividers(t *testing.T) {
	provider := settingsDialog(t)
	provider.width, provider.height = 80, 24
	provider.note = strings.Repeat("Details ", 20)
	command := NewCommandDialogCmp()
	command.SetCommands([]Command{{ID: "models", Title: "Models"}})
	command.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	themes := NewThemeDialogCmp()
	themes.Init()
	themes.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	initialize := NewInitDialogCmp()
	initialize.SetSize(80, 24)
	help := NewHelpCmp()
	help.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for _, item := range []struct {
		name  string
		view  func() string
		frame int
	}{{"commands", command.View, 6}, {"themes", themes.View, 6}, {"initialize", initialize.View, 6}, {"details", provider.noteView, 6}, {"help", help.View, 4}} {
		t.Run(item.name, func(t *testing.T) {
			view := ansi.Strip(item.view())
			dividers := []string{}
			for _, line := range strings.Split(view, "\n") {
				inside := strings.TrimSpace(strings.Trim(line, "│"))
				if inside != "" && strings.Trim(inside, "─") == "" {
					dividers = append(dividers, inside)
				}
			}
			require.Len(t, dividers, 1, "a footer divider must remain a single line")
			require.Equal(t, lipgloss.Width(view)-item.frame, lipgloss.Width(dividers[0]), "frame width includes border and padding")
			require.LessOrEqual(t, lipgloss.Width(view), 80)
		})
	}
}
