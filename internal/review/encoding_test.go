package review

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/stretchr/testify/require"
)

func TestUndoRedoPreservesInvalidUTF8AndNULSnapshots(t *testing.T) {
	for _, values := range []struct {
		name          string
		before, after []byte
	}{{"invalid_before", []byte{'b', 0xff, 0, 0xfe, '\n'}, []byte("valid after\n")}, {"invalid_after", []byte("valid before\n"), []byte{'a', 0xc0, 0xaf, 0, 0xff, '\n'}}} {
		t.Run(values.name, func(t *testing.T) {
			store, root := reviewStore(t)
			path := filepath.Join(root, "bytes.dat")
			require.NoError(t, os.WriteFile(path, values.before, 0640))
			before, after := string(values.before), string(values.after)
			ctx := tools.WithChangeObserver(context.Background(), func(changes []tools.FileChange) error { return store.Record("bytes-session", "bytes-turn", changes) })
			require.NoError(t, tools.ApplyFileChanges(ctx, []tools.FileChange{{Path: path, Before: &before, After: &after}}))
			// Load from the actual JSON journal through a fresh Store and clone path.
			restored := New(root, filepath.Join(root, ".opcode"))
			turns, err := restored.List("bytes-session")
			require.NoError(t, err)
			require.Len(t, turns, 1)
			require.Equal(t, values.before, []byte(*turns[0].Changes[0].Before))
			require.Equal(t, values.after, []byte(*turns[0].Changes[0].After))
			permissions := permission.NewPermissionService()
			permissions.AutoApproveSession("bytes-session")
			preview, err := restored.Preview("bytes-session", "bytes-turn", false)
			require.NoError(t, err)
			require.NoError(t, restored.Apply(context.Background(), "bytes-session", preview, permissions, nil))
			raw, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, values.before, raw)
			redo, err := restored.Preview("bytes-session", "bytes-turn", true)
			require.NoError(t, err)
			require.NoError(t, restored.Apply(context.Background(), "bytes-session", redo, permissions, nil))
			raw, err = os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, values.after, raw)
			info, err := os.Stat(path)
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0640), info.Mode().Perm())
		})
	}
}
