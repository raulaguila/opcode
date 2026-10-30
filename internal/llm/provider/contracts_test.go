package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func wireHTTP(t *testing.T, status int, contentType, body string, check func(*http.Request, map[string]any)) *http.Client {
	t.Helper()
	return &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
		var payload map[string]any
		if req.Body != nil {
			raw, err := io.ReadAll(req.Body)
			if !assert.NoError(t, err) {
				return nil, err
			}
			err = json.Unmarshal(raw, &payload)
			if !assert.NoError(t, err) {
				return nil, err
			}
		}
		if check != nil {
			check(req, payload)
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
}

func testOptions(provider models.ModelProvider) providerClientOptions {
	return providerClientOptions{apiKey: "unit-test-key", maxTokens: 4096, systemMessage: "test instructions", model: models.Model{ID: "test", Provider: provider, APIModel: "test-model", CanReason: true}}
}

func userMessages() []message.Message {
	return []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "inspect"}}}}
}

type contractTool struct{}

func (contractTool) Info() tools.ToolInfo {
	return tools.ToolInfo{Name: "inspect", Description: "Inspect a path", Parameters: map[string]any{"path": map[string]any{"type": "string"}}, Required: []string{"path"}}
}

func (contractTool) Run(context.Context, tools.ToolCall) (tools.ToolResponse, error) {
	panic("contract test must not execute tools")
}

func chatClient(t *testing.T, opts providerClientOptions, status int, contentType, body string, check func(*http.Request, map[string]any)) *openaiClient {
	t.Helper()
	client := newOpenAIClient(opts)
	client.transport.client = wireHTTP(t, status, contentType, body, check)
	return client
}

func anthropicTestClient(t *testing.T, status int, contentType, body string, check func(*http.Request, map[string]any)) *anthropicClient {
	t.Helper()
	client := newAnthropicClient(testOptions(models.ProviderAnthropic))
	client.transport.client = wireHTTP(t, status, contentType, body, check)
	return client
}

func geminiTestClient(t *testing.T, status int, contentType, body string, check func(*http.Request, map[string]any)) *geminiClient {
	t.Helper()
	client, err := newGeminiClient(testOptions(models.ProviderGemini))
	require.NoError(t, err)
	client.transport.client = wireHTTP(t, status, contentType, body, check)
	return client
}

const chatResult = `{"id":"chat-test","object":"chat.completion","created":1,"model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok","tool_calls":[{"id":"call_1","type":"function","function":{"name":"inspect","arguments":"{\"path\":\".\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":12,"completion_tokens":3,"prompt_tokens_details":{"cached_tokens":4}}}`
const responsesResult = `{"id":"resp-test","object":"response","status":"completed","output":[{"type":"reasoning","id":"rs-test","summary":[],"encrypted_content":"opaque-reasoning"},{"type":"message","id":"msg-test","role":"assistant","status":"completed","phase":"commentary","content":[{"type":"output_text","text":"ok","annotations":[]}]},{"type":"function_call","id":"fc-test","call_id":"call_1","name":"inspect","arguments":"{\"path\":\".\"}","status":"completed"}],"usage":{"input_tokens":12,"output_tokens":3,"input_tokens_details":{"cached_tokens":4}}}`
const anthropicResult = `{"id":"msg-test","type":"message","role":"assistant","model":"test-model","content":[{"type":"thinking","thinking":"private reasoning","signature":"opaque-signature"},{"type":"text","text":"ok"},{"type":"tool_use","id":"call_1","name":"inspect","input":{"path":"."}}],"stop_reason":"tool_use","usage":{"input_tokens":8,"output_tokens":3,"cache_read_input_tokens":4,"cache_creation_input_tokens":0}}`
const geminiResult = `{"candidates":[{"content":{"role":"model","parts":[{"thought":true,"text":"private reasoning"},{"text":"o"},{"text":"k"},{"functionCall":{"id":"call_1","name":"inspect","args":{"path":"."}},"thoughtSignature":"c2lnbmF0dXJl"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":3,"cachedContentTokenCount":4,"thoughtsTokenCount":2}}`

