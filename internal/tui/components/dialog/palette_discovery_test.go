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

func TestPalettePositionShowsFilteredCountAndContinuation(t *testing.T) {
	for _, size := range [][2]int{{36, 10}, {60, 20}, {80, 24}} {
		m := NewCommandDialogCmp().(*commandDialogCmp)
		items := []Command{}
		for i := range 24 {
			items = append(items, Command{ID: fmt.Sprintf("action-%02d", i), Title: fmt.Sprintf("Action %02d", i)})
		}
		m.SetCommands(items)
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := ansi.Strip(m.View())
		require.Contains(t, view, config.Text("1 de 24 · ↓", "1 of 24 · ↓"))
		for range 23 {
			m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		}
		view = ansi.Strip(m.View())
		require.Contains(t, view, config.Text("24 de 24 · ↑", "24 of 24 · ↑"))
		require.LessOrEqual(t, lipgloss.Height(view), size[1])
		assertVisibleCommandClicks(t, m, items, view)
		m.Update(tea.PasteMsg{Content: "action-23"})
		view = ansi.Strip(m.View())
		require.Contains(t, view, config.Text("1 de 1", "1 of 1"))
		require.NotContains(t, view, " · ↓")
		require.NotContains(t, view, " · ↑")
	}
}

func TestApprovalConsequencesExplainEffectiveReadOnlyMode(t *testing.T) {
	settingsDialog(t)
	previous := config.Get().TUI.Language
	t.Cleanup(func() { config.Get().TUI.Language = previous })
	for _, language := range []string{"pt", "en"} {
		config.Get().TUI.Language = language
		for _, mode := range []string{"mode-ask", "mode-plan", "mode-execute"} {
			m := NewCommandDialogCmp().(*commandDialogCmp)
			m.SetCommands([]Command{{ID: mode}, {ID: "approval-project_edits"}})
			m.SetContext("Work mode", mode)
			m.SetApprovalContext("approval-project_edits")
			m.FocusCommand("approval-project_edits")
			m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
			view := ansi.Strip(m.View())
			readOnly := config.Text("Somente leitura neste modo", "Read only in this mode")
			if mode == "mode-execute" {
				require.NotContains(t, view, readOnly)
			} else {
				require.Contains(t, strings.Join(m.detailLines(52), " "), readOnly)
			}
			_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			require.Equal(t, "approval-project_edits", cmd().(CommandSelectedMsg).Command.ID)
		}
	}
}
