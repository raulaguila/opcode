package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

func TestAzureClonesPreserveVersionedRoutesAndCredentials(t *testing.T) {
	for _, version := range []string{"2024-10-21", "v1"} {
		for _, api := range []string{"chat_completions", "responses"} {
			t.Run(version+"/"+api, func(t *testing.T) { testAzureCloneRequests(t, version, api) })
		}
	}
}

func testAzureCloneRequests(t *testing.T, version, api string) {
	t.Helper()
	endpoint := "/chat/completions"
	if api == "responses" {
		endpoint = "/responses"
	}
	want := "/openai/v1" + endpoint
	if version != "v1" {
		want = "/openai" + endpoint + "?api-version=" + version
		if api != "responses" {
			want = "/openai/deployments/my-deployment" + endpoint + "?api-version=" + version
		}
	}
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.Equal(t, want, r.URL.String())
		require.Equal(t, "fixture-api-key", r.Header.Get("api-key"))
		require.Empty(t, r.Header.Get("Authorization"))
		if strings.HasSuffix(r.URL.Path, "/responses") {
			fmt.Fprint(w, `{"id":"fixture","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`)
		} else {
			fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
		}
	}))
	defer server.Close()
	t.Setenv("AZURE_OPENAI_ENDPOINT", server.URL)
	t.Setenv("AZURE_OPENAI_API_VERSION", version)
	model := models.Model{Provider: models.ProviderAzure, APIModel: "my-deployment", API: api, ContextWindow: 32768}
	p, err := NewProvider(models.ProviderAzure, WithAPIKey("fixture-api-key"), WithModel(model), WithMaxTokens(2048))
	require.NoError(t, err)
	transport := p.(*baseProvider).client.(*openaiClient).transport
	transport.client = server.Client()
	for range 4 {
		assertAzurePreparedAndDirect(t, p)
		p, err = p.(SystemPromptController).WithSystemPrompt("scoped system prompt")
		require.NoError(t, err)
		p, err = p.(ContextWindowController).WithContextWindow(16384)
		require.NoError(t, err)
		p, err = p.(SourceCitationController).WithSourceCitations([]string{"main.go:1-2"})
		require.NoError(t, err)
		require.Same(t, transport, p.(*baseProvider).client.(*openaiClient).transport)
	}
	require.Equal(t, 8, requests)
}

func assertAzurePreparedAndDirect(t *testing.T, p Provider) {
	t.Helper()
	history := []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Hello"}}}}
	result, err := p.SendMessages(context.Background(), history, nil)
	require.NoError(t, err)
	require.Equal(t, "ok", result.Content)
	prepared, err := p.(RequestPreparer).PrepareRequest(history, nil, false)
	require.NoError(t, err)
	result, err = prepared.SendMessages(context.Background())
	require.NoError(t, err)
	require.Equal(t, "ok", result.Content)
}
