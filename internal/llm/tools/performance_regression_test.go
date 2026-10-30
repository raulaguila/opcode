package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryPolicyInvalidatesChangedAndCreatedIgnore(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			root := t.TempDir()
			if existing {
				require.NoError(t, os.WriteFile(filepath.Join(root, ".opcodeignore"), []byte("old.py\n"), 0600))
			}
			policy := newDiscoveryPolicy()
			require.Empty(t, policy.exclusionReason(root, filepath.Join(root, "new.py")))
			require.NoError(t, policy.Validate())
			require.NoError(t, os.WriteFile(filepath.Join(root, ".opcodeignore"), []byte("new.py\n"), 0600))
			require.ErrorContains(t, policy.Validate(), "policy changed")
			require.NotEmpty(t, newDiscoveryPolicy().exclusionReason(root, filepath.Join(root, "new.py")))
		})
	}
}

func TestDiscoveryPolicyCapacityFailsClosed(t *testing.T) {
	policy := newDiscoveryPolicy()
	for i := 0; i < 65536; i++ {
		policy.files[fmt.Sprint(i)] = policyFile{}
	}
	_, err := policy.matcher(t.TempDir(), ".opcodeignore")
	require.ErrorContains(t, err, "snapshot exceeds")
	require.ErrorContains(t, policy.Validate(), "snapshot exceeds")
}

func TestOperationRedactorRefreshesPublicBoundaryAfterCredentialRotation(t *testing.T) {
	first := "first-fixture-credential"
	second := "rotated-fixture-credential"
	redactor := newCredentialRedactor([]string{first, "prefix", `quoted"fixture`})
	require.Equal(t, "[REDACTED] [REDACTED]", redactor.Redact(first+` quoted\"fixture`))
	config.RegisterRuntimeCredentials("audit-redaction", second)
	t.Cleanup(func() { config.RegisterRuntimeCredentials("audit-redaction") })
	require.NotContains(t, redact(redactor.Redact(first+" "+second)), second)
}

func TestHybridSearchBatchesVectorsAndReleasesWorkspaceLock(t *testing.T) {
	root := productivityV2Workspace(t)
	// Initialize the WASM SQLite engine before timing HTTP/lock coordination.
	db, err := openCodeIndex(toolContext(), root)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	for i := 0; i < 20; i++ {
		retrievalFixture(t, root, fmt.Sprintf("file%d.txt", i), fmt.Sprintf("search_fixture_%d returns %d\n", i, i))
	}
	started, release := make(chan struct{}, 1), make(chan struct{})
	var requests, coldSize atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct{ Input []string }
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			w.WriteHeader(400)
			return
		}
		if requests.Add(1) == 1 {
			coldSize.Store(int32(len(input.Input)))
			started <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		vectors := make([][]float64, len(input.Input))
		for i := range vectors {
			vectors[i] = []float64{1, 0}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": vectors})
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(toolContext())
	defer cancel()
	previous := config.Get().Index.Embeddings
	config.Get().Index.Embeddings = &config.EmbeddingConfig{Endpoint: server.URL + "/api/embed", Model: "batch-fixture"}
	t.Cleanup(func() { config.Get().Index.Embeddings = previous })
	done := make(chan error, 1)
	go func() {
		response, err := SecureTool(NewCodeIndexTool(), nil).Run(ctx, toolInput(map[string]any{"query": "search_fixture", "strategy": "hybrid"}))
		if response.IsError && err == nil {
			err = fmt.Errorf("search fixture failed: %s", response.Content)
		}
		done <- err
	}()
	select {
	case <-started:
	case err := <-done:
		t.Fatalf("search ended before embeddings: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("embedding request did not start")
	}
	statusCtx, stop := context.WithTimeout(ctx, time.Second)
	_, err = ControlCodeIndex(statusCtx, "status", "")
	stop()
	close(release)
	runErr := <-done
	require.NoError(t, err, "status must not wait for embedding HTTP")
	require.NoError(t, runErr)
	require.Equal(t, int32(1), requests.Load(), "cold vectors use one HTTP batch")
	require.Greater(t, coldSize.Load(), int32(1))
	require.LessOrEqual(t, coldSize.Load(), int32(16))
}

func TestEmbeddingCacheWarmBudgetAndEvictionIndex(t *testing.T) {
	root := productivityV2Workspace(t)
	db, err := openCodeIndex(context.Background(), root)
	require.NoError(t, err)
	defer db.Close()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var input struct{ Input []string }
		_ = json.NewDecoder(r.Body).Decode(&input)
		vectors := make([][]float64, len(input.Input))
		for i := range vectors {
			vectors[i] = []float64{1, 0}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": vectors})
	}))
	defer server.Close()
	c := config.EmbeddingConfig{Endpoint: server.URL + "/api/embed", Model: "warm-fixture"}
	texts := make([]string, 20)
	for i := range texts {
		texts[i] = fmt.Sprint(i)
	}
	vectors, err := embeddingVectors(context.Background(), db, c, texts)
	require.NoError(t, err)
	require.Len(t, vectors, 16)
	_, err = embeddingVectors(context.Background(), db, c, texts[:16])
	require.NoError(t, err)
	require.Equal(t, int32(1), calls.Load())
	rows, err := db.Query(`EXPLAIN QUERY PLAN SELECT key FROM embeddings ORDER BY at DESC,key LIMIT 10000`)
	require.NoError(t, err)
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var id, parent, aux int
		var detail string
		require.NoError(t, rows.Scan(&id, &parent, &aux, &detail))
		plan.WriteString(detail)
	}
	require.NoError(t, rows.Err())
	require.Contains(t, plan.String(), "embeddings_recent")
	require.NotContains(t, plan.String(), "TEMP B-TREE")
}

