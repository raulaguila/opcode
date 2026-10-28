package diagnostic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/stretchr/testify/require"
)

type probeTransport func(*http.Request) (*http.Response, error)

func (f probeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func probeHTTP(t *testing.T, transport probeTransport) {
	t.Helper()
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
}
func reply(r *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}
}
func probeFixture() (models.Model, config.Provider) {
	return models.Model{ID: "unsaved-candidate", Provider: models.ProviderLocal, APIModel: "candidate", ContextWindow: 128000}, config.Provider{BaseURL: "http://snapshot.invalid/v1", APIKey: "private-fixture-credential"}
}

func TestProbeUnsavedCandidateUsesSnapshotAndNeverChangesSelection(t *testing.T) {
	model, connection := probeFixture()
	before := model
	configured := config.Get()
	registrySize := len(models.SupportedModels)
	t.Setenv("LOCAL_ENDPOINT", "http://wrong-legacy.invalid/v1")
	calls := 0
	probeHTTP(t, func(r *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "snapshot.invalid", r.URL.Host)
		require.Equal(t, "Bearer private-fixture-credential", r.Header.Get("Authorization"))
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		nonce := regexp.MustCompile(`[a-f0-9]{32}`).FindString(string(raw))
		require.NotEmpty(t, nonce)
		var payload map[string]any
		require.NoError(t, json.Unmarshal(raw, &payload))
		require.EqualValues(t, 256, payload["max_tokens"])
		body := `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"probe","type":"function","function":{"name":"opcode_health_check","arguments":"{\"challenge\":\"` + nonce + `\"}"}}]}}]}`
		return reply(r, 200, body), nil
	})
	require.NoError(t, ProbeModel(context.Background(), model, connection))
	require.Equal(t, 1, calls)
	require.Equal(t, before, model)
	require.Same(t, configured, config.Get())
	require.Len(t, models.SupportedModels, registrySize)
	_, exists := models.SupportedModels[model.ID]
	require.False(t, exists)
	_, err := (probeTool{}).Run(context.Background(), tools.ToolCall{})
	require.ErrorContains(t, err, "never execute")
}
func TestProbeRejectsUnverifiedCallsAndOmitsProviderBodies(t *testing.T) {
	model, connection := probeFixture()
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{"missing", 200, `{"choices":[{"message":{"content":"no tool needed"}}]}`},
		{"wrong challenge", 200, `{"choices":[{"message":{"tool_calls":[{"id":"p","type":"function","function":{"name":"opcode_health_check","arguments":"{\"challenge\":\"wrong\"}"}}]}}]}`},
		{"authentication", 401, `private-fixture-credential provider response must not escape`},
	} {
		t.Run(test.name, func(t *testing.T) {
			probeHTTP(t, func(r *http.Request) (*http.Response, error) { return reply(r, test.status, test.body), nil })
			err := ProbeModel(context.Background(), model, connection)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "private-fixture-credential")
			require.NotContains(t, err.Error(), "provider response must not escape")
			var safe *ProbeError
			require.ErrorAs(t, err, &safe)
			if test.status == 401 {
				require.Equal(t, "authentication", safe.Kind)
				require.Equal(t, 401, safe.StatusCode)
			}
		})
	}
}
func TestProbeCancellationAndDeadlineArePreserved(t *testing.T) {
	model, connection := probeFixture()
	calls := 0
	probeHTTP(t, func(r *http.Request) (*http.Response, error) {
		calls++
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, ProbeModel(ctx, model, connection), context.Canceled)
	require.Zero(t, calls)
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, ProbeModel(ctx, model, connection), context.DeadlineExceeded)
	require.Equal(t, 1, calls)
}

func TestNativeProbeBoundsContextAndGeneration(t *testing.T) {
	model, connection := probeFixture()
	connection.API = "ollama"
	probeHTTP(t, func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "/api/chat", r.URL.Path)
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		nonce := regexp.MustCompile(`[a-f0-9]{32}`).FindString(string(raw))
		require.NotEmpty(t, nonce)
		var payload struct{ Options map[string]any }
		require.NoError(t, json.Unmarshal(raw, &payload))
		require.EqualValues(t, 4096, payload.Options["num_ctx"])
		require.EqualValues(t, 256, payload.Options["num_predict"])
		return reply(r, 200, `{"done":true,"message":{"role":"assistant","tool_calls":[{"function":{"name":"opcode_health_check","arguments":{"challenge":"`+nonce+`"}}}]}}`), nil
	})
	require.NoError(t, ProbeModel(context.Background(), model, connection))
}
