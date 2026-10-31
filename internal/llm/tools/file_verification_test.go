package tools

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestAffectedFileVerificationIgnoresUnrelatedSizeAndDetectsSameSizeChanges(t *testing.T) {
	root := workspaceTestDir(t)
	artifact, err := os.Create(filepath.Join(root, "opcode"))
	require.NoError(t, err)
	require.NoError(t, artifact.Truncate(17<<20))
	require.NoError(t, artifact.Close())
	_, err = WorkspaceDigest(context.Background())
	require.ErrorContains(t, err, "fingerprint limit")
	path := filepath.Join(root, "created.txt")
	require.NoError(t, os.WriteFile(path, []byte("one"), 0600))
	receipts := []workflow.FileReceipt{{Path: path, After: fmt.Sprintf("%x", sha256.Sum256([]byte("one")))}}
	require.NoError(t, VerifyFileReceipts(context.Background(), receipts))
	stamp, err := os.Stat(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("two"), 0600))
	require.NoError(t, os.Chtimes(path, stamp.ModTime(), stamp.ModTime()))
	require.ErrorContains(t, VerifyFileReceipts(context.Background(), receipts), "content changed")
}

func TestAffectedFileVerificationRequiresExactDeletionAndRejectsSymlinks(t *testing.T) {
	root := workspaceTestDir(t)
	path := filepath.Join(root, "removed.txt")
	require.NoError(t, VerifyFileFingerprint(context.Background(), path, ""))
	require.NoError(t, os.WriteFile(path, nil, 0600))
	require.Error(t, VerifyFileFingerprint(context.Background(), path, ""))
	require.NoError(t, os.Remove(path))
	target := filepath.Join(root, "target.txt")
	require.NoError(t, os.WriteFile(target, []byte("one"), 0600))
	require.NoError(t, os.Symlink(target, path))
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte("one")))
	require.ErrorContains(t, VerifyFileFingerprint(context.Background(), path, hash), "symlink")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, VerifyFileFingerprint(ctx, target, hash), context.Canceled)
}

func TestFileCommitObserverReceivesVerifiedCreateEditAndDelete(t *testing.T) {
	root := workspaceTestDir(t)
	before, after := "before", "after"
	edited, deleted, created := filepath.Join(root, "edit.txt"), filepath.Join(root, "delete.txt"), filepath.Join(root, "create.txt")
	for _, path := range []string{edited, deleted} {
		require.NoError(t, os.WriteFile(path, []byte(before), 0600))
	}
	calls := 0
	ctx := WithMutationObserver(toolContext(), func(changes []FileChange) error {
		calls++
		require.Len(t, changes, 3)
		for _, file := range changes {
			content, err := os.ReadFile(file.Path)
			if file.After == nil {
				require.ErrorIs(t, err, os.ErrNotExist)
			} else {
				require.NoError(t, err)
				require.Equal(t, *file.After, string(content))
			}
		}
		return nil
	})
	require.NoError(t, ApplyFileChanges(ctx, []FileChange{{Path: edited, Before: &before, After: &after}, {Path: deleted, Before: &before}, {Path: created, After: &after}}))
	require.Equal(t, 1, calls)
}
