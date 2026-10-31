package provider

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/stretchr/testify/require"
)

func TestCompatibleProviderTLSOptInCoversCompletionAndStreaming(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/chat/completions", r.URL.Path)
		var body struct {
			Stream bool `json:"stream"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		if body.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`)
	}))
	defer server.Close()
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	model := models.Model{ID: "tls-fixture", Provider: models.ProviderLocal, ProviderProfile: "fixture", API: "chat_completions", APIModel: "fixture"}
	for _, insecure := range []bool{false, true, false} {
		p, err := NewProvider(models.ProviderLocal, WithModel(model), WithBaseURL(server.URL+"/v1"), WithTLSInsecure(insecure))
		require.NoError(t, err)
		response, err := p.SendMessages(context.Background(), userMessages(), nil)
		if !insecure {
			require.Error(t, err)
			continue
		}
		require.NoError(t, err)
		require.Equal(t, "OK", response.Content)
		events := collectEvents(t, p.StreamResponse(context.Background(), userMessages(), nil))
		require.Equal(t, EventComplete, events[len(events)-1].Type)
	}
	_, err := NewProvider(models.ProviderLocal, WithModel(model), WithBaseURL("http://localhost/v1"), WithTLSInsecure(true))
	require.Error(t, err)
}

func TestOllamaCarriesReportedGenerationDuration(t *testing.T) {
	response, err := ollamaResult(ollamaResponse{Done: true, DoneReason: "stop", EvalCount: 50, EvalDuration: 2_000_000_000}, nil)
	require.NoError(t, err)
	require.EqualValues(t, 2_000_000_000, response.Usage.OutputDurationNS)
	require.Equal(t, "provider", response.Usage.DurationSource)
}
