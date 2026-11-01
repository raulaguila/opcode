package page

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/stretchr/testify/require"
)

func TestQuestionDigitsFocusChoicesButStayTextInCustomAnswer(t *testing.T) {
	p := workflowPage(t)
	p.SetSize(80, 24)
	p.workflow.question = interaction.Question{ID: "q", Question: "Pick", Choices: []string{"Now", "Later"}}
	p.workflow.choice = -1
	p.questionKey(tea.KeyPressMsg{Code: '2', Text: "2"})
	require.Equal(t, 1, p.workflow.choice)
	_, ok := p.jumpQuestionChoice("3", 2)
	require.False(t, ok, "a digit beyond the choices is ordinary text")
	p.workflow.choice = 2 // the custom answer row
	_, ok = p.jumpQuestionChoice("1", 2)
	require.False(t, ok, "digits are text while typing a custom answer")
}
