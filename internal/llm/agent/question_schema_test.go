package agent

import (
	"context"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestGroupedQuestionsLoadProgressivelyAndWaitForHumanReview(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Ask)
	a.questions.SetInteractive(true)
	done, err := a.Run(context.Background(), "s", "Preciso decidir os recursos antes de continuar.")
	require.NoError(t, err)
	first := nextGeneration(t, p)
	require.NotContains(t, toolNames(first), "ask_user_questions", "initial schemas must stay small")
	replyTools(first, toolCall("request_tools", `{"names":["ask_user_questions"]}`))
	next := nextGeneration(t, p)
	require.Contains(t, toolNames(next), "ask_user_questions")
	replyTools(next, toolCall("ask_user_questions", `{"questions":[{"id":"features","type":"multi_choice","question":"Quais recursos?","choices":["A","B"]},{"id":"reason","type":"free_text","question":"Qual motivo?"}]}`))
	var question interaction.Question
	require.Eventually(t, func() bool { var ok bool; question, ok = a.questions.Pending("s"); return ok }, time.Second, time.Millisecond)
	select {
	case result := <-done:
		t.Fatalf("answered before human review: %+v", result)
	default:
	}
	require.Error(t, a.questions.AnswerBatch("s", question.ID, []interaction.AnswerItem{{QuestionID: "features", Choices: []string{"A"}}}))
	require.NoError(t, a.questions.AnswerBatch("s", question.ID, []interaction.AnswerItem{{QuestionID: "features", Choices: []string{"A", "B"}}, {QuestionID: "reason", Text: "Quero ambos"}}))
	final := nextGeneration(t, p)
	require.Contains(t, final.history[len(final.history)-1].ToolResults()[0].Content, "Quero ambos")
	final.reply <- provider.ProviderResponse{Content: "As escolhas foram registradas.", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
	task, err := a.workflow.Get(context.Background(), "s")
	require.NoError(t, err)
	require.False(t, task.Changed)
}
