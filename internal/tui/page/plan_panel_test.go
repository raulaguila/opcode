package page

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/tui/components/chat"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestPlanPanelUsesSharedSurfaceProgressAndSecondaryControls(t *testing.T) {
	p := workflowPage(t)
	p.workflow.expanded = true
	p.workflow.task = workflow.Task{Objective: "Fix and verify", Mode: workflow.Execute, Steps: []workflow.Step{
		{Title: "Inspect", Status: "completed"}, {Title: "Verify", Status: "in_progress"}, {Title: "Report", Status: "pending"},
	}}
	p.SetSize(60, 20)
	view := ansi.Strip(p.workflowView())
	require.Contains(t, view, "┌")
	require.Contains(t, view, config.Text("Plano 1/3", "Plan 1/3"))
	require.Contains(t, view, "✓ 1. Inspect")
	require.Contains(t, view, "→ 2. Verify")
	require.Contains(t, view, "○ 3. Report")
	require.Contains(t, view, "/task")
	require.NotContains(t, view, "Session:")
	require.NotContains(t, view, "Execute")
	require.Contains(t, p.taskBody(), config.Text("Sessão: s", "Session: s"), "session identity remains available in task details")
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		if strings.Contains(line, "Alt+↑/↓") {
			require.Contains(t, lines[i-1], strings.Repeat("─", 20))
		}
	}
}

func TestPlanPanelResizeScrollAndPendingQuestionPreserveDraft(t *testing.T) {
	p := workflowPage(t)
	p.workflow.expanded, p.workflow.offset = true, -1
	p.workflow.task.Objective = "Long plan"
	for i := range 20 {
		status := "pending"
		if i == 15 {
			status = "in_progress"
		}
		p.workflow.task.Steps = append(p.workflow.task.Steps, workflow.Step{Title: fmt.Sprintf("Step %d ação 漢字", i+1), Status: status})
	}
	p.editor.Update(chat.RestoreDraftMsg{Text: "Draft remains ação 漢字"})
	for _, size := range [][2]int{{120, 40}, {60, 20}, {24, 10}, {20, 8}} {
		p.SetSize(size[0], size[1])
		view := ansi.Strip(p.workflowView())
		require.Equal(t, p.workflowHeight(), lipgloss.Height(view))
		require.LessOrEqual(t, lipgloss.Width(view), size[0])
		require.Contains(t, view, "→ 16.", "the active step remains visible when the panel is small")
		draft, _ := p.composer.(chat.DraftControl).Draft()
		require.Equal(t, "Draft remains ação 漢字", draft)
	}
	p.SetSize(60, 20)
	p.workflow.offset = 0
	require.Contains(t, p.workflowView(), "Long plan", "scrolling can reach the objective and earlier steps")
	p.workflow.question = interaction.Question{ID: "q", Question: "Choose the next action", Choices: []string{"Continue", "Stop"}}
	view := ansi.Strip(p.workflowView())
	require.Contains(t, view, config.Text("Pergunta para você", "Question for you"))
	require.NotContains(t, view, config.Text("Plano 0/20", "Plan 0/20"), "questions retain priority over the expanded plan")
}

func TestPlanPanelHighlightAndLegendKeepThemeContrastAndNoColorMarkers(t *testing.T) {
	profile, name, dark := theme.ColorProfile(), theme.CurrentThemeName(), theme.HasDarkBackground()
	t.Cleanup(func() { theme.SetColorProfile(profile); _ = theme.PreviewTheme(name); theme.SetHasDarkBackground(dark) })
	p := workflowPage(t)
	p.SetSize(80, 24)
	p.workflow.expanded = true
	p.workflow.task = workflow.Task{Objective: "Task", Steps: []workflow.Step{{Title: "ACTIVE_STEP", Status: "in_progress"}}}
	for _, dark := range []bool{false, true} {
		theme.SetHasDarkBackground(dark)
		for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.Ascii} {
			theme.SetColorProfile(profile)
			view := p.workflowView()
			actual := panelHintGlyph(t, view, "Alt+↑/↓")
			expected := panelHintGlyph(t, styles.PanelHint().Render("legend"), "legend")
			require.Equal(t, expected.FG, actual.FG)
			require.Equal(t, expected.BG, actual.BG)
			active := panelHintGlyph(t, view, "ACTIVE_STEP")
			selected := panelHintGlyph(t, styles.SelectedRow().Bold(true).Render("selected"), "selected")
			require.Equal(t, selected.FG, active.FG)
			require.Equal(t, selected.BG, active.BG)
			require.Contains(t, ansi.Strip(view), "→ 1. ACTIVE_STEP", "active state is readable without color")
		}
	}
}
