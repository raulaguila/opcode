package memoryindex

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMemoryFTS5SurvivesConnectionReplacement(t *testing.T) {
	db, err := open(context.Background(), t.TempDir(), "/project")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.Exec(`INSERT INTO memory(id,text) VALUES ('one','reconnecttoken')`)
	require.NoError(t, err)
	// Force the next query onto a fresh connection to the existing index.
	db.SetMaxIdleConns(0)
	var count int
	err = db.QueryRow(`SELECT count(*) FROM memory WHERE memory MATCH 'reconnecttoken'`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}
