package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

func TestChatPlainTextUsesStringAndKeepsVisionParts(t *testing.T) {
	c := chatClient(t, testOptions(models.ProviderOpenAI), 200, "application/json", `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`, func(_ *http.Request, body map[string]any) {
		messages := body["messages"].([]any)
		require.Equal(t, "inspect", messages[1].(map[string]any)["content"], "text-only compatible servers need no multimodal array")
	})
	_, err := c.send(context.Background(), userMessages(), nil)
	require.NoError(t, err)
	history := userMessages()
	history[0].Parts = append(history[0].Parts, message.BinaryContent{MIMEType: "image/png", Data: budgetPNG(t, 1, 0)})
	converted := c.convertMessages(history)
	parts, ok := converted[1]["content"].([]object)
	require.True(t, ok)
	require.Len(t, parts, 2)
	require.Equal(t, "text", parts[0]["type"])
	require.Equal(t, "image_url", parts[1]["type"])
}

func TestChatOutputLimitIsTypedAndNeverCompletesCalls(t *testing.T) {
	opts := testOptions(models.ProviderOpenAI)
	opts.maxTokens = 256
	call := `{"id":"call-a","type":"function","function":{"name":"inspect","arguments":"{\"path\":"}}`
	payload := fmt.Sprintf(`{"choices":[{"index":0,"message":{"tool_calls":[%s]},"delta":{"tool_calls":[%s]},"finish_reason":"length"}]}`, call, call)
	c := chatClient(t, opts, 200, "application/json", payload, func(_ *http.Request, body map[string]any) { require.EqualValues(t, 256, body["max_completion_tokens"]) })
	response, err := c.send(context.Background(), userMessages(), []tools.BaseTool{contractTool{}})
	require.Nil(t, response)
	require.ErrorIs(t, err, ErrOutputLimit)
	c = chatClient(t, opts, 200, "text/event-stream", sseFrames(payload, `[DONE]`), nil)
	assertOpenAIStreamRejected(t, c, ErrOutputLimit)
}

func TestChatRejectsNonObjectArgumentsAndMissingCalls(t *testing.T) {
	for _, args := range []string{`[]`, `null`, `42`, `true`, `"{}"`} {
		var call chatCall
		call.ID = "call-a"
		call.Function.Name = "inspect"
		call.Function.Arguments = args
		_, err := chatResultFrom(chatMessage{ToolCalls: []chatCall{call}}, "tool_calls", chatUsage{})
		require.Error(t, err, args)
	}
	_, err := chatResultFrom(chatMessage{}, "tool_calls", chatUsage{})
	require.Error(t, err)
	var call chatCall
	call.ID = "call-a"
	call.Function.Name = "inspect"
	call.Function.Arguments = ` {"number":9007199254740993} `
	result, err := chatResultFrom(chatMessage{ToolCalls: []chatCall{call}}, "tool_calls", chatUsage{})
	require.NoError(t, err)
	require.Equal(t, call.Function.Arguments, result.ToolCalls[0].Input)
}

func TestChatPartialTextAndFilteredOutput(t *testing.T) {
	response, err := chatResultFrom(chatMessage{Content: "partial"}, "length", chatUsage{})
	require.NoError(t, err)
	require.Equal(t, "partial", response.Content)
	require.Equal(t, message.FinishReasonMaxTokens, response.FinishReason)
	_, err = chatResultFrom(chatMessage{}, "length", chatUsage{})
	require.ErrorIs(t, err, ErrOutputLimit)
	_, err = chatResultFrom(chatMessage{Content: "partial"}, "content_filter", chatUsage{})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrOutputLimit)
}

func TestResponsesOutputLimitAndOtherIncompleteReasons(t *testing.T) {
	opts := testOptions(models.ProviderOpenAI)
	opts.model.API = "responses"
	cases := []struct {
		name, output, reason string
		limit, partial       bool
	}{
		{"no-output", "[]", "max_output_tokens", true, false},
		{"reasoning-only", `[{"type":"reasoning","encrypted_content":"opaque"}]`, "max_output_tokens", true, false},
		{"function", `[{"type":"function_call","call_id":"call-a","name":"inspect","arguments":"{}"}]`, "max_output_tokens", true, false},
		{"partial-text", `[{"type":"message","content":[{"type":"output_text","text":"partial"}]}]`, "max_output_tokens", false, true},
		{"filtered", `[{"type":"message","content":[{"type":"output_text","text":"partial"}]}]`, "content_filter", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := fmt.Sprintf(`{"status":"incomplete","incomplete_details":{"reason":%q},"output":%s}`, tc.reason, tc.output)
			c := chatClient(t, opts, 200, "application/json", payload, nil)
			result, err := c.send(context.Background(), userMessages(), nil)
			if tc.partial {
				require.NoError(t, err)
				require.Equal(t, "partial", result.Content)
				require.Equal(t, message.FinishReasonMaxTokens, result.FinishReason)
				return
			}
			require.Error(t, err)
			require.Nil(t, result)
			if tc.limit {
				require.ErrorIs(t, err, ErrOutputLimit)
			} else {
				require.NotErrorIs(t, err, ErrOutputLimit)
			}
			c = chatClient(t, opts, 200, "text/event-stream", sseFrames(fmt.Sprintf(`{"type":"response.incomplete","response":%s}`, payload)), nil)
			expected := error(nil)
			if tc.limit {
				expected = ErrOutputLimit
			}
			assertOpenAIStreamRejected(t, c, expected)
		})
	}
}

func TestResponsesRejectNonObjectToolArguments(t *testing.T) {
	for _, args := range []string{`[]`, `null`, `42`, `true`, `"{}"`} {
		raw, _ := json.Marshal(map[string]any{"type": "function_call", "call_id": "call-a", "name": "inspect", "arguments": args})
		require.Error(t, appendResponsesOutput(&ProviderResponse{}, raw, "completed"), args)
	}
}

func assertOpenAIStreamRejected(t *testing.T, c *openaiClient, expected error) {
	t.Helper()
	events := collectEvents(t, c.stream(context.Background(), userMessages(), []tools.BaseTool{contractTool{}}))
	require.NotEmpty(t, events)
	require.Equal(t, EventError, events[len(events)-1].Type)
	for _, event := range events {
		require.NotEqual(t, EventComplete, event.Type)
		require.Nil(t, event.ToolCall)
	}
	if expected != nil {
		require.ErrorIs(t, events[len(events)-1].Error, expected)
	}
}

func TestChatRefusalRemainsVisibleWithoutContent(t *testing.T) {
	body := `{"choices":[{"index":0,"message":{"content":null,"refusal":"REQUEST_DECLINED"},"delta":{"refusal":"REQUEST_DECLINED"},"finish_reason":"stop"}]}`
	c := chatClient(t, testOptions(models.ProviderOpenAI), 200, "application/json", body, nil)
	reply, err := c.send(context.Background(), userMessages(), nil)
	require.NoError(t, err)
	require.Equal(t, "REQUEST_DECLINED", reply.Content)
	c = chatClient(t, testOptions(models.ProviderOpenAI), 200, "text/event-stream", sseFrames(body, `[DONE]`), nil)
	events := collectEvents(t, c.stream(context.Background(), userMessages(), nil))
	require.NotEmpty(t, events)
	var text string
	for _, e := range events {
		if e.Type == EventContentDelta {
			text += e.Content
		}
	}
	require.Equal(t, "REQUEST_DECLINED", text)
	last := events[len(events)-1]
	require.Equal(t, EventComplete, last.Type)
	require.Equal(t, text, last.Response.Content)
}
