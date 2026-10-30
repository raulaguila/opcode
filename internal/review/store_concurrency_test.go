package review

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/stretchr/testify/require"
)

type blockingReviewPermissions struct {
	permission.Service
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *blockingReviewPermissions) Request(ctx context.Context, _ permission.CreatePermissionRequest) bool {
	p.once.Do(func() { close(p.entered) })
	select {
	case <-p.release:
		return true
	case <-ctx.Done():
		return false
	}
}
func TestUndoApprovalDoesNotHoldJournalWhileAnotherCommitRecords(t *testing.T) {
	store, root := reviewStore(t)
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	require.NoError(t, os.WriteFile(a, []byte("a-new"), 0600))
	require.NoError(t, os.WriteFile(b, []byte("b-old"), 0600))
	require.NoError(t, store.Record("s", "turn-a", []tools.FileChange{{Path: a, Before: new("a-old"), After: new("a-new")}}))
	preview, err := store.Preview("s", "turn-a", false)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	permissions := &blockingReviewPermissions{entered: make(chan struct{}), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() { done <- store.Apply(ctx, "s", preview, permissions, nil) }()
	select {
	case <-permissions.entered:
	case <-ctx.Done():
		t.Fatal("undo did not reach approval")
	}
	committed := make(chan error, 1)
	go func() {
		run := tools.WithChangeObserver(ctx, func(changes []tools.FileChange) error { return store.Record("other", "turn-b", changes) })
		committed <- tools.ApplyFileChanges(run, []tools.FileChange{{Path: b, Before: new("b-old"), After: new("b-new")}})
	}()
	select {
	case err := <-committed:
		require.NoError(t, err)
	case <-ctx.Done():
		close(permissions.release)
		t.Fatal("journal/file transaction lock ordering deadlocked")
	}
	close(permissions.release)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("undo did not complete")
	}
	raw, err := os.ReadFile(a)
	require.NoError(t, err)
	require.Equal(t, "a-old", string(raw))
	raw, err = os.ReadFile(b)
	require.NoError(t, err)
	require.Equal(t, "b-new", string(raw))
	turns, err := store.List("other")
	require.NoError(t, err)
	require.Len(t, turns, 1)
}
