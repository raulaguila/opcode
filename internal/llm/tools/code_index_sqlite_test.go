package tools

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodeIndexFTS5SurvivesConnectionReplacement(t *testing.T) {
	root := productivityV2Workspace(t)
	db, err := openCodeIndex(toolContext(), root)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.Exec(`INSERT INTO chunks(content) VALUES ('reconnecttoken')`)
	require.NoError(t, err)
	// Force the next query onto a fresh connection to the existing index.
	db.SetMaxIdleConns(0)
	var count int
	err = db.QueryRow(`SELECT count(*) FROM chunks WHERE chunks MATCH 'reconnecttoken'`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}
