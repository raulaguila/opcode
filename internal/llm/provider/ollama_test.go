package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

func TestOllamaNativeContextToolsAndHistory(t *testing.T) {
	t.Setenv("LOCAL_ENDPOINT", "")
	p, err := NewProvider(models.ProviderLocal,
		WithModel(models.Model{ID: "local.llama3.1:8b", Provider: models.ProviderLocal, APIModel: "llama3.1:8b", API: "ollama", ContextWindow: 16384}),
		WithAPIKey("unit-test-key"), WithMaxTokens(2048), WithSystemMessage("inspect the project"),
		WithOpenAIOptions(WithOpenAIBaseURL("http://local.invalid/prefix/v1")))
	require.NoError(t, err)
	c := p.(*baseProvider).client.(*ollamaClient)
	c.transport.client = wireHTTP(t, 200, "application/json", `{"message":{"tool_calls":[{"function":{"name":"inspect","arguments":{"number":9007199254740993}}}]},"done":true,"done_reason":"stop","prompt_eval_count":42,"eval_count":7}`, func(req *http.Request, body map[string]any) {
		require.Equal(t, "/prefix/api/chat", req.URL.Path)
		require.Equal(t, "Bearer unit-test-key", req.Header.Get("Authorization"))
		require.Equal(t, "llama3.1:8b", body["model"])
		require.Equal(t, false, body["stream"])
		require.Equal(t, false, body["truncate"])
		require.EqualValues(t, 16384, body["options"].(map[string]any)["num_ctx"])
		require.EqualValues(t, 2048, body["options"].(map[string]any)["num_predict"])
		require.EqualValues(t, 0, body["options"].(map[string]any)["temperature"])
		require.Len(t, body["tools"], 2)
	})
	response, err := p.SendMessages(context.Background(), userMessages(), []tools.BaseTool{contractTool{}})
	require.NoError(t, err)
	require.Equal(t, message.FinishReasonToolUse, response.FinishReason)
	require.Equal(t, TokenUsage{InputTokens: 42, OutputTokens: 7}, response.Usage)
	require.Len(t, response.ToolCalls, 1)
	call := response.ToolCalls[0]
	require.NotEmpty(t, call.ID)
	require.True(t, call.Finished)
	require.Contains(t, call.Input, "9007199254740993")
	history := append(userMessages(), message.Message{Role: message.Assistant, Parts: []message.ContentPart{call, message.ReasoningContent{Thinking: "inspect first"}}},
		message.Message{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: call.ID, Content: "project data"}}})
	converted, err := c.convertMessages(history)
	require.NoError(t, err)
	require.Equal(t, "inspect", converted[3]["tool_name"], "agent tool results can omit Name; resolve it from the call ID")
	require.Equal(t, "inspect first", converted[2]["thinking"])
	raw, err := json.Marshal(converted)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"number":9007199254740993`)
}

func TestOllamaNativeStreamAndParallelCalls(t *testing.T) {
	opts := testOptions(models.ProviderLocal)
	c := newOllamaClient(opts, "http://local.invalid/v1")
	stream := strings.Join([]string{
		`{"message":{"thinking":"reason"},"done":false}`,
		`{"message":{"content":"Olá "},"done":false}`,
		`{"message":{"content":"mundo","tool_calls":[{"function":{"name":"inspect","arguments":{"path":"a"}}},{"function":{"name":"inspect","arguments":{"path":"b"}}}]},"done":false}`,
		`{"message":{},"done":true,"done_reason":"stop","prompt_eval_count":12,"eval_count":6}`,
	}, "\n")
	c.transport.client = wireHTTP(t, 200, "application/x-ndjson", stream, nil)
	events := collectEvents(t, c.stream(context.Background(), userMessages(), nil))
	require.Equal(t, EventThinkingDelta, events[0].Type)
	require.Equal(t, "reason", events[0].Thinking)
	last := events[len(events)-1]
	require.Equal(t, EventComplete, last.Type)
	require.Equal(t, "Olá mundo", last.Response.Content)
	require.Len(t, last.Response.ToolCalls, 2)
	require.NotEqual(t, last.Response.ToolCalls[0].ID, last.Response.ToolCalls[1].ID)
	require.JSONEq(t, `{"path":"a"}`, last.Response.ToolCalls[0].Input)
	require.JSONEq(t, `{"path":"b"}`, last.Response.ToolCalls[1].Input)
}

func TestOllamaRejectsIncompleteOrInvalidStreams(t *testing.T) {
	for _, body := range []string{
		`{"message":{"content":"partial"},"done":false}`,
		`{"message":{"tool_calls":[{"function":{"name":"inspect","arguments":{}}}]},"done":true,"done_reason":"length"}`,
		`{"message":{"tool_calls":[{"function":{"name":"inspect","arguments":"{}"}}]},"done":true,"done_reason":"stop"}`,
		`{"error":"private details"}`,
		`not json`,
		`{"message":{"tool_calls":[{"function":{"name":"final_answer","arguments":{"answer":"ok"}}},{"function":{"name":"inspect","arguments":"{}"}}]},"done":true,"done_reason":"stop"}`,
		`{"message":{"tool_calls":[{"function":{"name":"final_answer","arguments":{"answer":"one"}}},{"function":{"name":"final_answer","arguments":{"answer":"two"}}}]},"done":true,"done_reason":"stop"}`,
	} {
		c := newOllamaClient(testOptions(models.ProviderLocal), "http://local.invalid/v1")
		c.transport.client = wireHTTP(t, 200, "application/x-ndjson", body, nil)
		events := collectEvents(t, c.stream(context.Background(), userMessages(), nil))
		require.NotEmpty(t, events)
		require.Equal(t, EventError, events[len(events)-1].Type)
		require.NotContains(t, events[len(events)-1].Error.Error(), "private details")
		for _, event := range events {
			require.NotEqual(t, EventComplete, event.Type)
		}
	}
}

func TestOllamaMixedAnswerWaitsForRealToolResults(t *testing.T) {
	for _, answerFirst := range []bool{true, false} {
		answer := `{"function":{"name":"final_answer","arguments":{"answer":"PREMATURE_SUCCESS"}}}`
		operation := `{"id":"saved-call","function":{"name":"get_task","arguments":{}}}`
		calls := operation + "," + answer
		if answerFirst {
			calls = answer + "," + operation
		}
		body := `{"message":{"tool_calls":[` + calls + `]},"done":true,"done_reason":"stop"}`
		c := newOllamaClient(testOptions(models.ProviderLocal), "http://local.invalid/v1")
		decoded, err := c.decodeResponse([]byte(body))
		require.NoError(t, err)
		require.Empty(t, decoded.Content)
		require.Equal(t, message.FinishReasonToolUse, decoded.FinishReason)
		require.Len(t, decoded.ToolCalls, 1)
		require.Equal(t, "get_task", decoded.ToolCalls[0].Name)
		require.Equal(t, "saved-call", decoded.ToolCalls[0].ID)
		c.transport.client = wireHTTP(t, 200, "application/x-ndjson", body, nil)
		events := collectEvents(t, c.stream(context.Background(), userMessages(), nil))
		for _, event := range events {
			require.NotContains(t, event.Content, "PREMATURE_SUCCESS")
		}
		last := events[len(events)-1]
		require.Equal(t, EventComplete, last.Type)
		require.Equal(t, decoded.ToolCalls, last.Response.ToolCalls)
		require.Empty(t, last.Response.Content)
	}
}

func TestOllamaFinalAnswerPublishesTextWithoutAnExecutableToolCall(t *testing.T) {
	c := newOllamaClient(testOptions(models.ProviderLocal), "http://local.invalid/v1")
	stream := `{"message":{"content":"Resposta: "},"done":false}` + "\n" +
		`{"message":{"tool_calls":[{"function":{"name":"final_answer","arguments":{"answer":"O projeto usa Go e SQLite."}}}]},"done":true,"done_reason":"stop"}`
	c.transport.client = wireHTTP(t, 200, "application/x-ndjson", stream, nil)
	events := collectEvents(t, c.stream(context.Background(), userMessages(), nil))
	var text strings.Builder
	for _, event := range events {
		if event.Type == EventContentDelta {
			text.WriteString(event.Content)
		}
	}
	last := events[len(events)-1]
	require.Equal(t, EventComplete, last.Type)
	require.Equal(t, "Resposta: O projeto usa Go e SQLite.", text.String())
	require.Equal(t, text.String(), last.Response.Content)
	require.Equal(t, message.FinishReasonEndTurn, last.Response.FinishReason)
	require.Empty(t, last.Response.ToolCalls)
}

func TestOllamaDoesNotRepeatAnAnswerSentAsBothContentAndFinalTool(t *testing.T) {
	for _, tc := range []struct{ content, final, want string }{
		{"Resposta (source.go:1).", "Resposta (source.go:1).", "Resposta (source.go:1)."},
		{"Resposta (source.go:1).\n", "Resposta (source.go:1).", "Resposta (source.go:1).\n"},
		{"Resposta", "Resposta (source.go:1).", "Resposta (source.go:1)."},
		{"Introdução (unread.go:9). ", "Conclusão (source.go:1).", "Introdução (unread.go:9). Conclusão (source.go:1)."},
	} {
		first, err := json.Marshal(map[string]any{"message": map[string]any{"content": tc.content}, "done": false})
		require.NoError(t, err)
		last, err := json.Marshal(map[string]any{"message": map[string]any{"tool_calls": []any{map[string]any{"function": map[string]any{"name": "final_answer", "arguments": map[string]any{"answer": tc.final}}}}}, "done": true, "done_reason": "stop"})
		require.NoError(t, err)
		c := newOllamaClient(testOptions(models.ProviderLocal), "http://local.invalid/v1")
		c.transport.client = wireHTTP(t, 200, "application/x-ndjson", string(first)+"\n"+string(last), nil)
		events := collectEvents(t, c.stream(context.Background(), userMessages(), nil))
		var text strings.Builder
		for _, event := range events {
			if event.Type == EventContentDelta {
				text.WriteString(event.Content)
			}
		}
		require.Equal(t, tc.want, text.String())
		require.Equal(t, tc.want, events[len(events)-1].Response.Content)
		require.Empty(t, events[len(events)-1].Response.ToolCalls)
	}
}

func TestOllamaFinalAnswerCarriesModelCitationsForAgentValidation(t *testing.T) {
	c := newOllamaClient(testOptions(models.ProviderLocal), "http://local.invalid/v1")
	response, err := c.decodeResponse(json.RawMessage(`{"message":{"tool_calls":[{"function":{"name":"final_answer","arguments":{"answer":"A função subtrai os argumentos.","citations_text":"counter.py:2"}}}]},"done":true,"done_reason":"stop"}`))
	require.NoError(t, err)
	require.Equal(t, "A função subtrai os argumentos. (counter.py:2)", response.Content)
	require.Empty(t, response.ToolCalls, "citation rendering must not become an executable tool")
	// The adapter does not certify these model claims: the agent's strict guard
	// must reject this reference when it lacks an actual current read receipt.
	unread, err := c.decodeResponse(json.RawMessage(`{"message":{"tool_calls":[{"function":{"name":"final_answer","arguments":{"answer":"Unverified claim.","citations_text":"invented.go:999"}}}]},"done":true,"done_reason":"stop"}`))
	require.NoError(t, err)
	require.Contains(t, unread.Content, "invented.go:999")
	_, err = c.decodeResponse(json.RawMessage(`{"message":{"tool_calls":[{"function":{"name":"final_answer","arguments":{"answer":"Invalid shape.","citations_text":["counter.py:2"]}}}]},"done":true,"done_reason":"stop"}`))
	require.Error(t, err)
	_, err = c.decodeResponse(json.RawMessage(`{"message":{"tool_calls":[{"function":{"name":"final_answer","arguments":{"answer":"Too large.","citations_text":"` + strings.Repeat("a", 2049) + `"}}}]},"done":true,"done_reason":"stop"}`))
	require.Error(t, err)
}

func TestOllamaCitationPresentationRetainsUnverifiedSelectionsWithoutDuplicateFooters(t *testing.T) {
	for _, tc := range []struct{ answer, citations, want string }{
		{"Explicação (source.go:1-2).", "source.go:1-2", "Explicação (source.go:1-2)."},
		{"Explicação `source.go:1-2`.", "source.go:1-2", "Explicação `source.go:1-2`."},
		{"Explicação (source.go:10).", "source.go:1", "Explicação (source.go:10). (source.go:1)"},
		{"Explicação (source.go:1).", "source.go:1\ninvented.go:999", "Explicação (source.go:1). (invented.go:999)"},
		{"```go\nrun()\n```", "source.go:2", "```go\nrun()\n```\n\n(source.go:2)"},
	} {
		require.Equal(t, tc.want, appendOllamaCitations(tc.answer, tc.citations, nil))
	}
}

func TestOllamaCoveredCitationsRequireAnInspectedRequestScopedRange(t *testing.T) {
	inspected := []string{"source.go:1-44"}
	require.Equal(t, "Fato (source.go:3-5).", appendOllamaCitations("Fato (source.go:3-5).", "source.go:1-44", inspected))
	for _, tc := range []struct{ answer, source string }{
		{"Fato (source.go:3).", "source.go:1-999"},
		{"Fato (source.go:30-99).", "source.go:1-44"},
		{"Fato (other-source.go:3).", "source.go:1-44"},
		{"Fato (invented.go:3).", "invented.go:1-44"},
	} {
		require.Contains(t, appendOllamaCitations(tc.answer, tc.source, inspected), "("+tc.source+")", "unknown/outside selections must reach the ordinary citation guard")
	}
}

func TestOllamaRepairSchemaUsesOnlyRequestScopedCitationChoices(t *testing.T) {
	opts := testOptions(models.ProviderLocal)
	opts.sourceCitations = []string{"counter.py:1-2", "README.md:1-2"}
	c := newOllamaClient(opts, "http://local.invalid/v1")
	_, body, err := c.request(userMessages(), []tools.BaseTool{contractTool{}}, false)
	require.NoError(t, err)
	functions := body.(object)["tools"].([]object)
	final := functions[len(functions)-1]["function"].(object)
	properties := final["parameters"].(object)["properties"].(object)
	require.Equal(t, ollamaSourceAnswer, final["name"])
	require.Equal(t, opts.sourceCitations, properties["source"].(object)["enum"])
	require.Contains(t, properties["source"].(object)["description"], "not a restriction on the complete explanation")
	explanation := properties["explanation"].(object)["description"]
	for _, phrase := range []string{"ORIGINAL user request", "every requested input/output case", "exact returned values", "not replace concrete results with vague paraphrases", "executable call/return statement lines", "does not limit the answer to one source or one fact"} {
		require.Contains(t, explanation, phrase)
	}
	messages := body.(object)["messages"].([]object)
	require.Contains(t, messages[0]["content"], "including all requested cases and exact supported results")
	opts.sourceCitations = nil
	plain := newOllamaClient(opts, "http://local.invalid/v1")
	_, normal, err := plain.request(userMessages(), []tools.BaseTool{contractTool{}}, false)
	require.NoError(t, err)
	functions = normal.(object)["tools"].([]object)
	final = functions[len(functions)-1]["function"].(object)
	require.Equal(t, ollamaFinalAnswer, final["name"])
	require.NotContains(t, final["parameters"].(object)["properties"].(object), "source", "hints must not leak to another request")
}
