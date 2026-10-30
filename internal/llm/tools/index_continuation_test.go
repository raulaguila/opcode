package tools

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/stretchr/testify/require"
)

func continuationIndexFixture(t *testing.T, count int) (string, *sql.DB, *IndexWatcher) {
	t.Helper()
	root := productivityV2Workspace(t)
	for i := range count {
		retrievalFixture(t, root, fmt.Sprintf("source%04d.txt", i), "steady background fixture\n")
	}
	db, err := openCodeIndex(toolContext(), root)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	watcher := &IndexWatcher{root: root, state: indexWatchState{covered: true, dirty: map[string]uint64{}}}
	indexWatchers.Store(root, watcher)
	t.Cleanup(func() { indexWatchers.CompareAndDelete(root, watcher) })
	return root, db, watcher
}

func TestBackgroundContinuationSettlesDespiteExpiredEarlierBatches(t *testing.T) {
	root, db, watcher := continuationIndexFixture(t, 512)
	ctx := context.WithValue(toolContext(), indexBatchLimitKey{}, 8)
	for round := range 64 {
		stats, err := refreshSearchCodeIndex(ctx, db, root)
		require.NoError(t, err)
		require.Equal(t, 8, stats.Read)
		require.Equal(t, 512-(round+1)*8, stats.Pending)
		require.Equal(t, round == 63, watcher.state.known)
		var count int
		require.NoError(t, db.QueryRow("SELECT count(*) FROM files").Scan(&count))
		require.Equal(t, (round+1)*8, count)
		// Even a whole TTL passing between batches must not restart the captured pass.
		_, err = db.Exec("UPDATE files SET verified=verified-61")
		require.NoError(t, err)
		watcher.state.reconciled = time.Now().Add(-indexReconcileInterval)
		if round == 60 {
			watcher.reconcileWhenIdle()
			require.True(t, watcher.state.covered)
		}
	}
	require.Empty(t, watcher.state.pending)
	require.True(t, watcher.state.known)
}

func TestBackgroundExpiryPassContinuesWithoutDiscovery(t *testing.T) {
	root, db, watcher := continuationIndexFixture(t, 512)
	_, err := refreshCodeIndex(toolContext(), db, root)
	require.NoError(t, err)
	_, err = db.Exec("UPDATE files SET verified=verified-61")
	require.NoError(t, err)
	watcher.state.known = false
	ctx := context.WithValue(toolContext(), indexBatchLimitKey{}, 8)
	stats, err := refreshSearchCodeIndex(ctx, db, root)
	require.NoError(t, err)
	require.Equal(t, 504, stats.Pending)
	require.Zero(t, stats.structuralPending, "expiry must not mark the captured snapshot structurally incomplete")
	require.True(t, watcher.state.known)
	// Removing rg after the first pass proves every continuation avoids discovery.
	previousPath := os.Getenv("PATH")
	t.Setenv("PATH", t.TempDir())
	for round := range 63 {
		_, err = db.Exec("UPDATE files SET verified=verified-1")
		require.NoError(t, err)
		stats, err = refreshSearchCodeIndex(ctx, db, root)
		require.NoError(t, err)
		require.Equal(t, 8, stats.Read)
		require.Equal(t, 504-(round+1)*8, stats.Pending)
		require.True(t, watcher.state.known)
	}
	require.Empty(t, watcher.state.pending)
	stats, err = refreshSearchCodeIndex(ctx, db, root)
	require.NoError(t, err)
	require.Equal(t, indexRefresh{}, stats)
	t.Setenv("PATH", previousPath)
	watcher.reconcileWhenIdle()
	require.False(t, watcher.state.covered)
	_, err = refreshSearchCodeIndex(ctx, db, root)
	require.NoError(t, err, "an idle periodic reconciliation still performs bounded full discovery")
}

func TestBackgroundContinuationInvalidatesForPolicyChanges(t *testing.T) {
	root, db, watcher := continuationIndexFixture(t, 24)
	ctx := context.WithValue(toolContext(), indexBatchLimitKey{}, 8)
	stats, err := refreshSearchCodeIndex(ctx, db, root)
	require.NoError(t, err)
	require.Equal(t, 16, stats.Pending)
	config.RegisterRuntimeCredentials("index-continuation-fixture", "new-continuation-credential")
	t.Cleanup(func() { config.RegisterRuntimeCredentials("index-continuation-fixture") })
	require.False(t, planIndexRefresh(root, true).partial)
	stats, err = refreshSearchCodeIndex(ctx, db, root)
	require.NoError(t, err)
	require.Equal(t, 16, stats.structuralPending, "credential rotation starts a new bounded policy pass")
	watcher.invalidate()
	require.Empty(t, watcher.state.pending)
	require.False(t, planIndexRefresh(root, true).partial)
}
