package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryIgnoreSnapshotAndSharedMap(t *testing.T) {
	root := productivityV2Workspace(t)
	for name, text := range map[string]string{".opcodeignore": "*.tmp\n!keep.tmp\nignored/\n!.env\n", "keep.tmp": "ok", "drop.tmp": "no", "ignored/a.py": "no", "src/a.py": "def run(): return 37\n", ".env": "private"} {
		retrievalFixture(t, root, name, text)
	}
	tool := SecureTool(NewLsTool(), nil)
	r, err := tool.Run(toolContext(), toolInput(map[string]any{"path": ".", "limit": 1}))
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	var page struct{ Snapshot string }
	require.NoError(t, json.Unmarshal([]byte(r.Content), &page))
	require.Len(t, page.Snapshot, 64)
	require.Equal(t, ".opcodeignore", ExclusionReason(root, filepath.Join(root, "drop.tmp")))
	require.NotEmpty(t, ExclusionReason(root, filepath.Join(root, ".env")))
	all := listPage(t, tool, map[string]any{"path": "."})
	require.Equal(t, []string{"keep.tmp", "src/a.py"}, all.Files)
	r, err = projectMap(toolContext(), ".", 0, 30)
	require.NoError(t, err)
	require.Contains(t, r.Content, "src/a.py")
	require.NotContains(t, r.Content, "drop.tmp")
	retrievalFixture(t, root, "new.py", "def new(): pass\n")
	r, err = tool.Run(toolContext(), toolInput(map[string]any{"path": ".", "offset": 1, "snapshot": page.Snapshot}))
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.Contains(t, r.Content, "listing changed")
}
func TestPagedReadLongUnicodeLinesAndExternalEdit(t *testing.T) {
	root := productivityV2Workspace(t)
	text := strings.Repeat("á", 150) + "\nsecond\n"
	retrievalFixture(t, root, "source.py", text)
	tool := SecureTool(NewReadPageTool(), nil)
	offset, column := 0, 0
	hash := ""
	var result strings.Builder
	for i := 0; i < 20; i++ {
		r, err := tool.Run(toolContext(), toolInput(map[string]any{"file_path": "source.py", "offset": offset, "column": column, "max_bytes": 64, "expected_hash": hash}))
		require.NoError(t, err)
		require.False(t, r.IsError, r.Content)
		var p struct {
			Content string
			Hash    string `json:"file_hash"`
			Next    int    `json:"next_offset"`
			Column  int    `json:"next_column"`
			More    bool
		}
		require.NoError(t, json.Unmarshal([]byte(r.Content), &p))
		result.WriteString(p.Content)
		require.LessOrEqual(t, len(p.Content), 64)
		hash = p.Hash
		if !p.More {
			break
		}
		require.True(t, p.Next > offset || p.Next == offset && p.Column > column)
		offset, column = p.Next, p.Column
	}
	require.Equal(t, text, result.String())
	retrievalFixture(t, root, "source.py", "external edit\n")
	r, err := tool.Run(toolContext(), toolInput(map[string]any{"file_path": "source.py", "expected_hash": hash}))
	require.NoError(t, err)
	require.True(t, r.IsError)
}
func TestIndexControlPauseRebuildAndBounds(t *testing.T) {
	root := productivityV2Workspace(t)
	retrievalFixture(t, root, "a.py", "def total(): return 37\n")
	ctx := toolContext()
	s, err := ControlCodeIndex(ctx, "update", "")
	require.NoError(t, err)
	require.Equal(t, 1, s.Files)
	require.Greater(t, s.Chunks, 0)
	before := s.Snapshot
	_, err = ControlCodeIndex(ctx, "pause", "")
	require.NoError(t, err)
	_, err = ControlCodeIndex(ctx, "update", "")
	require.ErrorContains(t, err, "paused")
	r, err := SecureTool(NewCodeIndexTool(), nil).Run(ctx, toolInput(map[string]any{"query": "total"}))
	require.NoError(t, err)
	require.Contains(t, r.Content, `"index_paused":true`)
	_, err = ControlCodeIndex(ctx, "resume", "")
	require.NoError(t, err)
	retrievalFixture(t, root, "a.py", "def total(): return 38\n")
	s, err = ControlCodeIndex(ctx, "rebuild", "")
	require.NoError(t, err)
	require.NotEqual(t, before, s.Snapshot)
	cfg := config.Get()
	cfg.Index.MaxTextBytes = 1024
	t.Cleanup(func() { cfg.Index.MaxTextBytes = 0 })
	retrievalFixture(t, root, "big.py", strings.Repeat("x", 1100))
	_, err = ControlCodeIndex(ctx, "rebuild", "")
	require.Error(t, err)
	s, err = ControlCodeIndex(ctx, "status", "")
	require.NoError(t, err)
	require.Equal(t, 1, s.Files, "failed rebuild must preserve previous snapshot")
	_, err = ControlCodeIndex(ctx, "clear", "")
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(root, "a.py"))
}
func TestIndexWatcherStopsAndReconciles(t *testing.T) {
	root := productivityV2Workspace(t)
	retrievalFixture(t, root, "a.go", "package a\nfunc Total() int{return 37}\n")
	ctx, cancel := context.WithCancel(context.Background())
	w := StartIndexWatcher(ctx)
	require.Eventually(t, func() bool { s, e := ControlCodeIndex(ctx, "status", ""); return e == nil && s.Files == 1 }, 5*time.Second, 50*time.Millisecond)
	retrievalFixture(t, root, "b.py", "def other(): return 42\n")
	require.Eventually(t, func() bool { s, e := ControlCodeIndex(ctx, "status", ""); return e == nil && s.Files == 2 }, 5*time.Second, 50*time.Millisecond)
	cancel()
	w.Close()
}
func TestEmbeddingBoundsRedactionAndFallback(t *testing.T) {
	root := productivityV2Workspace(t)
	_ = root
	t.Setenv("TEST_EMBED_API_KEY", "embedding-secret-fixture")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/embed", r.URL.Path)
		var p struct {
			Input    []string
			Truncate bool
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&p))
		require.False(t, p.Truncate)
		require.NotContains(t, strings.Join(p.Input, " "), "embedding-secret-fixture")
		require.Empty(t, r.Header.Get("Authorization"))
		calls++
		fmt.Fprint(w, `{"embeddings":[[1,0]]}`)
	}))
	defer server.Close()
	c := config.EmbeddingConfig{Endpoint: server.URL + "/api/embed", Model: "fixture"}
	values, err := localEmbeddings(context.Background(), c, []string{"embedding-secret-fixture"})
	require.NoError(t, err)
	require.Equal(t, [][]float64{{1, 0}}, values)
	require.Equal(t, 1, calls)
	_, err = localEmbeddings(context.Background(), config.EmbeddingConfig{Endpoint: "https://example.com/api/embed", Model: "private"}, []string{"source"})
	require.Error(t, err)
	require.Equal(t, 1., cosine([]float64{1, 0}, []float64{1, 0}))
	require.Equal(t, -1., cosine([]float64{0}, []float64{1}))
}
func TestIgnoreSymlinkCannotExposeFiles(t *testing.T) {
	root := productivityV2Workspace(t)
	external := filepath.Join(t.TempDir(), "policy")
	require.NoError(t, os.WriteFile(external, []byte("!.env\n"), 0600))
	require.NoError(t, os.Symlink(external, filepath.Join(root, ".opcodeignore")))
	require.Contains(t, ExclusionReason(root, filepath.Join(root, "a.py")), "unsafe ignore policy")
}

