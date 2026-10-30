package page

import (
	"context"
	"encoding/json"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/tui/components/chat"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

func TestQuestionPanelSeparatesQuestionChoicesAndControls(t *testing.T) {
	for _, batch := range []bool{false, true} {
		p := workflowPage(t)
		p.SetSize(60, 20)
		p.workflow.question = interaction.Question{ID: "q", Question: "Which improvement should we implement first for this repository?", Choices: []string{"Now", "Later"}}
		if batch {
			p.workflow.question.Questions = []interaction.QuestionItem{{Question: p.workflow.question.Question, Type: "single_choice", Choices: p.workflow.question.Choices}}
		}
		p.workflow.choice = -1
		p.workflow.batchAnswers = make([]interaction.AnswerItem, 1)
		view := ansi.Strip(p.workflowView())
		require.Contains(t, view, "┌", "question needs a square boundary without depending on color")
		require.Contains(t, view, config.Text("Pergunta para você", "Question for you"))
		require.NotContains(t, view, config.Text("Aguardando sua resposta", "Waiting for your answer"), "the footer already exposes the waiting state")
		require.Contains(t, view, "repository?", "question wraps instead of silently losing its end")
		require.Contains(t, view, config.Text("Outra resposta", "Other answer"))
		require.NotContains(t, view, "› ○", "a choice must not be selected without input")
		require.LessOrEqual(t, lipgloss.Width(view), 60)
		require.LessOrEqual(t, lipgloss.Height(view), p.workflowHeight())
		lines := strings.Split(view, "\n")
		for i, line := range lines {
			if strings.Contains(line, "○ Now") {
				require.Equal(t, "│", strings.TrimSpace(lines[i-1][:strings.LastIndex(lines[i-1], "│")]), "a blank row separates question and choices")
			}
			if strings.Contains(line, "↑↓") {
				require.Contains(t, lines[i-1], strings.Repeat("─", 20), "a rule separates the choices from keyboard controls")
			}
		}
	}
}

func TestQuestionPanelDetailsAndSmallSizesKeepDraft(t *testing.T) {
	p := workflowPage(t)
	p.editor.Update(chat.RestoreDraftMsg{Text: "draft remains"})
	p.workflow.question = interaction.Question{ID: "q", Question: strings.Repeat("Long question 漢字 ", 30), Choices: []string{"First", "Second", "Third"}}
	p.workflow.choice = 2
	for _, size := range [][2]int{{60, 20}, {25, 10}, {20, 8}} {
		p.SetSize(size[0], size[1])
		for _, details := range []bool{false, true} {
			p.workflow.details = details
			view := p.workflowView()
			require.LessOrEqual(t, lipgloss.Width(view), size[0])
			require.LessOrEqual(t, lipgloss.Height(view), p.workflowHeight())
			require.Contains(t, p.editor.View(), "draft remains")
		}
	}
}

func TestQuestionPanelFocusedChoiceAndFullDetailsRemainReachable(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	p := workflowPage(t)
	p.SetSize(60, 20)
	p.workflow.question = interaction.Question{ID: "q", Question: "Choose the next improvement"}
	for i := range 20 {
		p.workflow.question.Choices = append(p.workflow.question.Choices, fmt.Sprintf("Option %d", i+1))
	}
	p.workflow.question.Choices[19] += strings.Repeat(" long option", 12) + " LASTOPTION"
	p.workflow.choice = 19
	view := ansi.Strip(p.workflowView())
	require.Contains(t, view, "› ● Option 20")
	p.questionKey(tea.KeyMsg{Type: tea.KeyF2})
	_ = p.workflowView()
	for range 20 {
		p.questionKey(tea.KeyMsg{Type: tea.KeyPgDown})
	}
	require.Contains(t, ansi.Strip(p.workflowView()), "LASTOPTION")
	require.Equal(t, 19, p.workflow.choice)
	p.questionKey(tea.KeyMsg{Type: tea.KeyEsc})
	require.False(t, p.workflow.details)
	require.Zero(t, p.app.CoderAgent.(*workflowProbe).canceled, "Esc in details only returns to the question")
}

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
