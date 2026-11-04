package session

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCatalogForgetsOldestEntryWithoutDraftAtTheLimit(t *testing.T) {
	dir := t.TempDir()
	c := NewCatalog(dir, dir)
	require.NoError(t, c.SaveDraft("draft", "keep me"))
	require.NoError(t, c.Archive("archived", true))
	for i := range 498 {
		require.NoError(t, c.Activate(fmt.Sprint("s", i)))
	}
	require.NoError(t, c.Activate("new"))
	sessions, _, err := c.Snapshot()
	require.NoError(t, err)
	require.Len(t, sessions, 500)
	require.Equal(t, "keep me", sessions["draft"].Draft)
	require.True(t, sessions["archived"].Archived, "a live entry goes before an archived one")
	require.NotContains(t, sessions, "s0")
	require.Contains(t, sessions, "new")
}
