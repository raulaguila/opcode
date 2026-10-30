package page

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/stretchr/testify/require"
)

func TestQuestionCardDividerSeparatesControlsInEveryAnswerView(t *testing.T) {
	before, dark := lipgloss.ColorProfile(), lipgloss.HasDarkBackground()
	t.Cleanup(func() { lipgloss.SetColorProfile(before); lipgloss.SetHasDarkBackground(dark) })
	for _, variant := range []struct {
		profile termenv.Profile
		dark    bool
	}{{termenv.Ascii, true}, {termenv.Ascii, false}, {termenv.TrueColor, true}, {termenv.TrueColor, false}} {
		lipgloss.SetColorProfile(variant.profile)
		lipgloss.SetHasDarkBackground(variant.dark)
		for _, kind := range []string{"choices", "text", "review", "details"} {
			p := workflowPage(t)
			p.SetSize(90, 30)
			p.workflow.question = interaction.Question{ID: "q", Question: "Choose next step", Choices: []string{"Keep diagnosis", "Plan changes"}}
			p.workflow.choice = 0
			switch kind {
			case "text":
				p.workflow.question.Choices = nil
				p.workflow.answer.SetValue("Custom response")
			case "review":
				p.workflow.question.Questions = []interaction.QuestionItem{{ID: "first", Question: "Next step"}}
				p.workflow.questionIndex = 1
				p.workflow.batchAnswers = []interaction.AnswerItem{{QuestionID: "first", Text: "Keep diagnosis"}}
			case "details":
				p.workflow.details = true
			}
			view := ansi.Strip(p.workflowView())
			lines := strings.Split(view, "\n")
			require.Contains(t, lines[0], "┌")
			require.Contains(t, lines[len(lines)-1], "┘")
			require.Contains(t, lines[len(lines)-3], strings.Repeat("─", 60), kind)
			require.NotContains(t, lines[len(lines)-2], "─", "keyboard controls are below the divider")
			require.LessOrEqual(t, lipgloss.Width(view), p.width)
			require.LessOrEqual(t, lipgloss.Height(view), p.workflowHeight())
			if kind == "choices" || kind == "details" {
				require.Contains(t, view, "› ● Keep diagnosis", "selection remains legible without color")
			}
			if kind == "text" {
				require.Contains(t, view, "Custom response")
			}
		}
	}
}

func TestQuestionCardDoesNotRefocusInactiveCustomAnswer(t *testing.T) {
	for _, batch := range []bool{false, true} {
		p := workflowPage(t)
		p.workflow.answer = textinput.New()
		p.inputEnabled = true
		p.workflow.question = interaction.Question{ID: "q", Question: "Next step?", Choices: []string{"Keep diagnosis", "Plan changes"}}
		if batch {
			p.workflow.question.Questions = []interaction.QuestionItem{{Question: "Next step?", Choices: p.workflow.question.Choices}}
		}
		for _, choice := range []int{-1, 0, 2, 1, 2} {
			p.workflow.choice = choice
			p.syncInputFocus()
			require.Equal(t, choice == 2, p.workflow.answer.Focused(), "focus follows the active answer row")
			p.syncInputFocus()
			require.Equal(t, choice == 2, p.workflow.answer.Focused(), "subsequent UI events must not re-enable the inactive cursor")
		}
		p.workflow.details = true
		p.syncInputFocus()
		require.False(t, p.workflow.answer.Focused())
		if batch {
			p.workflow.details = false
			p.workflow.questionIndex = 1
			p.syncInputFocus()
			require.False(t, p.workflow.answer.Focused(), "review does not focus an answer field")
		}
	}
}
