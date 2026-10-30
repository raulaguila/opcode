package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

func cloneFixture(t testing.TB, endpoint, key string) Provider {
	t.Helper()
	p, err := NewProvider(models.ProviderOpenAI, WithAPIKey(key), WithBaseURL(endpoint), WithSystemMessage("original"), WithMaxTokens(2048), WithModel(models.Model{Provider: models.ProviderOpenAI, APIModel: "fixture", ContextWindow: 32768}))
	require.NoError(t, err)
	return p
}

func TestRequestClonesReuseConnectionWithoutOptionChainGrowth(t *testing.T) {
	var connections atomic.Int64
	var requests atomic.Int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content any `json:"content"`
			} `json:"messages"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.NotEqual(t, "original", body.Messages[0].Content)
		require.Equal(t, "Bearer first-key", r.Header.Get("Authorization"))
		requests.Add(1)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.Start()
	defer server.Close()
	p := cloneFixture(t, server.URL, "first-key")
	initial := len(p.(*baseProvider).options.openaiOptions)
	originalTransport := p.(*baseProvider).client.(*openaiClient).transport
	for i := range 24 {
		clone, err := p.(SystemPromptController).WithSystemPrompt(fmt.Sprintf("prompt-%d", i))
		require.NoError(t, err)
		clone, err = clone.(ContextWindowController).WithContextWindow(8192)
		require.NoError(t, err)
		clone, err = clone.(SourceCitationController).WithSourceCitations([]string{"main.go:1"})
		require.NoError(t, err)
		require.Same(t, originalTransport, clone.(*baseProvider).client.(*openaiClient).transport)
		require.Len(t, clone.(*baseProvider).options.openaiOptions, initial)
		_, err = clone.SendMessages(context.Background(), []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "test"}}}}, nil)
		require.NoError(t, err)
		p = clone
	}
	require.EqualValues(t, 24, requests.Load())
	require.EqualValues(t, 1, connections.Load(), "cloning must retain the keep-alive pool")
}

func TestRequestCloneCredentialAndEndpointIsolation(t *testing.T) {
	for _, key := range []string{"first-key", "second-key"} {
		t.Run(key, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "Bearer "+key, r.Header.Get("Authorization"))
				fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
			}))
			defer server.Close()
			original := cloneFixture(t, server.URL, key)
			clone, err := original.(SystemPromptController).WithSystemPrompt("scoped")
			require.NoError(t, err)
			require.Equal(t, "original", original.(*baseProvider).options.systemMessage)
			_, err = clone.SendMessages(context.Background(), nil, nil)
			require.NoError(t, err)
		})
	}
}

func BenchmarkRequestScopedClone(b *testing.B) {
	p := cloneFixture(b, "http://127.0.0.1:11434/v1", "fixture")
	b.Run("rebuild_connection", func(b *testing.B) {
		options := p.(*baseProvider).options
		options.systemMessage = "request prompt"
		b.ReportAllocs()
		for b.Loop() {
			_, err := NewProvider(options.model.Provider, func(dst *providerClientOptions) { *dst = options })
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("clone_request", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, err := p.(SystemPromptController).WithSystemPrompt("request prompt")
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func TestRequestClonePreservesTLSIsolation(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	model := models.Model{Provider: models.ProviderOpenAI, APIModel: "fixture", ContextWindow: 32768}
	for _, insecure := range []bool{false, true, false} {
		p, err := NewProvider(models.ProviderOpenAI, WithModel(model), WithBaseURL(server.URL), WithAPIKey("fixture"), WithTLSInsecure(insecure))
		require.NoError(t, err)
		clone, err := p.(SystemPromptController).WithSystemPrompt("scoped")
		require.NoError(t, err)
		require.Same(t, p.(*baseProvider).client.(*openaiClient).transport, clone.(*baseProvider).client.(*openaiClient).transport)
		_, err = clone.SendMessages(context.Background(), nil, nil)
		if insecure {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
}

func TestRequestClonesRetainNormalizedProtocolAndAuthOwner(t *testing.T) {
	opts := providerClientOptions{model: models.Model{Provider: models.ProviderAnthropic, APIModel: "normalized-id", ContextWindow: 32768}, apiKey: "fixture", maxTokens: 2048}
	anthropic := newAnthropicClient(opts)
	anthropic.bedrock = &bedrockProtocol{model: "normalized-id"}
	gemini, err := newGeminiClient(opts)
	require.NoError(t, err)
	copilot, err := newCopilotClient(providerClientOptions{model: opts.model, copilotOptions: []CopilotOption{WithCopilotBearerToken("fixture")}})
	require.NoError(t, err)
	clients := []providerClient{anthropic, gemini, newOllamaClient(opts, "http://localhost/v1"), copilot}
	for _, client := range clients {
		p := &baseProvider{options: opts, client: client}
		clone, err := p.WithSystemPrompt("scoped")
		require.NoError(t, err)
		source := client.(interface{ requestHTTP() *httpProvider }).requestHTTP()
		target := clone.(*baseProvider).client.(interface{ requestHTTP() *httpProvider }).requestHTTP()
		require.Same(t, source.transport, target.transport)
		require.NotSame(t, source.protocol, target.protocol)
	}
	clone := anthropic.cloneRequest(providerClientOptions{model: models.Model{APIModel: "un-normalized-id"}, systemMessage: "scoped"}).(*anthropicClient)
	require.Equal(t, "normalized-id", clone.providerOptions.model.APIModel)
	require.Same(t, anthropic.bedrock, clone.bedrock)
	// Copilot clones use its OpenAI protocol and original locked auth owner;
	// copying copilotClient itself would copy a live mutex/token state.
	clonedCopilot := copilot.cloneRequest(opts).(*openaiClient)
	require.Same(t, copilot.transport, clonedCopilot.transport)
	require.NotNil(t, clonedCopilot.transport.authorize)
}
