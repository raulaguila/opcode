package provider

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

func TestOllamaOutputLimitRejectsCallsAndPreservesExplicitBudget(t *testing.T) {
	for _, name := range []string{"inspect", "final_answer", "answer_with_sources"} {
		t.Run(name, func(t *testing.T) {
			opts := testOptions(models.ProviderLocal)
			opts.maxTokens, opts.model.ContextWindow = 256, 65536
			c := newOllamaClient(opts, "http://local.invalid/v1")
			body := fmt.Sprintf(`{"message":{"tool_calls":[{"function":{"name":%q,"arguments":{}}}]},"done":true,"done_reason":"length","eval_count":256}`, name)
			c.transport.client = wireHTTP(t, 200, "application/x-ndjson", body, func(_ *http.Request, request map[string]any) {
				options := request["options"].(map[string]any)
				require.EqualValues(t, 256, options["num_predict"])
				require.EqualValues(t, 65536, options["num_ctx"])
			})
			response, err := c.send(context.Background(), userMessages(), []tools.BaseTool{contractTool{}})
			require.ErrorIs(t, err, ErrOutputLimit)
			require.Nil(t, response)
			events := collectEvents(t, c.stream(context.Background(), userMessages(), []tools.BaseTool{contractTool{}}))
			require.NotEmpty(t, events)
			require.Equal(t, EventError, events[len(events)-1].Type)
			for _, event := range events {
				require.NotEqual(t, EventComplete, event.Type)
				require.Nil(t, event.ToolCall, "an incomplete call must never reach execution")
				if event.Type == EventError {
					require.ErrorIs(t, event.Error, ErrOutputLimit)
				}
			}
		})
	}
}

func TestOllamaOutputLimitPreservesPlainPartialText(t *testing.T) {
	value := ollamaResponse{Done: true, DoneReason: "length", EvalCount: 256}
	value.Message.Content = "partial answer"
	response, err := ollamaResult(value, nil)
	require.NoError(t, err)
	require.Equal(t, "partial answer", response.Content)
	require.Equal(t, message.FinishReasonMaxTokens, response.FinishReason)
}
