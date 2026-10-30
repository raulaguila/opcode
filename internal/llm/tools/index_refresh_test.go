package tools

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/stretchr/testify/require"
)

func watchedIndexFixture(t *testing.T, count int) (string, *sql.DB, *IndexWatcher) {
	t.Helper()
	root := productivityV2Workspace(t)
	for i := range count {
		retrievalFixture(t, root, fmt.Sprintf("source%d.txt", i), "initial fixture text\n")
	}
	previous := config.Get().Index
	background := false
	config.Get().Index.Background = &background
	t.Cleanup(func() { config.Get().Index = previous })
	watcher := StartIndexWatcher(toolContext())
	t.Cleanup(watcher.Close)
	require.Eventually(t, func() bool {
		watcher.mu.Lock()
		defer watcher.mu.Unlock()
		return watcher.state.covered
	}, 3*time.Second, 5*time.Millisecond)
	db, err := openCodeIndex(toolContext(), root)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	stats, err := refreshSearchCodeIndex(toolContext(), db, root)
	require.NoError(t, err)
	require.Equal(t, count, stats.Read)
	return root, db, watcher
}

func waitIndexDirty(t *testing.T, watcher *IndexWatcher, path string) {
	t.Helper()
	require.Eventually(t, func() bool {
		watcher.mu.Lock()
		defer watcher.mu.Unlock()
		return watcher.state.dirty[path] > 0
	}, 3*time.Second, 5*time.Millisecond)
}

func TestWatchedIndexRefreshesOnlyChangedPaths(t *testing.T) {
	root, db, watcher := watchedIndexFixture(t, 30)
	ctx := toolContext()
	stats, err := refreshSearchCodeIndex(ctx, db, root)
	require.NoError(t, err)
	require.Equal(t, indexRefresh{}, stats, "warm covered snapshot must do no discovery or per-file refresh")
	path := filepath.Join(root, "source0.txt")
	before, err := os.Stat(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("changed fixture text\n"), 0600))
	require.NoError(t, os.Chtimes(path, before.ModTime(), before.ModTime()))
	waitIndexDirty(t, watcher, path)
	stats, err = refreshSearchCodeIndex(ctx, db, root)
	require.NoError(t, err)
	require.Equal(t, 1, stats.Read)
	require.Equal(t, 1, stats.Updated)
	require.Zero(t, stats.Reused)
	var digest string
	require.NoError(t, db.QueryRow("SELECT digest FROM files WHERE path='source0.txt'").Scan(&digest))
	require.Equal(t, indexDigest("changed fixture text\n"), digest)
	retrievalFixture(t, root, "new.txt", "new fixture\n")
	waitIndexDirty(t, watcher, filepath.Join(root, "new.txt"))
	stats, err = refreshSearchCodeIndex(ctx, db, root)
	require.NoError(t, err)
	require.GreaterOrEqual(t, stats.Read, 1)
	require.LessOrEqual(t, stats.Read, 2, "late events may recheck the previously edited path")
	require.Zero(t, stats.Reused)
	require.NoError(t, os.Remove(path))
	waitIndexDirty(t, watcher, path)
	stats, err = refreshSearchCodeIndex(ctx, db, root)
	require.NoError(t, err)
	require.LessOrEqual(t, stats.Read, 1, "a late creation event may recheck the new file")
	require.Zero(t, stats.Reused)
	var count int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM files").Scan(&count))
	require.Equal(t, 30, count)
	watcher.Close()
	_, registered := indexWatchers.Load(root)
	require.False(t, registered)
	retrievalFixture(t, root, "unwatched.txt", "immediate fallback fixture\n")
	stats, err = refreshSearchCodeIndex(ctx, db, root)
	require.NoError(t, err)
	require.Equal(t, 1, stats.Read)
	require.Equal(t, 30, stats.Reused)
}

