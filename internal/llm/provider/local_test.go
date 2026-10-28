package provider

import (
	"context"
	"net/http"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/stretchr/testify/require"
)

func TestLocalProviderUsesConfiguredEndpointWithoutAPIKey(t *testing.T) {
	t.Setenv("LOCAL_ENDPOINT", "")
	p, err := NewProvider(models.ProviderLocal,
		WithModel(models.Model{ID: "local.test", Provider: models.ProviderLocal, APIModel: "llama3.1:8b"}),
		WithMaxTokens(256),
		WithOpenAIOptions(WithOpenAIBaseURL("http://local.invalid/custom/v1")),
	)
	require.NoError(t, err)
	c := p.(*baseProvider).client.(*openaiClient)
	c.transport.client = wireHTTP(t, 200, "application/json", `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`, func(req *http.Request, body map[string]any) {
		require.Equal(t, "local.invalid", req.URL.Host)
		require.Equal(t, "/custom/v1/chat/completions", req.URL.Path)
		require.Empty(t, req.Header.Get("Authorization"))
		require.Equal(t, "llama3.1:8b", body["model"])
		require.EqualValues(t, 256, body["max_tokens"])
		require.NotContains(t, body, "reasoning_effort")
	})
	r, err := p.SendMessages(context.Background(), userMessages(), nil)
	require.NoError(t, err)
	require.Equal(t, "ok", r.Content)
}

func TestLocalProviderNeverDefaultsToRemoteEndpoint(t *testing.T) {
	t.Setenv("LOCAL_ENDPOINT", "")
	_, err := NewProvider(models.ProviderLocal)
	require.ErrorContains(t, err, "LOCAL_ENDPOINT")
}

func TestLocalProviderEndpointEnvironmentOverride(t *testing.T) {
	t.Setenv("LOCAL_ENDPOINT", "http://environment.invalid")
	p, err := NewProvider(models.ProviderLocal, WithOpenAIOptions(WithOpenAIBaseURL("http://file.invalid/v1")))
	require.NoError(t, err)
	c := p.(*baseProvider).client.(*openaiClient)
	require.Equal(t, "http://environment.invalid/v1", c.transport.baseURL)
}