func TestPatchPreviewReceiptRejectsSourceChanges(t *testing.T) {
	root := productivityV2Workspace(t)
	path := filepath.Join(root, "a.txt")
	retrievalFixture(t, root, "a.txt", "before\n")
	recordFileRead(path)
	perms := &denyingPermissions{}
	tool := SecureTool(NewPatchTool(nil, perms, nil), perms)
	patch := "*** Begin Patch\n*** Update File: " + path + "\n@@\n-before\n+after\n*** End Patch"
	r, err := tool.Run(toolContext(), toolInput(map[string]any{"patch_text": patch, "preview": true}))
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Empty(t, perms.requests)
	var preview struct {
		ID string `json:"preview_id"`
	}
	require.NoError(t, json.Unmarshal([]byte(r.Content), &preview))
	require.Len(t, preview.ID, 64)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "before\n", string(data))
	retrievalFixture(t, root, "a.txt", "prefix\nbefore\n")
	recordFileRead(path)
	r, err = tool.Run(toolContext(), toolInput(map[string]any{"patch_text": patch, "preview_id": preview.ID}))
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.Contains(t, r.Content, "changed since preview")
	require.Empty(t, perms.requests)
}

func TestHybridRetrievalRecoversAControlledSemanticOnlyMatch(t *testing.T) {
	root := productivityV2Workspace(t)
	retrievalFixture(t, root, "a.py", "def archive_invoice(): return 0\n")
	retrievalFixture(t, root, "b.py", "def calculate_total(): return 37\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Input []string }
		require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
		vectors := [][]float64{}
		for _, text := range in.Input {
			v := []float64{0, 1}
			if text == "somar faturamento" || strings.Contains(text, "calculate_total") {
				v = []float64{1, 0}
			}
			vectors = append(vectors, v)
		}
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"embeddings": vectors}))
	}))
	defer server.Close()
	cfg := config.Get()
	previous := cfg.Index.Embeddings
	cfg.Index.Embeddings = &config.EmbeddingConfig{Endpoint: server.URL + "/api/embed", Model: "controlled-fixture"}
	t.Cleanup(func() { cfg.Index.Embeddings = previous })
	tool := SecureTool(NewCodeIndexTool(), nil)
	lexical := searchPage(t, tool, map[string]any{"query": "somar faturamento", "strategy": "lexical"})
	require.Empty(t, lexical.Results)
	hybrid := searchPage(t, tool, map[string]any{"query": "somar faturamento", "strategy": "hybrid"})
	require.NotEmpty(t, hybrid.Results)
	require.Equal(t, "b.py", hybrid.Results[0].Path)
	t.Log("Controlled vectors: lexical target recall@1=0; hybrid target recall@1=1. This tests ranking/fallback, not real embedding-model quality.")
}