func TestWatchedIndexPolicyRotationAndExplicitUpdates(t *testing.T) {
	root, db, watcher := watchedIndexFixture(t, 12)
	ctx := toolContext()
	secret := "newly-registered-index-secret"
	retrievalFixture(t, root, "secret.txt", "credential fixture "+secret+"\n")
	waitIndexDirty(t, watcher, filepath.Join(root, "secret.txt"))
	_, err := refreshSearchCodeIndex(ctx, db, root)
	require.NoError(t, err)
	config.RegisterRuntimeCredentials("watcher-regression", secret)
	t.Cleanup(func() { config.RegisterRuntimeCredentials("watcher-regression") })
	stats, err := refreshSearchCodeIndex(ctx, db, root)
	require.NoError(t, err)
	require.Equal(t, 13, stats.Read, "credential changes must invalidate every indexed preview without filesystem events")
	var content string
	require.NoError(t, db.QueryRow("SELECT content FROM chunks WHERE path='secret.txt'").Scan(&content))
	require.NotContains(t, content, secret)
	require.Contains(t, content, "[REDACTED]")
	stats, err = refreshCodeIndex(ctx, db, root)
	require.NoError(t, err)
	require.Equal(t, 13, stats.Reused, "explicit updates still perform full discovery")
	_, err = db.Exec("UPDATE files SET verified=verified-61")
	require.NoError(t, err)
	watcher.mu.Lock()
	watcher.state.reconciled = time.Now().Add(-indexReconcileInterval)
	watcher.mu.Unlock()
	stats, err = refreshSearchCodeIndex(ctx, db, root)
	require.NoError(t, err)
	require.Equal(t, 13, stats.Read, "periodic full reconciliation revalidates expired contents")
	retrievalFixture(t, root, ".opcodeignore", "*.txt\n")
	require.Eventually(t, func() bool { return !planIndexRefresh(root, true).partial }, 3*time.Second, 5*time.Millisecond)
	_, err = refreshSearchCodeIndex(ctx, db, root)
	require.NoError(t, err)
	var count int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM chunks").Scan(&count))
	require.Zero(t, count)
}

func TestWatchedIndexDeltaRetainsTextAndFileLimits(t *testing.T) {
	root, db, watcher := watchedIndexFixture(t, 2)
	previous := config.Get().Index
	t.Cleanup(func() { config.Get().Index = previous })
	config.Get().Index.MaxTextBytes = 60
	_, err := refreshSearchCodeIndex(toolContext(), db, root)
	require.NoError(t, err)
	path := filepath.Join(root, "source0.txt")
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("x", 50)), 0600))
	waitIndexDirty(t, watcher, path)
	_, err = refreshSearchCodeIndex(toolContext(), db, root)
	require.ErrorContains(t, err, "text exceeds index limit")
	var digest string
	require.NoError(t, db.QueryRow("SELECT digest FROM files WHERE path='source0.txt'").Scan(&digest))
	require.Equal(t, indexDigest("initial fixture text\n"), digest, "failed delta must roll back")
	watcher.mu.Lock()
	watcher.state.known = true
	watcher.state.policy = indexRefreshPolicy()
	watcher.state.inventory = make(map[string]bool)
	for i := range indexLimits().MaxFiles {
		watcher.state.inventory[fmt.Sprint(i)] = true
	}
	watcher.mu.Unlock()
	require.ErrorContains(t, planIndexRefresh(root, true).checkFileLimit(root), "20000-file")
}

func TestIndexWatcherUnknownCoverageAndOverflowForceDiscovery(t *testing.T) {
	root, _, watcher := watchedIndexFixture(t, 1)
	require.True(t, planIndexRefresh(root, true).partial)
	watcher.invalidate()
	require.False(t, planIndexRefresh(root, true).partial)
	watcher.mu.Lock()
	watcher.state.covered, watcher.state.known = true, true
	for i := range maxIndexDirtyPaths {
		watcher.state.dirty[fmt.Sprint(i)] = 1
	}
	watcher.mu.Unlock()
	watcher.recordEvent(fsnotify.Event{Name: filepath.Join(root, "source0.txt"), Op: fsnotify.Write})
	require.False(t, planIndexRefresh(root, true).partial)
	watcher.mu.Lock()
	require.Empty(t, watcher.state.dirty)
	watcher.mu.Unlock()
	watcher.stopCoverage()
	watch, err := fsnotify.NewWatcher()
	require.NoError(t, err)
	defer watch.Close()
	watcher.syncDirectories(context.Background(), watch)
	watcher.mu.Lock()
	covered := watcher.state.covered
	watcher.mu.Unlock()
	require.False(t, covered, "closed event channels cannot regain trusted coverage")
}

func TestIndexWatcherCoversEmptyDirectoriesAndStops(t *testing.T) {
	root, _, watcher := watchedIndexFixture(t, 1)
	require.NoError(t, os.Mkdir(filepath.Join(root, "empty"), 0700))
	require.Eventually(t, func() bool { return !planIndexRefresh(root, true).partial }, 3*time.Second, 5*time.Millisecond)
	enabled := true
	config.Get().Index.Background = &enabled
	require.Eventually(t, func() bool {
		watcher.mu.Lock()
		defer watcher.mu.Unlock()
		return watcher.state.covered && watcher.state.known
	}, 3*time.Second, 5*time.Millisecond)
	path := filepath.Join(root, "empty", "new.txt")
	require.NoError(t, os.WriteFile(path, []byte("nested fixture"), 0600))
	waitIndexDirty(t, watcher, path)
	watcher.Close()
	select {
	case <-watcher.done:
	default:
		t.Fatal("watcher did not join")
	}
	_, registered := indexWatchers.Load(root)
	require.False(t, registered)
}
