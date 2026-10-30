package message

import (
	"context"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/memoryindex"
	"github.com/stretchr/testify/require"
)

func TestMessageCloseStopsProjectionAndSubscribers(t *testing.T) {
	ctx := context.Background()
	directory, project := t.TempDir(), "/message-owner"
	// Cold WASM initialization under -race can exceed the projection deadline.
	_, _, err := memoryindex.Search(ctx, directory, project, "conversation", "session", "warmup", 0, 1)
	require.NoError(t, err)
	s := NewService(nil, WithSearchIndex(directory, project, func(text string) string {
		return strings.ReplaceAll(text, "secret", "[redacted]")
	})).(*service)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	events := s.Subscribe(ctx)
	msg := Message{ID: "one", SessionID: "session", Role: User, Parts: []ContentPart{TextContent{Text: "secret committedmarker"}}}
	s.projectMessage(ctx, msg)
	items, _, err := memoryindex.Search(ctx, directory, project, "conversation", "session", "committedmarker", 0, 2)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.NotContains(t, items[0].Text, "secret")
	require.NoError(t, s.Close())
	_, open := <-events
	require.False(t, open, "message shutdown also closes broker subscriptions")
	require.ErrorIs(t, s.index.store.Remove(ctx, "message:one"), context.Canceled)
	require.NoError(t, s.Close())
}
