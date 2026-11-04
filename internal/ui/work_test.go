package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/raulaguila/opcode/internal/session"
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
	require.True(t, strings.HasPrefix(strings.TrimSpace(view), "▁ Pergunta do agente") || strings.Contains(view, "Agent question"))
	require.Contains(t, view, "1 of 2")
	require.Contains(t, view, "1 ☑ Testes")
	require.Contains(t, view, "2 ☐ IDs")
	require.Contains(t, view, "3 ☑ JSON atômico")
	require.Contains(t, view, "✎ Other answer…")
	m.questionKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Len(t, m.question.answers, 1)
	require.Equal(t, []string{"Testes", "JSON atômico"}, m.question.answers[0].Choices)
	require.Equal(t, 1, m.question.index, "a batch moves to its next question")
	require.Equal(t, 0, m.question.cursor, "the next question starts on its first choice")
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
	m.foldPlan = true
	strip := ansi.Strip(m.workView())
	require.Contains(t, strip, "Plan 1/3")
	require.Contains(t, strip, "→ 2. Ligar")
	m.foldPlan = false
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

func TestEscPutsTheQuestionAwayAndAnEmptyEnterBringsItBack(t *testing.T) {
	m := New(nil)
	m.width, m.height = 100, 30
	m.question = &question{q: interaction.Question{ID: "q1", Question: "Qual?", Choices: []string{"A", "B"}}}
	require.Zero(t, m.composerRows())
	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	require.True(t, m.answeringLater())
	require.NotZero(t, m.composerRows())
	m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.False(t, m.answeringLater())
}

func TestUpAndDownWalkTheMessagesYouSent(t *testing.T) {
	m := New(nil)
	m.width, m.height = 100, 30
	m.messages = []message.Message{
		{ID: "1", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "primeira"}}},
		{ID: "2", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "resposta"}}},
		{ID: "3", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "segunda\ncom duas linhas"}}},
	}
	up, down := tea.KeyPressMsg{Code: tea.KeyUp}, tea.KeyPressMsg{Code: tea.KeyDown}
	m.composer.SetValue("rascunho")
	m.key(up)
	require.Equal(t, "segunda\ncom duas linhas", m.composer.Value())
	m.key(up) // the cursor climbs to the first line before going further back
	require.Equal(t, "segunda\ncom duas linhas", m.composer.Value())
	m.key(up)
	require.Equal(t, "primeira", m.composer.Value())
	m.key(up)
	require.Equal(t, "primeira", m.composer.Value(), "the oldest stays")
	m.key(down)
	require.Equal(t, "segunda\ncom duas linhas", m.composer.Value())
	m.key(down)
	require.Equal(t, "rascunho", m.composer.Value(), "past the newest, the draft comes back")
}

func TestRecallSurvivesAConversationWithFewerMessages(t *testing.T) {
	m := New(nil)
	m.width, m.height = 100, 30
	m.recall = 5
	m.messages = []message.Message{{ID: "1", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "só uma"}}}}
	m.key(tea.KeyPressMsg{Code: tea.KeyDown})
	require.Equal(t, "", m.composer.Value())
	m.key(tea.KeyPressMsg{Code: tea.KeyUp})
	require.Equal(t, "só uma", m.composer.Value())
}

func TestTheFooterNamesTheModeWhenTheComposerDoesNot(t *testing.T) {
	m := New(nil)
	m.width, m.height, m.mode = 100, 30, "plan"
	require.Empty(t, m.footerMode(), "the composer's edge names it")
	require.Equal(t, 5, m.composerRows(), "the text with a blank row above and below")
	m.question = &question{q: interaction.Question{ID: "q", Question: "Qual?", Choices: []string{"A"}}, checked: map[int]bool{}}
	require.Contains(t, ansi.Strip(m.footer()), modeLabel("plan")+"  ")
}

func TestContextLimitPointsToCompaction(t *testing.T) {
	require.Equal(t, "The conversation no longer fits the model", failureTitle("context budget exceeded: start a new task"))
	require.Contains(t, cause("context budget exceeded"), "/compact")
	require.Equal(t, "This turn's tool output limit was reached", failureTitle("tool result budget reached (384000 bytes); progress saved"))
	line := contextLine(agent.ContextStatus{EstimatedInputTokens: 700, ReservedOutputTokens: 200, SafetyTokens: 100, ContextWindow: 2000, CompactedMessages: 3}, true)
	require.Equal(t, "Context: 1000 of 2000 tokens (50%) · 3 messages compacted · does not fit: compact or change model", line)
}

func TestPlanShowsByDefaultAndTheArrowFoldsIt(t *testing.T) {
	m := New(nil)
	m.width, m.height = 100, 40
	for i := range 3 {
		m.task.Steps = append(m.task.Steps, workflow.Step{Title: fmt.Sprint("step ", i)})
	}
	m.layout()
	require.Contains(t, ansi.Strip(m.workView()), "step 2", "a new plan is visible without asking")

	arrowRow := func(arrow string) int {
		for y, line := range strings.Split(ansi.Strip(m.View().Content), "\n") {
			if strings.Contains(line, arrow) {
				return y
			}
		}
		t.Fatalf("no %s on screen", arrow)
		return -1
	}
	m.click(m.width-4, arrowRow(planFoldArrow))
	require.True(t, m.foldPlan)
	require.NotContains(t, ansi.Strip(m.workView()), "step 2")
	m.click(m.width-4, arrowRow(planUnfoldArrow))
	require.False(t, m.foldPlan)
}

