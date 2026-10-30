package tools

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBackgroundIndexAdvancesPastExpiredPrefix(t *testing.T) {
	root := productivityV2Workspace(t)
	for i := range 24 {
		retrievalFixture(t, root, fmt.Sprintf("source%03d.txt", i), "index progress fixture\n")
	}
	ctx := context.WithValue(toolContext(), indexBatchLimitKey{}, 8)
	for round := range 3 {
		status, err := ControlCodeIndex(ctx, "update", "")
		require.NoError(t, err)
		require.Equal(t, (round+1)*8, status.Files)
		require.Equal(t, 8, status.UpdatedFiles)
		require.Equal(t, 16, status.Pending, "expired entries remain pending too")
		// Simulate expiration between batches without sleeps or relaxed validity.
		db, err := openCodeIndex(ctx, root)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, "UPDATE files SET verified=verified-61")
		require.NoError(t, err)
		require.NoError(t, db.Close())
	}
	// Once expiration stops advancing, a fair pass also finishes revalidation.
	for range 2 {
		_, err := ControlCodeIndex(ctx, "update", "")
		require.NoError(t, err)
	}
	status, err := ControlCodeIndex(ctx, "update", "")
	require.NoError(t, err)
	require.Equal(t, 24, status.Files)
	require.Zero(t, status.Pending)
}
