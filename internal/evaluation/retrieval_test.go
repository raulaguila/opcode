package evaluation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/stretchr/testify/require"
)

func TestRankingFixturesAndEmbeddingConfigAreBoundedAndExplicit(t *testing.T) {
	suite, _, err := LoadRetrieval("")
	require.NoError(t, err)
	require.Len(t, suite.Cases, 3)
	suite.Cases[0].Expected = []string{"../outside"}
	require.Error(t, suite.Validate())
	path := filepath.Join(t.TempDir(), "embedding.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"endpoint":"http://127.0.0.1:11434/api/embed","model":"already-installed"}`), 0600))
	c, err := LoadEmbeddingConfig(path)
	require.NoError(t, err)
	require.Equal(t, "already-installed", c.Model)
	for _, bad := range []string{`{"endpoint":"https://remote.example/api/embed","model":"x"}`, `{"endpoint":"http://127.0.0.1:11434/api/embed","model":"x","apiKey":"private"}`, `{} {}`} {
		require.NoError(t, os.WriteFile(path, []byte(bad), 0600))
		_, err = LoadEmbeddingConfig(path)
		require.Error(t, err)
	}
}

func TestRankingWorkerHelper(t *testing.T) {
	if !strings.Contains(strings.Join(os.Args, " "), "evaluate-retrieval-worker") {
		return
	}
	for i, arg := range os.Args {
		if arg == "--job" && i+1 < len(os.Args) {
			if err := RetrievalWorker(context.Background(), os.Args[i+1], os.Stdout); err != nil {
				os.Exit(2)
			}
			os.Exit(0)
		}
	}
	os.Exit(3)
}

func TestDirectRankingUsesProductionSearchAndReportsHybridFallback(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("real production index requires rg")
	}
	if os.PathSeparator != '/' {
		t.Skip("test helper shell requires POSIX")
	}
	suite, raw, err := LoadRetrieval("")
	require.NoError(t, err)
	suite.Cases = suite.Cases[:1]
	binary, err := os.Executable()
	require.NoError(t, err)
	script := filepath.Join(t.TempDir(), "ranking-worker")
	quoted := "'" + strings.ReplaceAll(binary, "'", "'\\''") + "'"
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\nexec "+quoted+" -test.run=TestRankingWorkerHelper -- \"$@\"\n"), 0700))
	report, err := RunRetrieval(context.Background(), suite, raw, script, nil)
	require.NoError(t, err)
	require.Len(t, report.Results, 4)
	require.False(t, report.EmbeddingsConfigured)
	for _, result := range report.Results {
		require.Equal(t, "measured", result.Status, result)
		require.NotNil(t, result.Recall)
		require.Equal(t, 1., *result.Recall)
		require.Equal(t, 1., *result.MRR)
		require.Positive(t, result.ResponseBytes)
	}
	require.Equal(t, "cold", report.Results[0].Cache)
	require.Equal(t, "warm", report.Results[1].Cache)
	require.Zero(t, report.Results[1].ScanReads)
	require.Contains(t, report.Results[2].EffectiveStrategy, "embeddings not configured")
	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "return amount")
}

func TestDirectRankingMeasuresConfiguredHybridWithControlledVectors(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("production index requires rg")
	}
	if os.PathSeparator != '/' {
		t.Skip("POSIX test helper")
	}
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var request struct {
			Model    string
			Input    []string
			Truncate bool
		}
		if r.URL.Path != "/api/embed" || json.NewDecoder(r.Body).Decode(&request) != nil || request.Model != "controlled-vectors" || request.Truncate {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		vectors := [][]float64{}
		for _, text := range request.Input {
			if strings.Contains(text, "refund_purchase") || strings.Contains(text, "customer reimbursement") {
				vectors = append(vectors, []float64{1, 0})
			} else {
				vectors = append(vectors, []float64{0, 1})
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": vectors})
	}))
	defer server.Close()
	suite, raw, err := LoadRetrieval("")
	require.NoError(t, err)
	suite.Cases = suite.Cases[2:]
	binary, err := os.Executable()
	require.NoError(t, err)
	script := filepath.Join(t.TempDir(), "ranking-worker")
	quoted := "'" + strings.ReplaceAll(binary, "'", "'\\''") + "'"
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\nexec "+quoted+" -test.run=TestRankingWorkerHelper -- \"$@\"\n"), 0700))
	report, err := RunRetrieval(context.Background(), suite, raw, script, &config.EmbeddingConfig{Endpoint: server.URL + "/api/embed", Model: "controlled-vectors"})
	require.NoError(t, err)
	require.Len(t, report.Results, 4)
	require.Equal(t, 0., *report.Results[0].Recall)
	for _, result := range report.Results[2:] {
		require.Equal(t, "measured", result.Status)
		require.Equal(t, 1., *result.Recall)
		// RRF retains the exact "customer" lexical hit at rank one. The
		// relevant synonym is recovered at rank two, not magically first.
		require.Equal(t, .5, *result.MRR)
		require.Contains(t, result.EffectiveStrategy, "hybrid")
	}
	require.Positive(t, calls.Load())
}