func TestLongPlanKeepsTheCurrentStepInASmallWindow(t *testing.T) {
	steps := make([]workflow.Step, 14)
	for i := range steps {
		steps[i].Status = "completed"
	}
	steps[8].Status, steps[9].Status = "in_progress", ""
	first, last := planWindow(steps, 3)
	require.Equal(t, []int{7, 10}, []int{first, last})
	steps[13].Status = "in_progress"
	steps[8].Status, steps[9].Status = "completed", "completed"
	first, last = planWindow(steps, 3)
	require.Equal(t, []int{11, 14}, []int{first, last})
	first, last = planWindow(steps[:2], 3)
	require.Equal(t, []int{0, 2}, []int{first, last})
}

func TestFinishedPlanFoldsIntoAReceiptThatStaysOpenable(t *testing.T) {
	m := New(nil)
	m.width, m.height = 100, 40
	step := func(status string) workflow.Task {
		return workflow.Task{Steps: []workflow.Step{{Title: "Ligar", Status: status}}}
	}

	m.setTask(step("in_progress"))
	require.Contains(t, ansi.Strip(m.workView()), "● 1. Ligar")

	m.setTask(step("completed"))
	require.Contains(t, ansi.Strip(m.workView()), "✓ Plan completed · 1/1")

	m.togglePlan()
	require.Contains(t, ansi.Strip(m.workView()), "✓ 1. Ligar", "the receipt opens on request")
	m.setTask(step("completed"))
	require.False(t, m.foldPlan, "polling must not fold it back")

	m.foldPlan = true
	more := step("completed")
	more.Steps = append(more.Steps, workflow.Step{Title: "Nova", Status: "pending"})
	m.setTask(more)
	require.False(t, m.foldPlan, "a new step brings the plan back")

	m.setTask(step("blocked"))
	require.False(t, m.foldPlan, "an unfinished plan stays in view")
}

func TestStoppedTaskSaysSoUntilTheNextRun(t *testing.T) {
	m := New(nil)
	m.width, m.height = 100, 40
	m.task = workflow.Task{Status: "completed", Summary: "pronto"}
	require.Empty(t, m.workView(), "success needs no banner")

	m.task = workflow.Task{Status: "partial", Summary: "faltou\na doc"}
	require.Contains(t, ansi.Strip(m.workView()), "◐ Task partial · faltou a doc")

	m.task.Status = "blocked"
	m.task.Steps = []workflow.Step{{Title: "Ligar", Status: "in_progress"}}
	view := ansi.Strip(m.workView())
	require.Contains(t, view, "● 1. Ligar")
	require.Contains(t, view, "■ Task blocked")
	lines := strings.Split(view, "\n")
	require.Contains(t, lines[0], planFoldArrow, "the arrow keeps the top row")

	m.busy = true
	require.NotContains(t, ansi.Strip(m.workView()), "Task blocked", "a new run replaces it")
}

func TestPlanReachesTheScreenWhenSavedAndWhenAConversationOpens(t *testing.T) {
	_, err := config.Load(t.TempDir(), false)
	require.NoError(t, err)
	ctx := context.Background()
	store := workflow.New(nil, t.TempDir(), workflow.Execute)
	steps := []workflow.Step{{Title: "Ligar", Status: "in_progress"}}
	_, err = store.UpdatePlan(ctx, "saved", "Ligar", steps, []string{"liga"}, nil)
	require.NoError(t, err)

	reopened := New(&app.App{Workflows: store, Messages: emptyMessages{}}, session.Session{ID: "saved"})
	reopened.width, reopened.height = 100, 40
	require.Contains(t, ansi.Strip(reopened.workView()), "1. Ligar", "--resume shows the plan at once")

	m := New(&app.App{Workflows: store, Messages: emptyMessages{}, Sessions: &clearableSessions{}})
	m.width, m.height = 100, 40
	m.openSession(session.Session{ID: "saved"})
	require.Contains(t, ansi.Strip(m.workView()), "1. Ligar", "a conversation at rest has no tick to read it")

	m.session.ID = "live"
	m.task = workflow.Task{}
	_, err = store.UpdatePlan(ctx, "live", "Ligar", steps, []string{"liga"}, nil)
	require.NoError(t, err)
	m.Update(pubsub.Event[message.Message]{Payload: message.Message{ID: "r", SessionID: "live", Role: message.Tool}})
	require.Contains(t, ansi.Strip(m.workView()), "1. Ligar", "no waiting for the next second")
}

func TestFilesChangedLaterDoNotReadAsAStoppedTask(t *testing.T) {
	m := New(nil)
	m.width, m.height = 100, 40
	m.task = workflow.Task{Status: "blocked", Summary: workflow.StaleValidation}
	require.Empty(t, m.workView())
}
