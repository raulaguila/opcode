package dialog

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCommandVisibleRowsAndMouseShareCompactGeometry(t *testing.T) {
	for _, size := range [][2]int{{36, 8}, {36, 10}, {36, 14}, {80, 14}, {80, 24}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := NewCommandDialogCmp().(*commandDialogCmp)
			var items []Command
			for i := range 30 {
				items = append(items, Command{ID: fmt.Sprint(i), Title: fmt.Sprintf("Action %02d", i)})
			}
			m.SetCommands(items)
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			for range 22 {
				m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
			}
			view := ansi.Strip(m.View())
			require.LessOrEqual(t, lipgloss.Width(view), size[0])
			require.LessOrEqual(t, lipgloss.Height(view), size[1])
			assertVisibleCommandClicks(t, m, items, view)
			_, cmd := m.Update(tea.MouseClickMsg{X: size[0] + 1, Y: m.geometry().firstRow, Button: tea.MouseLeft})
			require.Nil(t, cmd)
		})
	}
}

func TestPaletteIsActionFirstAndModeConsequenceIsLocalized(t *testing.T) {
	settingsDialog(t)
	previous := config.Get().TUI.Language
	t.Cleanup(func() { config.Get().TUI.Language = previous })
	for _, lang := range []string{"pt", "en"} {
		config.Get().TUI.Language = lang
		m := NewCommandDialogCmp().(*commandDialogCmp)
		m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
		m.SetCommands([]Command{{ID: "settings", Title: "Settings", Description: "Providers and preferences"}})
		require.Contains(t, ansi.Strip(m.View()), config.Text("Configurações", "Settings"), "known stored English labels use the explicitly chosen locale")

		m.SetCommands([]Command{{ID: "resume", Title: "Resume"}, {ID: "settings", Title: "Settings"}, {ID: "new", Title: "New"}})
		require.Equal(t, "resume", m.listView.GetItems()[0].ID, "contextual ordering is stable")
		require.NotContains(t, ansi.Strip(m.View()), "Settings · Settings")
		row := ansi.Strip(m.commandRow(Command{ID: "settings", Title: "Settings"}, false, 64))
		require.True(t, strings.HasPrefix(row, "  Settings"), "action comes before secondary category")
		m.SetCommands([]Command{{ID: "mode-ask", Title: "Ask"}, {ID: "mode-plan", Title: "Plan"}, {ID: "mode-execute", Title: "Execute"}})
		m.SetContext("Work mode", "mode-ask")
		m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		view := ansi.Strip(m.View())
		require.Contains(t, view, "✓ "+config.Text("Perguntar", "Ask"))
		require.NotContains(t, view, "✓ "+config.Text("Executar", "Execute"))
		require.Contains(t, view, config.Text("solicita as permissões necessárias", "requests the required permissions"))
		require.NotContains(t, view, "/mode-execute")
	}
}

func assertVisibleCommandClicks(t *testing.T, m *commandDialogCmp, items []Command, view string) {
	t.Helper()

	visible := 0
	for y, line := range strings.Split(view, "\n") {
		for _, item := range items {
			if strings.Contains(line, item.Title) {
				_, cmd := m.Update(tea.MouseClickMsg{X: 3, Y: y, Button: tea.MouseLeft})
				require.NotNil(t, cmd, "visible action %s", item.ID)
				require.Equal(t, item.ID, cmd().(CommandSelectedMsg).Command.ID)
				visible++
			}
		}
	}
	require.Greater(t, visible, 0)
}
