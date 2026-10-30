package localstate

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFileLockUsesStablePrivateSidecar(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.json")
	unlock, err := LockFile(path)
	require.NoError(t, err)
	require.NoError(t, Write(path, "first", 128))
	before, err := os.Stat(filepath.Join(filepath.Dir(path), ".state.json.lock"))
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		require.Equal(t, os.FileMode(0600), before.Mode().Perm())
	}
	unlock()
	unlock() // Cleanup may safely run after early release.
	unlock, err = LockFile(path)
	require.NoError(t, err)
	defer unlock()
	require.NoError(t, Write(path, "second", 128))
	after, err := os.Stat(filepath.Join(filepath.Dir(path), ".state.json.lock"))
	require.NoError(t, err)
	require.True(t, os.SameFile(before, after), "JSON replacement must not replace the lock inode")
}

func TestFileLockRejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	require.NoError(t, os.WriteFile(outside, []byte("unchanged"), 0600))
	if err := os.Symlink(outside, filepath.Join(root, ".state.json.lock")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := LockFile(filepath.Join(root, "state.json"))
	require.Error(t, err)
	content, err := os.ReadFile(outside)
	require.NoError(t, err)
	require.Equal(t, "unchanged", string(content))
}
