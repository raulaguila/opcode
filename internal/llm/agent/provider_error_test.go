package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/stretchr/testify/require"
)

func TestProviderErrorDoesNotPublishCredentialEchoOrRequestID(t *testing.T) {
	a, _, _ := steeringAgent(t)
	const key = "fake-provider-key-123456"
	const echoedID = "fake-private-request-id-987654"
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer "+key, r.Header.Get("Authorization"))
		w.Header().Set("x-request-id", echoedID)
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, "invalid API key: "+key)
	}))
	defer server.Close()
	model := models.Model{ID: "fixture", Provider: models.ProviderOpenAI, APIModel: "fixture", ContextWindow: 32768}
	p, err := provider.NewProvider(model.Provider, provider.WithModel(model), provider.WithAPIKey(key), provider.WithBaseURL(server.URL), provider.WithMaxTokens(2048))
	require.NoError(t, err)
	a.provider = p
	done, err := a.Run(context.Background(), "session", "Hello")
	require.NoError(t, err)
	result := finishGeneration(t, done)
	require.Error(t, result.Error)
	var failure *provider.HTTPError
	require.ErrorAs(t, result.Error, &failure)
	require.Contains(t, failure.Body, key, "internal classification retains the original response")
	require.Equal(t, echoedID, failure.RequestID)
	for _, secret := range []string{key, echoedID} {
		require.NotContains(t, result.Error.Error(), secret)
		require.NotContains(t, logs.String(), secret)
	}
	require.Contains(t, logs.String(), "provider HTTP 400")
}

func TestPrivateHTTPBodiesStillClassifyContextAndLocalOOM(t *testing.T) {
	model := models.Model{Provider: models.ProviderLocal}
	for _, body := range []string{"maximum context length exceeded", "out of memory"} {
		failure := fmt.Errorf("request failed: %w", &provider.HTTPError{StatusCode: 500, Body: body})
		require.True(t, contextFailure(failure, model))
	}
	require.False(t, contextFailure(&provider.HTTPError{StatusCode: 401, Body: "context window"}, model))
}

func TestSSEProviderErrorsDoNotExposeEchoedSecrets(t *testing.T) {
	for _, backend := range []models.ModelProvider{models.ProviderOpenAI, models.ProviderAnthropic, models.ProviderGemini} {
		t.Run(string(backend), func(t *testing.T) { testSSEPrivateError(t, backend) })
	}
}

func testSSEPrivateError(t *testing.T, backend models.ModelProvider) {
	t.Helper()
	a, _, _ := steeringAgent(t)
	const secret = "fake-stream-echoed-key-123456"
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: error\ndata: {\"type\":\"error\",\"error\":{\"message\":%q}}\n\n", secret)
	}))
	defer server.Close()
	model := models.Model{ID: "fixture", Provider: backend, APIModel: "fixture", ContextWindow: 32768}
	p, err := provider.NewProvider(backend, provider.WithModel(model), provider.WithAPIKey(secret), provider.WithBaseURL(server.URL), provider.WithMaxTokens(2048))
	require.NoError(t, err)
	a.provider = p
	done, err := a.Run(context.Background(), "session", "Hello")
	require.NoError(t, err)
	result := finishGeneration(t, done)
	require.Error(t, result.Error)
	require.NotContains(t, result.Error.Error(), secret)
	require.NotContains(t, logs.String(), secret)
	var published *providerEventError
	require.ErrorAs(t, result.Error, &published)
	require.Contains(t, published.cause.Error(), secret, "the original cause stays private, never lost")
}

func TestProviderPublicFailureRetainsErrorIdentityAndContextClassification(t *testing.T) {
	model := models.Model{Provider: models.ProviderLocal}
	private := errors.New("maximum context length exceeded; fake-private-payload")
	public := publicProviderFailure(private, model)
	require.ErrorIs(t, public, private)
	require.True(t, contextFailure(public, model))
	require.NotContains(t, public.Error(), "fake-private-payload")
	require.ErrorIs(t, publicProviderFailure(provider.ErrInvalidFinalAnswer, model), provider.ErrInvalidFinalAnswer)
}

func TestSSEContextAndOOMCausesStillClassifyPrivately(t *testing.T) {
	for _, body := range []string{"maximum context length exceeded; fake-private-payload", "out of memory; fake-private-payload"} {
		t.Run(body[:8], func(t *testing.T) {
			a, _, _ := steeringAgent(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: {\"error\":{\"message\":%q}}\n\n", body)
			}))
			defer server.Close()
			model := models.Model{Provider: models.ProviderLocal, ProviderProfile: "fixture", API: "chat_completions", APIModel: "fixture", ContextWindow: 32768}
			p, err := provider.NewProvider(model.Provider, provider.WithModel(model), provider.WithBaseURL(server.URL))
			require.NoError(t, err)
			a.provider = p
			event := <-p.StreamResponse(context.Background(), nil, nil)
			require.Equal(t, provider.EventError, event.Type)
			failure := a.processEvent(context.Background(), "s", nil, event)
			require.ErrorIs(t, failure, event.Error)
			require.True(t, contextFailure(failure, model))
			require.NotContains(t, failure.Error(), "fake-private-payload")
			cancelled := fmt.Errorf("fake-private-payload: %w", context.Canceled)
			failure = a.processEvent(context.Background(), "s", nil, provider.ProviderEvent{Type: provider.EventError, Error: cancelled})
			require.ErrorIs(t, failure, context.Canceled)
			require.False(t, contextFailure(failure, model))
		})
	}
}

func TestAuxiliaryHTTP200ErrorsKeepPrivateBodiesOutOfPublicResultsAndTitleLogs(t *testing.T) {
	for _, backend := range []models.ModelProvider{models.ProviderOpenAI, models.ProviderAnthropic, models.ProviderGemini} {
		t.Run(string(backend), func(t *testing.T) { testAuxiliaryPrivateError(t, backend) })
	}
}

func testAuxiliaryPrivateError(t *testing.T, backend models.ModelProvider) {
	t.Helper()
	a, _, _ := steeringAgent(t)
	const secret = "fake-auxiliary-private-key-123456"
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"type":"error","error":{"message":%q}}`, secret)
	}))
	defer server.Close()
	model := models.Model{ID: "auxiliary", Provider: backend, APIModel: "fixture", ContextWindow: 32768, DefaultMaxTokens: 2048}
	p, err := provider.NewProvider(backend, provider.WithModel(model), provider.WithAPIKey(secret), provider.WithBaseURL(server.URL), provider.WithMaxTokens(2048))
	require.NoError(t, err)
	for _, role := range []config.AgentName{config.AgentTitle, config.AgentSummarizer, config.AgentTask} {
		_, err := sendAuxiliaryRequest(context.Background(), role, p, auxiliaryHistory("Hello"), nil)
		require.Error(t, err)
		require.NotContains(t, err.Error(), secret)
		var private *providerEventError
		require.ErrorAs(t, err, &private)
		require.Contains(t, private.cause.Error(), secret)
		require.ErrorIs(t, err, private.cause)
	}
	a.titleProvider = p
	a.generateInitialTitle(context.Background(), "session", "Hello")
	require.Contains(t, logs.String(), "failed to generate title")
	require.NotContains(t, logs.String(), secret)
}
