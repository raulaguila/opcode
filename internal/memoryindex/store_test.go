package memoryindex

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProjectIsolationAndUpdates(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	d := Document{ID: "message:one", Kind: "conversation", Session: "session", Text: "billing authentication decision", Stamp: 1}
	require.NoError(t, Upsert(ctx, dir, "/project-a", d))
	items, more, err := Search(ctx, dir, "/project-a", "conversation", "", "authentication", 0, 2)
	require.NoError(t, err)
	require.False(t, more)
	require.Len(t, items, 1)
	items, _, err = Search(ctx, dir, "/project-b", "conversation", "", "authentication", 0, 2)
	require.NoError(t, err)
	require.Empty(t, items)
	d.Text = "changed decision"
	require.NoError(t, Upsert(ctx, dir, "/project-a", d))
	items, _, err = Search(ctx, dir, "/project-a", "conversation", "", "authentication", 0, 2)
	require.NoError(t, err)
	require.Empty(t, items)
}
