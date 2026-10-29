package page

import (
	"context"
	"encoding/json"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/tui/components/chat"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestQuestionBatchReviewsEditsAndPreservesComposer(t *testing.T) {
	p := workflowPage(t)
	p.editor.Update(chat.RestoreDraftMsg{Text: "rascunho da próxima tarefa"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan string, 1)
	go func() {
		answer, err := p.app.Questions.Ask(ctx, interaction.Question{SessionID: "s", Questions: []interaction.QuestionItem{
			{Question: "Recursos", Type: "multi_choice", Choices: []string{"A", "B", "C"}},
			{Question: "Motivo", Type: "free_text"},
		}})
		if err != nil {
			done <- err.Error()
		} else {
			done <- answer
		}
	}()
	require.Eventually(t, func() bool { p.refreshWorkflow(); return p.workflow.question.ID != "" }, time.Second, time.Millisecond)
	key := func(kind tea.KeyType) { flattenCommands(p.questionKey(tea.KeyMsg{Type: kind})) }
	runeKey := func(value string) {
		flattenCommands(p.questionKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)}))
	}
	key(tea.KeyEnter)
	require.Zero(t, p.workflow.questionIndex, "empty answer must not advance")
	key(tea.KeyDown)
	runeKey(" ")
	key(tea.KeyDown)
	runeKey(" ")
	key(tea.KeyEnter)
	require.Equal(t, 1, p.workflow.questionIndex)
	runeKey("razão inicial")
	key(tea.KeyEnter)
	require.Contains(t, p.workflowView(), config.Text("Revisar", "Review"))
	select {
	case <-done:
		t.Fatal("review advanced without explicit final send")
	default:
	}
	key(tea.KeyShiftTab)
	require.Equal(t, "razão inicial", p.workflow.answer.Value())
	p.workflow.answer.SetValue("razão revisada")
	key(tea.KeyEnter)
	p.SetSize(25, 10)
	require.LessOrEqual(t, lipgloss.Width(p.workflowView()), 25)
	require.LessOrEqual(t, lipgloss.Height(p.workflowView()), p.workflowHeight())
	p.SetSize(80, 24)
	key(tea.KeyEnter)
	var answer interaction.AnswerBatch
	require.NoError(t, json.Unmarshal([]byte(<-done), &answer))
	require.Equal(t, []string{"A", "B"}, answer.Answers[0].Choices)
	require.Equal(t, "razão revisada", answer.Answers[1].Text)
	require.Empty(t, p.workflow.question.ID)
	require.Contains(t, p.editor.View(), "rascunho da próxima tarefa")
	require.Zero(t, p.app.CoderAgent.(*workflowProbe).canceled)
}
