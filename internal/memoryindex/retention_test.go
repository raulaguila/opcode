package memoryindex

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRetentionLimitTieBreakersAndAtomicFailure(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	project := "/retention-fixture"
	db, err := open(ctx, directory, project)
	require.NoError(t, err)
	defer db.Close()
	runRetentionPrune(t, db)
	requireRetentionCount(t, db, 0)
	seedRetentionDocuments(t, db, maxDocuments-1)
	runRetentionPrune(t, db)
	requireRetentionCount(t, db, maxDocuments-1)

	insertRetentionDocument(t, db, "zz-last", 1)
	runRetentionPrune(t, db)
	requireRetentionCount(t, db, maxDocuments)
	requireRetentionID(t, db, "zz-last", true)

	insertRetentionDocument(t, db, "yy-tie", 1)
	runRetentionPrune(t, db)
	requireRetentionCount(t, db, maxDocuments)
	requireRetentionID(t, db, "zz-last", false)
	requireRetentionID(t, db, "yy-tie", true)

	insertRetentionDocument(t, db, "newer", 2)
	runRetentionPrune(t, db)
	requireRetentionCount(t, db, maxDocuments)
	requireRetentionID(t, db, "yy-tie", false)
	requireRetentionID(t, db, "newer", true)

	insertRetentionDocument(t, db, "000-oldest", 0)
	runRetentionPrune(t, db)
	requireRetentionCount(t, db, maxDocuments)
	requireRetentionID(t, db, "000-oldest", false)

	// Updating an existing document at the cap must not remove another row.
	require.NoError(t, Upsert(ctx, directory, project, Document{ID: "message:000001", Kind: "conversation", Session: "s", Stamp: 3, Text: "updatedevidence"}))
	requireRetentionCount(t, db, maxDocuments)
	found, _, err := Search(ctx, directory, project, "conversation", "s", "updatedevidence", 0, 2)
	require.NoError(t, err)
	require.Len(t, found, 1)
	require.Equal(t, "message:000001", found[0].ID)

	requireRetentionRollback(t, db, directory, project)
}

func seedRetentionDocuments(t *testing.T, db *sql.DB, count int) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.Exec(`WITH RECURSIVE seed(n) AS (VALUES(0) UNION ALL SELECT n+1 FROM seed WHERE n+1 < ?) INSERT INTO documents(id,kind,session,sources,stamp) SELECT printf('message:%06d',n),'conversation','s','',1 FROM seed`, count)
	require.NoError(t, err)
	_, err = tx.Exec(`INSERT INTO memory(id,text) SELECT id,'retained evidence' FROM documents`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
}
func insertRetentionDocument(t *testing.T, db *sql.DB, id string, stamp int64) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO documents(id,kind,session,sources,stamp) VALUES(?,'conversation','s','',?)`, id, stamp)
	require.NoError(t, err)
	_, err = tx.Exec(`INSERT INTO memory(id,text) VALUES(?,'retained evidence')`, id)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
}
func runRetentionPrune(t *testing.T, db *sql.DB) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	require.NoError(t, prune(context.Background(), tx))
	require.NoError(t, tx.Commit())
}
func requireRetentionCount(t *testing.T, db *sql.DB, want int) {
	t.Helper()
	for _, table := range []string{"documents", "memory"} {
		var count int
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM `+table).Scan(&count))
		require.Equal(t, want, count, table)
	}
}
func requireRetentionID(t *testing.T, db *sql.DB, id string, present bool) {
	t.Helper()
	want := 0
	if present {
		want = 1
	}
	for _, table := range []string{"documents", "memory"} {
		var count int
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM `+table+` WHERE id=?`, id).Scan(&count))
		require.Equal(t, want, count, table+"/"+id)
	}
}
func requireRetentionRollback(t *testing.T, db *sql.DB, directory, project string) {
	t.Helper()
	var oldest string
	require.NoError(t, db.QueryRow(`SELECT id FROM documents ORDER BY stamp ASC,id DESC LIMIT 1`).Scan(&oldest))
	_, err := db.Exec(`CREATE TRIGGER reject_retention BEFORE DELETE ON documents BEGIN SELECT RAISE(ABORT,'retention rollback fixture'); END`)
	require.NoError(t, err)
	err = Upsert(context.Background(), directory, project, Document{ID: "rolled-back", Kind: "conversation", Session: "s", Stamp: 4, Text: "rollbackmarker"})
	require.ErrorContains(t, err, "retention rollback fixture")
	requireRetentionCount(t, db, maxDocuments)
	requireRetentionID(t, db, oldest, true)
	requireRetentionID(t, db, "rolled-back", false)
	var matches int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM memory WHERE memory MATCH 'rollbackmarker'`).Scan(&matches))
	require.Zero(t, matches, "a failed retention delete must roll back the inserted FTS content too")
}
