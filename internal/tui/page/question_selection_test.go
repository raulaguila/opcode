package page

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/stretchr/testify/require"
)

func TestCustomAnswerIsLastSelectableRowAndRequiresText(t *testing.T) {
	for _, batch := range []bool{false, true} {
		p := workflowPage(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan string, 1)
		q := interaction.Question{SessionID: "s", Question: "Qual banco?", Choices: []string{"SQLite", "Postgres"}}
		if batch {
			q.Questions = []interaction.QuestionItem{{ID: "db", Question: q.Question, Type: "single_choice", Choices: q.Choices}}
			q.Choices = nil
		}
		go func() {
			answer, err := p.app.Questions.Ask(ctx, q)
			if err != nil {
				done <- err.Error()
			} else {
				done <- answer
			}
		}()
		require.Eventually(t, func() bool { p.refreshWorkflow(); return p.workflow.question.ID != "" }, time.Second, time.Millisecond)
		for range 3 {
			p.questionKey(tea.KeyPressMsg{Code: tea.KeyDown})
		}
		require.Equal(t, 2, p.workflow.choice)
		require.True(t, p.workflow.answer.Focused())
		require.Empty(t, p.workflow.answer.Value())
		require.Contains(t, ansi.Strip(p.workflowView()), "› ● ")
		flattenCommands(p.questionKey(tea.KeyPressMsg{Code: tea.KeyEnter}))
		select {
		case value := <-done:
			t.Fatalf("empty custom row submitted %q", value)
		default:
		}
		p.questionKey(tea.KeyPressMsg{Code: []rune("Minha opção 漢字")[0], Text: "Minha opção 漢字"})
		flattenCommands(p.questionKey(tea.KeyPressMsg{Code: tea.KeyEnter}))
		if batch {
			flattenCommands(p.questionKey(tea.KeyPressMsg{Code: tea.KeyEnter}))
		}
		assertCustomAnswerDelivered(t, done, batch)
	}
}

func assertCustomAnswerDelivered(t *testing.T, done <-chan string, batch bool) {
	t.Helper()
	select {
	case value := <-done:
		if !batch {
			require.Equal(t, "Minha opção 漢字", value)
			return
		}
		var answer interaction.AnswerBatch
		require.NoError(t, json.Unmarshal([]byte(value), &answer))
		require.Equal(t, "Minha opção 漢字", answer.Answers[0].Text)
		require.Empty(t, answer.Answers[0].Choices)
	case <-time.After(time.Second):
		t.Fatal("answer not delivered")
	}
}

func TestInactiveTextDraftCannotOverrideSelectedChoice(t *testing.T) {
	p := workflowPage(t)
	p.workflow.answer = textinput.New()
	item := interaction.QuestionItem{ID: "q1", Type: "single_choice", Choices: []string{"A", "B"}}
	// Navigate the actual two-choice question, retaining the typed draft.
	p.workflow.question = interaction.Question{ID: "q", Choices: item.Choices}
	p.workflow.answer.SetValue("draft")
	p.workflow.choice = 2
	p.questionKey(tea.KeyPressMsg{Code: tea.KeyUp})
	require.False(t, p.workflow.answer.Focused())
	require.Equal(t, "draft", p.workflow.answer.Value())
	answer := p.selectedQuestionAnswer(item)
	require.Equal(t, []string{"B"}, answer.Choices)
	require.Empty(t, answer.Text)
	p.questionKey(tea.KeyPressMsg{Code: tea.KeyDown})
	require.Equal(t, "draft", p.selectedQuestionAnswer(item).Text)
}

func TestCustomAnswerExcludesCheckedChoicesAndRestoresBatchFocus(t *testing.T) {
	p := workflowPage(t)
	p.workflow.answer = textinput.New()
	item := interaction.QuestionItem{ID: "q1", Type: "multi_choice", Choices: []string{"A", "B"}}
	p.workflow.selectedChoices = map[int]bool{0: true}
	p.workflow.answer.SetValue("custom")
	p.workflow.choice = 0
	require.Equal(t, []string{"A"}, p.selectedQuestionAnswer(item).Choices)
	p.navigateQuestionAnswer("up", 2)
	answer := p.selectedQuestionAnswer(item)
	require.Equal(t, "custom", answer.Text)
	require.Empty(t, answer.Choices)
	p.workflow.question = interaction.Question{Questions: []interaction.QuestionItem{item}}
	p.workflow.batchAnswers = []interaction.AnswerItem{answer}
	p.restoreBatchAnswer()
	require.Equal(t, 2, p.workflow.choice)
	require.True(t, p.workflow.answer.Focused())
	require.Equal(t, "custom", p.workflow.answer.Value())
}