func BenchmarkDiscoveryPolicy(b *testing.B) {
	root := b.TempDir()
	directory := filepath.Join(root, "src")
	if err := os.Mkdir(directory, 0700); err != nil {
		b.Fatal(err)
	}
	paths := make([]string, 100)
	for i := range paths {
		paths[i] = filepath.Join(directory, fmt.Sprintf("file%d.py", i))
	}
	for _, snapshot := range []bool{false, true} {
		b.Run(fmt.Sprint(snapshot), func(b *testing.B) {
			benchmarkDiscoveryOperation(b, root, paths, snapshot)
		})
	}
}

func benchmarkDiscoveryOperation(b *testing.B, root string, paths []string, snapshot bool) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		policy := newDiscoveryPolicy()
		for _, path := range paths {
			if snapshot {
				_ = policy.exclusionReason(root, path)
			} else {
				_ = ExclusionReason(root, path)
			}
		}
		if snapshot {
			if err := policy.Validate(); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkCredentialRedactionBatch(b *testing.B) {
	secrets := []string{"fixture-secret-token-12345", `fixture-secret-with"quotes`}
	for _, prepared := range []bool{false, true} {
		b.Run(fmt.Sprint(prepared), func(b *testing.B) {
			benchmarkRedactionOperation(b, secrets, prepared)
		})
	}
}

func benchmarkRedactionOperation(b *testing.B, secrets []string, prepared bool) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var redactor *credentialRedactor
		if prepared {
			redactor = newCredentialRedactor(secrets)
		}
		for range 100 {
			text := "a source preview without secrets"
			if prepared {
				_ = redactor.Redact(text)
			} else {
				for _, secret := range secrets {
					text = redactValue(text, secret)
				}
			}
		}
	}
}

func BenchmarkIndexPreparedStatements(b *testing.B) {
	for _, prepared := range []bool{false, true} {
		b.Run(fmt.Sprint(prepared), func(b *testing.B) {
			benchmarkIndexTransaction(b, prepared)
		})
	}
}

func benchmarkIndexTransaction(b *testing.B, prepared bool) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TABLE seen(path TEXT PRIMARY KEY)`); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx, err := db.Begin()
		if err != nil {
			b.Fatal(err)
		}
		if err := benchmarkIndexInserts(tx, prepared); err != nil {
			_ = tx.Rollback()
			b.Fatal(err)
		}
		if err := tx.Rollback(); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkIndexInserts(tx *sql.Tx, prepared bool) error {
	const query = `INSERT INTO seen(path) VALUES(?)`
	var statement *sql.Stmt
	if prepared {
		var err error
		statement, err = tx.Prepare(query)
		if err != nil {
			return err
		}
		defer statement.Close()
	}
	for i := 0; i < 100; i++ {
		var err error
		if prepared {
			_, err = statement.Exec(fmt.Sprint(i))
		} else {
			_, err = tx.Exec(query, fmt.Sprint(i))
		}
		if err != nil {
			return err
		}
	}
	return nil
}
