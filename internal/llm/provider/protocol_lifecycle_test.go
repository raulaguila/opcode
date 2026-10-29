package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type protocolFixture struct{ name, contentType, partial, toolResponse, finalResponse string }

func protocolFixtures() []protocolFixture {
	return []protocolFixture{
		{"chat", "text/event-stream", sseFrames(`{"choices":[{"index":0,"delta":{"content":"ação"}}]}`), chatResult, `{"choices":[{"message":{"role":"assistant","content":"ROUNDTRIP_OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":14,"completion_tokens":2}}`},
		{"responses", "text/event-stream", sseFrames(`{"type":"response.output_text.delta","delta":"ação"}`), responsesResult, `{"id":"final","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ROUNDTRIP_OK"}]}],"usage":{"input_tokens":14,"output_tokens":2}}`},
		{"anthropic", "text/event-stream", sseFrames(`{"type":"message_start","message":{"content":[],"usage":{"input_tokens":8}}}`, `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ação"}}`), anthropicResult, `{"id":"final","content":[{"type":"text","text":"ROUNDTRIP_OK"}],"stop_reason":"end_turn","usage":{"input_tokens":14,"output_tokens":2}}`},
		{"gemini", "text/event-stream", sseFrames(`{"candidates":[{"content":{"role":"model","parts":[{"text":"ação"}]}}]}`), geminiResult, `{"candidates":[{"content":{"role":"model","parts":[{"text":"ROUNDTRIP_OK"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":14,"candidatesTokenCount":2}}`},
		{"ollama", "application/x-ndjson", `{"message":{"content":"ação"},"done":false}` + "\n", `{"message":{"tool_calls":[{"function":{"name":"inspect","arguments":{"path":"."}}}]},"done":true,"done_reason":"stop","prompt_eval_count":12,"eval_count":3}`, `{"message":{"content":"ROUNDTRIP_OK"},"done":true,"done_reason":"stop","prompt_eval_count":14,"eval_count":2}`},
	}
}
func lifecycleClient(t *testing.T, name, endpoint string) providerClient {
	t.Helper()
	opts := testOptions(models.ProviderOpenAI)
	var client providerClient
	var transport *apiTransport
	switch name {
	case "chat", "responses":
		if name == "responses" {
			opts.model.API = "responses"
		}
		c := newOpenAIClient(opts)
		client, transport = c, c.transport
	case "anthropic":
		c := newAnthropicClient(testOptions(models.ProviderAnthropic))
		client, transport = c, c.transport
	case "gemini":
		c, err := newGeminiClient(testOptions(models.ProviderGemini))
		require.NoError(t, err)
		client, transport = c, c.transport
	case "ollama":
		c := newOllamaClient(testOptions(models.ProviderLocal), endpoint)
		client, transport = c, c.transport
	default:
		t.Fatal("unknown fixture")
	}
	transport.baseURL = endpoint
	transport.client = &http.Client{Timeout: 5 * time.Second}
	return client
}

func TestProtocolStreamsCancelActiveHTTPBody(t *testing.T) {
	for _, fixture := range protocolFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			cancelled := make(chan struct{})
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				_ = r.Body.Close()
				w.Header().Set("Content-Type", fixture.contentType)
				_, _ = io.WriteString(w, fixture.partial)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				close(cancelled)
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			events := lifecycleClient(t, fixture.name, server.URL).stream(ctx, userMessages(), nil)
			select {
			case event := <-events:
				require.Equal(t, EventContentDelta, event.Type)
				require.Equal(t, "ação", event.Content)
			case <-time.After(3 * time.Second):
				t.Fatal("first delta did not arrive while HTTP body remained open")
			}
			cancel()
			remaining := collectEvents(t, events)
			for _, event := range remaining {
				require.NotEqual(t, EventComplete, event.Type, "a cancelled partial stream must not claim completion")
			}
			select {
			case <-cancelled:
			case <-time.After(3 * time.Second):
				t.Fatal("cancellation did not close upstream request")
			}
			require.EqualValues(t, 1, requests.Load(), "cancellation must not replay a generation")
		})
	}
}
func TestProtocolToolRoundTripsOverHTTP(t *testing.T) {
	for _, fixture := range protocolFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, err := io.ReadAll(r.Body)
				if !assert.NoError(t, err) {
					return
				}
				var payload map[string]any
				if !assert.NoError(t, json.Unmarshal(raw, &payload)) {
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if requests.Add(1) == 1 {
					assert.Contains(t, string(raw), "inspect")
					_, _ = io.WriteString(w, fixture.toolResponse)
					return
				}
				assert.Contains(t, string(raw), "SOURCE_RESULT_42", "a second request must carry the actual result")
				assert.Contains(t, string(raw), "inspect")
				_, _ = io.WriteString(w, fixture.finalResponse)
			}))
			defer server.Close()
			client := lifecycleClient(t, fixture.name, server.URL)
			response, err := client.send(context.Background(), userMessages(), []tools.BaseTool{contractTool{}})
			require.NoError(t, err)
			require.Equal(t, message.FinishReasonToolUse, response.FinishReason)
			require.Len(t, response.ToolCalls, 1)
			call := response.ToolCalls[0]
			assistant := message.Message{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: response.Content}, call}}
			assistant.AddFinish(response.FinishReason)
			assistant.SetProviderState(response.ProviderState)
			history := append(userMessages(), assistant, message.Message{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: call.ID, Name: call.Name, Content: "SOURCE_RESULT_42"}}})
			final, err := client.send(context.Background(), history, []tools.BaseTool{contractTool{}})
			require.NoError(t, err)
			require.Equal(t, "ROUNDTRIP_OK", final.Content)
			require.Empty(t, final.ToolCalls)
			require.Equal(t, message.FinishReasonEndTurn, final.FinishReason)
			require.EqualValues(t, 14, final.Usage.InputTokens)
			require.EqualValues(t, 2, final.Usage.OutputTokens)
			require.EqualValues(t, 2, requests.Load())
		})
	}
}

// Keep fixture content static and assert the matrix never silently drops an adapter.
func TestProtocolLifecycleMatrixIsExplicit(t *testing.T) {
	names := []string{}
	for _, f := range protocolFixtures() {
		names = append(names, f.name)
		require.True(t, json.Valid([]byte(f.toolResponse)))
		require.True(t, json.Valid([]byte(f.finalResponse)))
		require.NotEmpty(t, strings.TrimSpace(f.partial))
	}
	require.Equal(t, []string{"chat", "responses", "anthropic", "gemini", "ollama"}, names)
}
