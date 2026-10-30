package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/stretchr/testify/require"
)

func TestIndexBinaryAssetsDoNotConsumeTextBudget(t *testing.T) {
	root := productivityV2Workspace(t)
	previous := config.Get().Index
	t.Cleanup(func() { config.Get().Index = previous })
	config.Get().Index.MaxTextBytes = 1024
	for i := range 3 {
		require.NoError(t, os.WriteFile(filepath.Join(root, fmt.Sprintf("asset%d.png", i)), bytes.Repeat([]byte{0, 1}, 400), 0600))
	}
	retrievalFixture(t, root, "source.txt", "search_fixture under the text limit\n")
	for range 2 {
		result, err := SecureTool(NewCodeIndexTool(), nil).Run(toolContext(), toolInput(map[string]any{"query": "search_fixture"}))
		require.NoError(t, err)
		require.False(t, result.IsError, result.Content)
		require.Contains(t, result.Content, "source.txt")
		require.NotContains(t, result.Content, "asset0.png")
	}
	// Neither file exceeds the limit alone; reused text must still contribute.
	retrievalFixture(t, root, "source.txt", string(bytes.Repeat([]byte("text"), 150)))
	_, err := NewCodeIndexTool().Run(toolContext(), toolInput(map[string]any{"query": "text"}))
	require.NoError(t, err)
	retrievalFixture(t, root, "too-large.txt", string(bytes.Repeat([]byte("text"), 150)))
	_, err = NewCodeIndexTool().Run(toolContext(), toolInput(map[string]any{"query": "search_fixture"}))
	require.ErrorContains(t, err, "text exceeds index limit")
}

func TestIndexBinaryAssetsDoNotStarveBackgroundBatch(t *testing.T) {
	root := productivityV2Workspace(t)
	path := filepath.Join(root, "source.txt")
	require.NoError(t, os.WriteFile(path, []byte("search_fixture\n"), 0600))
	old := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(path, old, old))
	for i := range 3 {
		require.NoError(t, os.WriteFile(filepath.Join(root, fmt.Sprintf("asset%d.png", i)), []byte{0, 1, 2}, 0600))
	}
	ctx := context.WithValue(toolContext(), indexBatchLimitKey{}, 1)
	status, err := ControlCodeIndex(ctx, "update", "")
	require.NoError(t, err)
	require.Equal(t, 1, status.Files)
	require.Zero(t, status.Pending)
}

func TestIndexScanHasIndependentByteLimit(t *testing.T) {
	root := productivityV2Workspace(t)
	path := filepath.Join(root, "source.txt")
	require.NoError(t, os.WriteFile(path, []byte("fixture"), 0600))
	refresh := indexRefresher{ctx: toolContext(), scanned: maxIndexScanBytes - 2}
	err := refresh.refreshSource(&indexedSource{path: path, bytes: 7})
	require.ErrorContains(t, err, "128 MiB index scan limit")
	require.Zero(t, refresh.stats.Read)
}
