package memoryindex

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOwnedStoreReusesConnectionAndCloses(t *testing.T) {
	ctx := context.Background()
	directory, project := t.TempDir(), "/owned-projection"
	store := NewStore(directory, project)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	doc := Document{ID: "message:one", Kind: "conversation", Session: "session", Text: "originalmarker", Stamp: 1}
	require.NoError(t, store.Upsert(ctx, doc))
	first := store.db
	doc.Text = "updatedmarker"
	require.NoError(t, store.Upsert(ctx, doc))
	require.Same(t, first, store.db)
	items, _, err := Search(ctx, directory, project, "conversation", "session", "updatedmarker", 0, 2)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.NoError(t, store.Remove(ctx, doc.ID))
	items, _, err = Search(ctx, directory, project, "conversation", "session", "updatedmarker", 0, 2)
	require.NoError(t, err)
	require.Empty(t, items)
	require.NoError(t, store.Close())
	require.Zero(t, first.Stats().OpenConnections)
	require.NoError(t, store.Close())
	require.ErrorIs(t, store.Upsert(ctx, doc), context.Canceled)
	require.ErrorIs(t, store.Remove(ctx, doc.ID), context.Canceled)
}

func TestOwnedStoreCancellationAndShutdown(t *testing.T) {
	store := NewStore(t.TempDir(), "/owned-cancellation")
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	doc := Document{ID: "message:one", Kind: "conversation", Text: "evidence"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, store.Upsert(ctx, doc), context.Canceled)
	require.Nil(t, store.db, "a canceled caller must not initialize the projection")
	require.NoError(t, store.Upsert(context.Background(), doc))
	first := store.db
	started, finished := make(chan struct{}), make(chan error, 1)
	go func() {
		finished <- store.withDB(context.Background(), func(ctx context.Context, db *sql.DB) error {
			close(started)
			_, err := db.ExecContext(ctx, `WITH RECURSIVE busy(n) AS (VALUES(0) UNION ALL SELECT n+1 FROM busy WHERE n<1000000000) SELECT sum(n) FROM busy`)
			return err
		})
	}()
	<-started
	require.ErrorIs(t, store.Upsert(ctx, doc), context.Canceled, "waiting for the owner must honor caller cancellation")
	require.NoError(t, store.Close(), "shutdown cancels its active database operation before waiting")
	require.Error(t, <-finished)
	require.Zero(t, first.Stats().OpenConnections)
}

func TestOwnedStoreReopensChangedProjectionIdentity(t *testing.T) {
	for _, replaceRoot := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "directory"}[replaceRoot], func(t *testing.T) {
			directory, project := t.TempDir(), "/owned-replacement"
			store := NewStore(directory, project)
			t.Cleanup(func() { require.NoError(t, store.Close()) })
			doc := Document{ID: "message:old", Kind: "conversation", Text: "oldmarker"}
			require.NoError(t, store.Upsert(context.Background(), doc))
			first := store.db
			dir, name := projectionPath(directory, project)
			path := filepath.Join(dir, name)
			if replaceRoot {
				path = dir
			}
			require.NoError(t, os.Rename(path, path+".old"))
			doc.ID, doc.Text = "message:new", "newmarker"
			require.NoError(t, store.Upsert(context.Background(), doc))
			require.NotSame(t, first, store.db)
			require.Zero(t, first.Stats().OpenConnections)
			items, _, err := Search(context.Background(), directory, project, "conversation", "", "oldmarker", 0, 2)
			require.NoError(t, err)
			require.Empty(t, items, "replacement is a disposable projection, not a stale open database")
			items, _, err = Search(context.Background(), directory, project, "conversation", "", "newmarker", 0, 2)
			require.NoError(t, err)
			require.Len(t, items, 1)
		})
	}
}

func TestOwnedStoreRejectsChangedPrivatePaths(t *testing.T) {
	for _, replaceRoot := range []bool{false, true} {
		t.Run(map[bool]string{false: "file-symlink", true: "ancestor-symlink"}[replaceRoot], func(t *testing.T) {
			directory, project := t.TempDir(), "/owned-private-path"
			store := NewStore(directory, project)
			t.Cleanup(func() { require.NoError(t, store.Close()) })
			doc := Document{ID: "message:one", Kind: "conversation", Text: "evidence"}
			require.NoError(t, store.Upsert(context.Background(), doc))
			first := store.db
			dir, name := projectionPath(directory, project)
			path := filepath.Join(dir, name)
			if replaceRoot {
				path = dir
			}
			require.NoError(t, os.Rename(path, path+".original"))
			require.NoError(t, os.Symlink(path+".original", path))
			require.Error(t, store.Upsert(context.Background(), doc))
			require.Zero(t, first.Stats().OpenConnections)
			require.Nil(t, store.db)
			require.NoError(t, os.Remove(path))
			require.NoError(t, os.Rename(path+".original", path))
			require.NoError(t, store.Upsert(context.Background(), doc), "a repaired private path may be reopened")
		})
	}
}

func TestOwnedStorePreservesPrivateModeAndBounds(t *testing.T) {
	directory, project := t.TempDir(), "/owned-bounds"
	store := NewStore(directory, project)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	doc := Document{ID: "message:one", Kind: "conversation", Text: strings.Repeat("x", 8193)}
	require.Error(t, store.Upsert(context.Background(), doc))
	require.Nil(t, store.db)
	doc.Text = "evidence"
	require.NoError(t, store.Upsert(context.Background(), doc))
	dir, name := projectionPath(directory, project)
	path := filepath.Join(dir, name)
	require.NoError(t, os.Chmod(path, 0644))
	require.NoError(t, store.Upsert(context.Background(), doc))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestOwnedStoreRetentionFailureRollsBackAndRemainsUsable(t *testing.T) {
	ctx := context.Background()
	store := NewStore(t.TempDir(), "/owned-retention")
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.withDB(ctx, func(_ context.Context, db *sql.DB) error {
		seedRetentionDocuments(t, db, maxDocuments)
		return nil
	}))
	require.NoError(t, store.Upsert(ctx, Document{ID: "message:000000", Kind: "conversation", Session: "s", Text: "evidence", Stamp: 2}))
	first := store.db
	_, err := store.db.Exec(`CREATE TRIGGER reject_retention BEFORE DELETE ON documents BEGIN SELECT RAISE(ABORT,'owned retention failure'); END`)
	require.NoError(t, err)
	doc := Document{ID: "newer", Kind: "conversation", Session: "s", Text: "newmarker", Stamp: 3}
	require.ErrorContains(t, store.Upsert(ctx, doc), "owned retention failure")
	requireRetentionCount(t, store.db, maxDocuments)
	requireRetentionID(t, store.db, "newer", false)
	_, err = store.db.Exec(`DROP TRIGGER reject_retention`)
	require.NoError(t, err)
	require.NoError(t, store.Upsert(ctx, doc))
	require.Same(t, first, store.db)
	requireRetentionCount(t, store.db, maxDocuments)
	requireRetentionID(t, store.db, "message:099999", false)
	requireRetentionID(t, store.db, "newer", true)
}
