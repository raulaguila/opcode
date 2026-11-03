package transcript

import (
	"encoding/json"
	"testing"

	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

func questionResult(t *testing.T, answer string) message.ToolResult {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"user_response": answer})
	require.NoError(t, err)
	return message.ToolResult{ToolCallID: "ask", Content: string(raw)}
}

func TestAnsweredQuestionsUsesOnlyTheActualMatchingResponse(t *testing.T) {
	call := message.ToolCall{ID: "ask", Name: "ask_user_question", Input: `{"question":"Qual banco?","choices":["Postgres","SQLite"]}`}
	result := questionResult(t, "SQLite")
	require.Equal(t, []Decision{{"Qual banco?", "SQLite"}}, AnsweredQuestions(call, result))
	result.IsError = true
	require.Empty(t, AnsweredQuestions(call, result))
	result.IsError = false
	result.ToolCallID = "other"
	require.Empty(t, AnsweredQuestions(call, result))
	result.ToolCallID = "ask"
	result.Content = `{"error":"cancelled"}`
	require.Empty(t, AnsweredQuestions(call, result))
	call.Input = `{"question":false}`
	require.Empty(t, AnsweredQuestions(call, questionResult(t, "SQLite")))
}

func TestAnsweredBatchRestoresOrderAndImplicitIDs(t *testing.T) {
	call := message.ToolCall{ID: "ask", Name: "ask_user_questions", Input: `{"questions":[{"question":"Ativar?","type":"yes_no"},{"id":"db","question":"Qual banco?","type":"single_choice","choices":["Postgres","SQLite"]},{"question":"Observação?","type":"free_text"}]}`}
	answer := `{"answers":[{"question_id":"db","choices":["SQLite"]},{"question_id":"q3","text":"Apenas local"},{"question_id":"q1","choices":["yes"]}]}`
	require.Equal(t, []Decision{{"Ativar?", "yes"}, {"Qual banco?", "SQLite"}, {"Observação?", "Apenas local"}}, AnsweredQuestions(call, questionResult(t, answer)))
	for _, invalid := range []string{`{"answers":[]}`, `{"answers":[{"question_id":"wrong","text":"Sim"}]}`, `not JSON`} {
		require.Empty(t, AnsweredQuestions(call, questionResult(t, invalid)))
	}
}