func TestPausedHybridDoesNotSendNewlyIgnoredSourceToEmbeddings(t *testing.T) {
	root := productivityV2Workspace(t)
	retrievalFixture(t, root, "private.py", "def confidential_total(): return 37 # PRIVATE_FIXTURE_MARKER\n")
	tool := SecureTool(NewCodeIndexTool(), nil)
	searchPage(t, tool, map[string]any{"query": "confidential_total"})
	_, err := ControlCodeIndex(toolContext(), "pause", "")
	require.NoError(t, err)
	retrievalFixture(t, root, ".opcodeignore", "private.py\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Input []string }
		require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
		for _, text := range in.Input {
			require.NotContains(t, text, "PRIVATE_FIXTURE_MARKER")
			require.NotContains(t, text, "confidential_total")
		}
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float64{{1, 0}}}))
	}))
	defer server.Close()
	cfg := config.Get()
	previous := cfg.Index.Embeddings
	cfg.Index.Embeddings = &config.EmbeddingConfig{Endpoint: server.URL + "/api/embed", Model: "fixture"}
	t.Cleanup(func() { cfg.Index.Embeddings = previous })
	page := searchPage(t, tool, map[string]any{"query": "sum amounts", "strategy": "hybrid"})
	require.Empty(t, page.Results)
}