func TestChatCompletionContract(t *testing.T) {
	opts := testOptions(models.ProviderGROQ)
	client := chatClient(t, opts, 200, "application/json", chatResult, func(req *http.Request, body map[string]any) {
		assert.Equal(t, "/v1/chat/completions", req.URL.Path)
		assert.Equal(t, "test-model", body["model"])
		assert.EqualValues(t, 4096, body["max_completion_tokens"])
		assert.NotContains(t, body, "reasoning_effort", "compatible providers must not receive an invented reasoning setting")
		function := body["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)
		assert.Equal(t, []any{"path"}, function["parameters"].(map[string]any)["required"])
	})
	result, err := client.send(context.Background(), userMessages(), []tools.BaseTool{contractTool{}})
	require.NoError(t, err)
	require.Equal(t, "ok", result.Content)
	require.Equal(t, "call_1", result.ToolCalls[0].ID)
	require.Equal(t, TokenUsage{InputTokens: 8, OutputTokens: 3, CacheReadTokens: 4}, result.Usage)

	// Historical tool calls and results retain their wire representation.
	history := append(userMessages(), message.Message{Role: message.Assistant, Parts: []message.ContentPart{result.ToolCalls[0]}}, message.Message{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "call_1", Content: "found"}}})
	raw, err := json.Marshal(client.convertMessages(history))
	require.NoError(t, err)
	require.Contains(t, string(raw), `"tool_call_id":"call_1"`)
	require.Contains(t, string(raw), `"arguments":"{\"path\":\".\"}"`)
}

func TestResponsesContractAndNativeReplay(t *testing.T) {
	opts := testOptions(models.ProviderOpenAI)
	opts.model.API = "responses"
	client := chatClient(t, opts, 200, "application/json", responsesResult, func(req *http.Request, body map[string]any) {
		assert.Equal(t, "/v1/responses", req.URL.Path)
		assert.Equal(t, false, body["store"])
		assert.Equal(t, []any{"reasoning.encrypted_content"}, body["include"])
		assert.Equal(t, "test instructions", body["instructions"])
		tool := body["tools"].([]any)[0].(map[string]any)
		assert.Equal(t, false, tool["strict"])
		assert.Equal(t, []any{"path"}, tool["parameters"].(map[string]any)["required"])
	})
	result, err := client.send(context.Background(), userMessages(), []tools.BaseTool{contractTool{}})
	require.NoError(t, err)
	require.Equal(t, "ok", result.Content)
	require.Equal(t, "call_1", result.ToolCalls[0].ID)
	require.Equal(t, message.FinishReasonToolUse, result.FinishReason)
	require.Equal(t, TokenUsage{InputTokens: 8, OutputTokens: 3, CacheReadTokens: 4}, result.Usage)
	history := append(userMessages(), message.Message{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: result.Content}, result.ToolCalls[0], message.Finish{ProviderState: result.ProviderState}}}, message.Message{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "call_1", Content: "found"}}})
	raw, err := json.Marshal(client.responseParams(history, nil))
	require.NoError(t, err)
	require.Contains(t, string(raw), `"encrypted_content":"opaque-reasoning"`)
	require.Contains(t, string(raw), `"phase":"commentary"`)
	require.Contains(t, string(raw), `"type":"function_call_output"`)
	require.Contains(t, string(raw), `"call_id":"call_1"`)
	client.providerOptions.model.APIModel = "different-model"
	raw, err = json.Marshal(client.responseParams(history, nil))
	require.NoError(t, err)
	require.NotContains(t, string(raw), "opaque-reasoning", "provider state must not cross model boundaries")
}

