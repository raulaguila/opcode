package shell

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRemoveCommandCacheClearsReadOnlyModules(t *testing.T) {
	cwd := t.TempDir()
	module := filepath.Join(commandCacheRoot(cwd), "go", "path", "pkg", "mod", "example@v1")
	require.NoError(t, os.MkdirAll(module, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(module, "a.go"), []byte("package a"), 0o444))
	require.NoError(t, os.Chmod(module, 0o555))
	require.EqualValues(t, 9, CommandCacheSize(cwd))
	trimmed, err := TrimCommandCache(cwd)
	require.NoError(t, err)
	require.False(t, trimmed, "under the limit nothing is removed")
	require.NoError(t, removeCommandCache(cwd))
	require.NoDirExists(t, commandCacheRoot(cwd))
	require.Zero(t, CommandCacheSize(cwd))
}

func TestPreparedCommandCacheIsIgnoredByGit(t *testing.T) {
	cwd := t.TempDir()
	_, err := prepareCommandCache(cwd)
	require.NoError(t, err)
	canonical, err := filepath.EvalSymlinks(cwd)
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(commandCacheRoot(canonical), ".gitignore"))
	require.NoError(t, err)
	require.Equal(t, "*\n", string(data))
}
