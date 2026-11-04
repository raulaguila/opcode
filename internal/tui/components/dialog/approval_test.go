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

func TestApprovalChooserSeparatesPoliciesAndPreservesVisibleMouseTargets(t *testing.T) {
	items := []Command{{ID: "mode-ask", Title: "Ask"}, {ID: "mode-plan", Title: "Plan"}, {ID: "mode-execute", Title: "Execute"}, {ID: "approval-prompt", Title: "Ask for approval"}, {ID: "approval-sandboxed", Title: "Allow project edits"}}
	for _, size := range [][2]int{{60, 20}, {80, 24}, {120, 36}, {36, 10}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := NewCommandDialogCmp().(*commandDialogCmp)
			m.SetCommands(items)
			m.SetContext("Work mode", "mode-execute")
			m.SetApprovalContext("approval-prompt")
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
			view := ansi.Strip(m.View())
			require.LessOrEqual(t, lipgloss.Width(view), size[0])
			require.LessOrEqual(t, lipgloss.Height(view), size[1])
			require.Contains(t, view, config.Text("Aprovação", "Approval"))
			require.Contains(t, view, "✓ "+config.Text("Confirmar tudo", "Confirm all"))
			for y, line := range strings.Split(view, "\n") {
				if strings.Contains(line, "✓ "+config.Text("Confirmar tudo", "Confirm all")) {
					_, cmd := m.Update(tea.MouseClickMsg{X: 3, Y: y, Button: tea.MouseLeft})
					require.NotNil(t, cmd)
					require.Equal(t, "approval-prompt", cmd().(CommandSelectedMsg).Command.ID)
				}
			}
			m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
			_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			require.Equal(t, "approval-sandboxed", cmd().(CommandSelectedMsg).Command.ID)
			m.SetCommands(items[:3])
			require.Empty(t, m.approvalID, "palette reopening does not carry policy markers")
		})
	}
}