func TestAnthropicContractAndSignedReplay(t *testing.T) {
	client := anthropicTestClient(t, 200, "application/json", anthropicResult, func(req *http.Request, body map[string]any) {
		assert.Equal(t, "/v1/messages", req.URL.Path)
		assert.NotContains(t, body, "temperature")
		assert.Equal(t, []any{"path"}, body["tools"].([]any)[0].(map[string]any)["input_schema"].(map[string]any)["required"])
	})
	result, err := client.send(context.Background(), userMessages(), []tools.BaseTool{contractTool{}})
	require.NoError(t, err)
	require.Equal(t, message.FinishReasonToolUse, result.FinishReason)
	history := []message.Message{{Role: message.Assistant, Parts: []message.ContentPart{result.ToolCalls[0], message.Finish{ProviderState: result.ProviderState}}}}
	params := client.preparedMessages(client.convertMessages(history), nil)
	raw, err := json.Marshal(params)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"signature":"opaque-signature"`)
	require.Contains(t, string(raw), `"type":"enabled"`, "signed tool history must keep thinking enabled")
	client.providerOptions.model.ThinkingMode = "adaptive"
	raw, err = json.Marshal(client.preparedMessages(client.convertMessages(userMessages()), nil))
	require.NoError(t, err)
	require.Contains(t, string(raw), `"type":"adaptive"`)
	client.providerOptions.model.APIModel = "different-model"
	raw, err = json.Marshal(client.convertMessages(history))
	require.NoError(t, err)
	require.NotContains(t, string(raw), "opaque-signature")
}

func TestGeminiContractAndThoughtSignatures(t *testing.T) {
	messages := userMessages()
	messages[0].Parts = append(messages[0].Parts, message.BinaryContent{MIMEType: "image/png", Data: []byte("image")})
	client := geminiTestClient(t, 200, "application/json", geminiResult, func(req *http.Request, body map[string]any) {
		assert.Contains(t, req.URL.Path, "test-model:generateContent")
		parts := body["contents"].([]any)[0].(map[string]any)["parts"].([]any)
		assert.Equal(t, "image/png", parts[1].(map[string]any)["inlineData"].(map[string]any)["mimeType"])
	})
	result, err := client.send(context.Background(), messages, []tools.BaseTool{contractTool{}})
	require.NoError(t, err)
	require.Equal(t, "ok", result.Content)
	require.Equal(t, "call_1", result.ToolCalls[0].ID)
	require.Equal(t, TokenUsage{InputTokens: 8, OutputTokens: 5, CacheReadTokens: 4}, result.Usage)
	history := []message.Message{{Role: message.Assistant, Parts: []message.ContentPart{result.ToolCalls[0], message.Finish{ProviderState: result.ProviderState}}}, {Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "call_1", Content: "found"}}}}
	converted := client.convertMessages(history)
	require.Contains(t, string(rawJSON(converted[0].Parts[3])), `"thoughtSignature":"c2lnbmF0dXJl"`)
	require.Equal(t, "user", converted[1].Role)
	require.Contains(t, string(rawJSON(converted[1].Parts[0])), `"id":"call_1"`)
	client.providerOptions.model.APIModel = "different-model"
	raw, err := json.Marshal(client.convertMessages(history))
	require.NoError(t, err)
	require.NotContains(t, string(raw), "thoughtSignature")
}

func collectEvents(t *testing.T, events <-chan ProviderEvent) []ProviderEvent {
	t.Helper()
	var result []ProviderEvent
	deadline := time.After(3 * time.Second)
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return result
			}
			result = append(result, event)
		case <-deadline:
			t.Fatal("provider stream failed to close")
			return nil
		}
	}
}

func TestStreamingContracts(t *testing.T) {
	chatSSE := "data: {\"id\":\"chat\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"chat\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: {\"id\":\"chat\",\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":3,\"prompt_tokens_details\":{\"cached_tokens\":4}}}\n\ndata: [DONE]\n\n"
	responsesSSE := "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\",\"sequence_number\":1}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"sequence_number\":2,\"response\":" + responsesResult + "}\n\n"
	anthropicSSE := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"test-model\",\"usage\":{\"input_tokens\":8,\"output_tokens\":0}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	for _, name := range []string{"chat", "responses", "anthropic", "gemini"} {
		t.Run(name, func(t *testing.T) {
			var client providerClient
			switch name {
			case "chat":
				client = chatClient(t, testOptions(models.ProviderOpenAI), 200, "text/event-stream", chatSSE, nil)
			case "responses":
				opts := testOptions(models.ProviderOpenAI)
				opts.model.API = "responses"
				client = chatClient(t, opts, 200, "text/event-stream", responsesSSE, nil)
			case "anthropic":
				client = anthropicTestClient(t, 200, "text/event-stream", anthropicSSE, nil)
			case "gemini":
				client = geminiTestClient(t, 200, "text/event-stream", "data: "+geminiResult+"\n\n", nil)
			}
			events := collectEvents(t, client.stream(context.Background(), userMessages(), nil))
			require.NotEmpty(t, events)
			last := events[len(events)-1]
			require.Equal(t, EventComplete, last.Type, "%+v", last)
			require.Equal(t, "ok", last.Response.Content)
			if name != "chat" {
				require.NotNil(t, last.Response.ProviderState)
			}
		})
	}
}

func TestStreamsRejectMissingTerminalEvents(t *testing.T) {
	clients := []providerClient{
		chatClient(t, testOptions(models.ProviderOpenAI), 200, "text/event-stream", "data: [DONE]\n\n", nil),
		anthropicTestClient(t, 200, "text/event-stream", "", nil),
		geminiTestClient(t, 200, "text/event-stream", "", nil),
	}
	opts := testOptions(models.ProviderOpenAI)
	opts.model.API = "responses"
	clients = append(clients, chatClient(t, opts, 200, "text/event-stream", "", nil))
	for _, client := range clients {
		events := collectEvents(t, client.stream(context.Background(), userMessages(), nil))
		require.NotEmpty(t, events)
		require.Equal(t, EventError, events[len(events)-1].Type)
		require.Error(t, events[len(events)-1].Error)
	}
}

func TestProviderHTTPErrorsRemainErrors(t *testing.T) {
	clients := []providerClient{
		chatClient(t, testOptions(models.ProviderOpenAI), 400, "application/json", `{"error":{"message":"bad input","type":"invalid_request_error"}}`, nil),
		anthropicTestClient(t, 400, "application/json", `{"type":"error","error":{"type":"invalid_request_error","message":"bad input"}}`, nil),
		geminiTestClient(t, 400, "application/json", `{"error":{"code":400,"message":"bad input","status":"INVALID_ARGUMENT"}}`, nil),
	}
	for _, client := range clients {
		result, err := client.send(context.Background(), userMessages(), nil)
		require.Error(t, err)
		require.Nil(t, result)
	}
}

func TestUnreadStreamCanBeCancelled(t *testing.T) {
	opts := testOptions(models.ProviderOpenAI)
	client := chatClient(t, opts, 200, "text/event-stream", "data: {\"id\":\"chat\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\n", nil)
	ctx, cancel := context.WithCancel(context.Background())
	events := client.stream(ctx, userMessages(), nil)
	cancel()
	collectEvents(t, events)
}

func TestAzureDoesNotFallBackToPublicOpenAI(t *testing.T) {
	t.Setenv("AZURE_OPENAI_ENDPOINT", "")
	t.Setenv("AZURE_OPENAI_API_VERSION", "")
	client, err := NewProvider(models.ProviderAzure, WithModel(testOptions(models.ProviderAzure).model), WithAPIKey("azure-test-key"))
	require.ErrorContains(t, err, "AZURE_OPENAI_ENDPOINT")
	require.Nil(t, client)
}
