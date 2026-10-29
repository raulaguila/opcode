//go:build integration

package evaluation

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	appconfig "github.com/raulaguila/opcode/internal/config"
	"github.com/stretchr/testify/require"
)

// This test uses a separately built production binary. No fake provider is
// present in the evaluator; it is deliberately opt-in and loopback-only.
func TestLiveOllamaOverviewEvaluation(t *testing.T) {
	liveOllamaEvaluation(t, []string{"overview"}, false)
}

func TestLiveOllamaWorkflowEvaluation(t *testing.T) {
	// Keep the bundled prompts, fixtures, time limits and criteria unchanged.
	// These are model-quality gates, separate from deterministic runtime tests.
	liveOllamaEvaluation(t, nil, true)
}

func liveOllamaEvaluation(t *testing.T, caseIDs []string, allowExecution bool) {
	t.Helper()
	if appconfig.Environment("OPCODE_LIVE_TESTS") != "1" || appconfig.Environment("OPCODE_LIVE_OLLAMA") != "1" {
		t.Skip("requires OPCODE_LIVE_TESTS=1 OPCODE_LIVE_OLLAMA=1 and a production OPCODE_EVAL_BINARY")
	}
	binary := appconfig.Environment("OPCODE_EVAL_BINARY")
	if binary == "" {
		t.Skip("set OPCODE_EVAL_BINARY to a freshly built Opcode executable")
	}
	require.True(t, filepath.IsAbs(binary))
	endpoint := os.Getenv("LOCAL_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://127.0.0.1:11434/v1"
	}
	parsed, err := url.Parse(endpoint)
	require.NoError(t, err)
	require.Contains(t, []string{"127.0.0.1", "localhost", "::1"}, parsed.Hostname())
	require.Contains(t, []string{"http", "https"}, parsed.Scheme)
	model := appconfig.Environment("OPCODE_LIVE_OLLAMA_MODEL")
	if model == "" {
		model = "llama3.1:8b"
	}
	id := "local." + model
	configuration := map[string]any{"providers": map[string]any{"local": map[string]any{"baseURL": endpoint}}, "models": map[string]any{id: map[string]any{"provider": "local", "api": "ollama", "api_model": model, "context_window": 16384, "default_max_tokens": 2048}}, "agents": map[string]any{"coder": map[string]any{"model": id}, "title": map[string]any{"model": id}, "summarizer": map[string]any{"model": id}, "task": map[string]any{"model": id}}}
	raw, err := json.Marshal(configuration)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "provider.json")
	require.NoError(t, os.WriteFile(path, raw, 0600))
	suite, raw, err := Load("")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 11*time.Minute)
	defer cancel()
	report, err := Run(ctx, suite, raw, Options{Live: true, AllowExecution: allowExecution, CaseIDs: caseIDs, ProviderConfig: path, Executable: binary})
	require.NoError(t, err)
	require.False(t, Failed(report), "declared fixture criteria failed: %+v", report)
	require.Equal(t, "live_provider", report.Mode)
}
