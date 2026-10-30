package tools

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestBackgroundBudgetCommitsProgressAndDrainsQueue(t *testing.T) {
	_, db, watcher := continuationIndexFixture(t, 20)
	ctx := context.WithValue(toolContext(), indexBatchLimitKey{}, 128)
	// A tiny time budget deterministically yields after one read, not before it.
	ctx = context.WithValue(ctx, indexBatchWorkKey{}, indexBatchWork{duration: time.Nanosecond})
	for i := range 20 {
		stats, err := refreshSearchCodeIndex(ctx, db, watcher.root)
		require.NoError(t, err)
		require.Equal(t, 1, stats.Read)
		require.Equal(t, 19-i, stats.Pending)
	}
	require.True(t, watcher.state.known)
	require.Empty(t, watcher.state.pending)
}
func TestBackgroundByteBudgetYieldsAfterOneRead(t *testing.T) {
	_, db, watcher := continuationIndexFixture(t, 20)
	ctx := context.WithValue(toolContext(), indexBatchLimitKey{}, 128)
	ctx = context.WithValue(ctx, indexBatchWorkKey{}, indexBatchWork{bytes: 1})
	stats, err := refreshSearchCodeIndex(ctx, db, watcher.root)
	require.NoError(t, err)
	require.Equal(t, 1, stats.Read)
	require.Equal(t, 19, stats.Pending)
}
