package review

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func reviewStore(t *testing.T) (*Store, string) {
	t.Helper()
	root := t.TempDir()
	if config.Get() == nil {
		settings := filepath.Join(root, "config.json")
		require.NoError(t, os.WriteFile(settings, []byte(`{}`), 0600))
		viper.SetConfigFile(settings)
		_, err := config.Load(root, false)
		require.NoError(t, err)
	}
	previous := config.Get().WorkingDir
	config.Get().WorkingDir = root
	t.Cleanup(func() { config.Get().WorkingDir = previous })
	return New(root, filepath.Join(root, ".opcode")), root
}

func TestUndoRedoConsolidatesWritesAndPreservesNewFileExistence(t *testing.T) {
	store, root := reviewStore(t)
	file := filepath.Join(root, "main.go")
	created := filepath.Join(root, "new.go")
	require.NoError(t, os.WriteFile(file, []byte("original"), 0644))
	ctx := tools.WithChangeObserver(context.Background(), func(changes []tools.FileChange) error { return store.Record("s", "turn-1", changes) })
	require.NoError(t, tools.ApplyFileChanges(ctx, []tools.FileChange{{Path: file, Before: new("original"), After: new("middle")}}))
	require.NoError(t, tools.ApplyFileChanges(ctx, []tools.FileChange{{Path: file, Before: new("middle"), After: new("final")}, {Path: created, Before: nil, After: new("new")}}))
	turns, err := store.List("s")
	require.NoError(t, err)
	require.Len(t, turns, 1)
	require.Len(t, turns[0].Changes, 2)
	require.Equal(t, "original", *turns[0].Changes[0].Before)
	preview, err := store.Preview("s", "", false)
	require.NoError(t, err)
	perms := permission.NewPermissionService()
	perms.AutoApproveSession("s")
	invalidated := 0
	require.NoError(t, store.Apply(context.Background(), "s", preview, perms, func() error { invalidated++; return nil }))
	raw, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, "original", string(raw))
	_, err = os.Stat(created)
	require.ErrorIs(t, err, os.ErrNotExist)
	redo, err := store.Preview("s", "", true)
	require.NoError(t, err)
	require.NoError(t, store.Apply(context.Background(), "s", redo, perms, nil))
	raw, err = os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, "final", string(raw))
	raw, err = os.ReadFile(created)
	require.NoError(t, err)
	require.Equal(t, "new", string(raw))
	require.Equal(t, 1, invalidated)
	info, err := os.Stat(store.path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
}
func TestUndoFailsClosedOnDenialConflictAndForgedPreview(t *testing.T) {
	store, root := reviewStore(t)
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	require.NoError(t, os.WriteFile(a, []byte("a2"), 0600))
	require.NoError(t, os.WriteFile(b, []byte("b2"), 0600))
	require.NoError(t, store.Record("s", "t", []tools.FileChange{{Path: a, Before: new("a1"), After: new("a2")}, {Path: b, Before: new("b1"), After: new("b2")}}))
	preview, err := store.Preview("s", "", false)
	require.NoError(t, err)
	perms := permission.NewPermissionService()
	require.ErrorIs(t, store.Apply(context.Background(), "s", preview, perms, nil), permission.ErrorPermissionDenied)
	perms.AutoApproveSession("s")
	require.NoError(t, os.WriteFile(b, []byte("manual"), 0600))
	require.Error(t, store.Apply(context.Background(), "s", preview, perms, nil))
	raw, err := os.ReadFile(a)
	require.NoError(t, err)
	require.Equal(t, "a2", string(raw), "no file changes if any preflight fails")
	require.NoError(t, os.WriteFile(b, []byte("b2"), 0600))
	preview.Changes[0].After = new("injected")
	require.NoError(t, store.Apply(context.Background(), "s", preview, perms, nil))
	raw, err = os.ReadFile(a)
	require.NoError(t, err)
	require.Equal(t, "a1", string(raw), "apply reloads journal rather than trusting UI bytes")
}
func TestRecordingFailureRollsBackAndCannotCrossSessions(t *testing.T) {
	store, root := reviewStore(t)
	file := filepath.Join(root, "a")
	require.NoError(t, os.WriteFile(file, []byte("v1"), 0600))
	require.NoError(t, store.Record("s", "t", []tools.FileChange{{Path: file, Before: new("old"), After: new("v1")}}))
	ctx := tools.WithChangeObserver(context.Background(), func(changes []tools.FileChange) error { return store.Record("s", "t", changes) })
	require.NoError(t, os.WriteFile(file, []byte("manual"), 0600))
	require.Error(t, tools.ApplyFileChanges(ctx, []tools.FileChange{{Path: file, Before: new("manual"), After: new("new")}}))
	raw, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, "manual", string(raw))
	_, err = store.Preview("other", "t", false)
	require.Error(t, err)
}
