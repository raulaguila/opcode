package provider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

func sseFrames(values ...string) string {
	return "data: " + strings.Join(values, "\n\ndata: ") + "\n\n"
}
func TestParallelChatToolFragments(t *testing.T) {
	stream := sseFrames(
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"inspect","arguments":"{\"path\":"}},{"index":1,"id":"call_b","type":"function","function":{"name":"inspect","arguments":"{\"path\":"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"\"b\"}"}},{"index":0,"function":{"arguments":"\"a\"}"}}]},"finish_reason":"tool_calls"}]}`,
		`[DONE]`,
	)
	client := chatClient(t, testOptions(models.ProviderOpenAI), 200, "text/event-stream", stream, nil)
	events := collectEvents(t, client.stream(context.Background(), userMessages(), nil))
	last := events[len(events)-1]
	require.Equal(t, EventComplete, last.Type)
	require.Equal(t, message.FinishReasonToolUse, last.Response.FinishReason)
	calls := last.Response.ToolCalls
	require.Len(t, calls, 2)
	require.Equal(t, "call_a", calls[0].ID)
	require.JSONEq(t, `{"path":"a"}`, calls[0].Input)
	require.Equal(t, "call_b", calls[1].ID)
	require.JSONEq(t, `{"path":"b"}`, calls[1].Input)
}
func TestAnthropicStreamPreservesSignedThinkingAndExactArguments(t *testing.T) {
	stream := sseFrames(
		`{"type":"message_start","message":{"content":[],"usage":{"input_tokens":8,"cache_read_input_tokens":4}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"reason"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"signature-a"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"signature-b"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_1","name":"inspect","input":{},"future_field":{"integer":9007199254740993}}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"number\":9007199254740993}"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":3}}`,
		`{"type":"message_stop"}`,
	)
	client := anthropicTestClient(t, 200, "text/event-stream", stream, nil)
	events := collectEvents(t, client.stream(context.Background(), userMessages(), nil))
	last := events[len(events)-1]
	require.Equal(t, EventComplete, last.Type)
	require.Len(t, last.Response.ToolCalls, 1)
	require.Contains(t, last.Response.ToolCalls[0].Input, "9007199254740993")
	require.Contains(t, string(last.Response.ProviderState.Data), `"signature":"signature-asignature-b"`)
	require.Contains(t, string(last.Response.ProviderState.Data), `"integer":9007199254740993`)
	require.Equal(t, TokenUsage{InputTokens: 8, OutputTokens: 3, CacheReadTokens: 4}, last.Response.Usage)
	history := []message.Message{{Role: message.Assistant, Parts: []message.ContentPart{last.Response.ToolCalls[0], message.Finish{ProviderState: last.Response.ProviderState}}}}
	raw, err := json.Marshal(client.convertMessages(history))
	require.NoError(t, err)
	require.Contains(t, string(raw), "9007199254740993")
	require.Contains(t, string(raw), "signature-asignature-b")
}
func TestIncompleteToolCallsFail(t *testing.T) {
	clients := []providerClient{
		chatClient(t, testOptions(models.ProviderOpenAI), 200, "text/event-stream", sseFrames(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call","function":{"name":"inspect","arguments":"{"}}]},"finish_reason":"length"}]}`, `[DONE]`), nil),
		anthropicTestClient(t, 200, "text/event-stream", sseFrames(`{"type":"message_start","message":{"content":[]}}`, `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call","name":"inspect","input":{}}}`, `{"type":"message_delta","delta":{"stop_reason":"tool_use"}}`, `{"type":"message_stop"}`), nil),
		geminiTestClient(t, 200, "text/event-stream", sseFrames(`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"inspect","args":{}}}]},"finishReason":"MAX_TOKENS"}]}`), nil),
	}
	opts := testOptions(models.ProviderOpenAI)
	opts.model.API = "responses"
	clients = append(clients, chatClient(t, opts, 200, "text/event-stream", sseFrames(`{"type":"response.incomplete","response":{"status":"incomplete","output":[{"type":"function_call","call_id":"call","name":"inspect","arguments":"{}"}]}}`), nil))
	for _, client := range clients {
		events := collectEvents(t, client.stream(context.Background(), userMessages(), nil))
		require.NotEmpty(t, events)
		require.Equal(t, EventError, events[len(events)-1].Type)
		for _, e := range events {
			require.NotEqual(t, EventComplete, e.Type)
		}
	}
}
func TestGeminiOlderModelsDoNotReceiveInventedCallIDs(t *testing.T) {
	body := strings.ReplaceAll(geminiResult, `"id":"call_1",`, "")
	client := geminiTestClient(t, 200, "application/json", body, nil)
	result, err := client.send(context.Background(), userMessages(), nil)
	require.NoError(t, err)
	require.Len(t, result.ToolCalls, 1)
	call := result.ToolCalls[0]
	require.NotEmpty(t, call.ID)
	history := []message.Message{{Role: message.Assistant, Parts: []message.ContentPart{call, message.Finish{ProviderState: result.ProviderState}}}, {Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: call.ID, Content: `{"number":9007199254740993}`}}}}
	converted := client.convertMessages(history)
	require.Len(t, converted, 2)
	require.NotContains(t, string(converted[1].Parts[0]), `"id"`)
	require.Contains(t, string(converted[1].Parts[0]), `9007199254740993`)
	require.Contains(t, string(converted[1].Parts[0]), `"name":"inspect"`)
}
