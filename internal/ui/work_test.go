package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestQuestionCardNumbersChoicesAndMarksSelection(t *testing.T) {
	m := New(nil)
	m.width, m.height = 100, 30
	m.question = &question{q: interaction.Question{ID: "q", Questions: []interaction.QuestionItem{
		{ID: "a", Question: "Quais melhorias?", Type: "multi_choice", Choices: []string{"Testes", "IDs", "JSON atômico"}},
		{ID: "b", Question: "Quando?", Type: "single_choice", Choices: []string{"Agora", "Depois"}},
	}}, cursor: -1, checked: map[int]bool{}}
	m.questionKey(tea.KeyPressMsg{Code: '1', Text: "1"})
	m.questionKey(tea.KeyPressMsg{Code: '3', Text: "3"})
	view := ansi.Strip(m.questionView())
	require.True(t, strings.HasPrefix(strings.TrimSpace(view), "╭─ Pergunta do agente") || strings.Contains(view, "Agent question"))
	require.Contains(t, view, "1 of 2")
	require.Contains(t, view, "1 ☑ Testes")
	require.Contains(t, view, "2 ☐ IDs")
	require.Contains(t, view, "3 ☑ JSON atômico")
	require.Contains(t, view, "✎ Other answer…")
	m.questionKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Len(t, m.question.answers, 1)
	require.Equal(t, []string{"Testes", "JSON atômico"}, m.question.answers[0].Choices)
	require.Equal(t, 1, m.question.index, "a batch moves to its next question")
	m.questionKey(tea.KeyPressMsg{Text: "x"})
	require.Equal(t, 2, m.question.cursor, "typing focuses the free-text row")
	require.Equal(t, "x", m.question.text)
}

func TestPlanCardAndStrip(t *testing.T) {
	m := New(nil)
	m.width, m.height = 100, 30
	m.task = workflow.Task{Mode: workflow.Execute, Steps: []workflow.Step{
		{Title: "Ler", Status: "completed"}, {Title: "Ligar", Status: "in_progress"}, {Title: "Testar", Status: "pending"},
	}}
	strip := ansi.Strip(m.workView())
	require.Contains(t, strip, "Plan 1/3")
	require.Contains(t, strip, "→ 2. Ligar")
	m.showPlan = true
	card := ansi.Strip(m.workView())
	require.Contains(t, card, "Plan · 1 of 3 completed")
	require.Contains(t, card, "Execute · task saved")
	require.NotContains(t, card, "%", "the bar alone shows progress")
	require.Contains(t, card, "✓ 1. Ler")
	require.Contains(t, card, "● 2. Ligar")
	require.Contains(t, card, "○ 3. Testar")
}

func TestFailureCardNamesTheCauseAndHidesTheRawError(t *testing.T) {
	m := New(nil)
	m.width, m.height = 100, 30
	m.failure = errors.New("dial tcp 127.0.0.1:11434: connection refused")
	view := ansi.Strip(m.renderTranscript(100))
	require.Contains(t, view, "■ Stopped: Provider unavailable", "the card sits in the conversation")
	require.Contains(t, view, "Resume · Ctrl+R")
	require.NotContains(t, view, "11434", "the raw reason waits for Ctrl+E")
	m.failureKey("ctrl+e")
	require.Contains(t, ansi.Strip(m.failureView()), "connection refused")
	m.failureKey("esc")
	require.Nil(t, m.failure)
}
