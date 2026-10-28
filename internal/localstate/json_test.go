package localstate

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrivateStateRoundTripLimitsAndModes(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "private", "nested")
	path := filepath.Join(directory, "state.json")
	require.NoError(t, Write(path, map[string]string{"text": "private"}, 1024))
	var result map[string]string
	require.NoError(t, Read(path, 1024, &result))
	require.Equal(t, "private", result["text"])
	info, err := os.Stat(path)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		require.Equal(t, os.FileMode(0600), info.Mode().Perm())
		info, err = os.Stat(directory)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0700), info.Mode().Perm())
	}
	require.Error(t, Read(path, 2, &result))
	require.Error(t, Write(path, "too long", 2))
	require.NoError(t, Read(path, 1024, &result))
	require.Equal(t, "private", result["text"])
}
func TestPrivateStateRejectsAncestorAndFinalSymlinks(t *testing.T) {
	parent, outside := t.TempDir(), t.TempDir()
	link := filepath.Join(parent, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	require.Error(t, Write(filepath.Join(link, "nested", "state.json"), map[string]string{"text": "must not escape"}, 1024))
	_, err := os.Stat(filepath.Join(outside, "nested"))
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NoError(t, os.WriteFile(filepath.Join(outside, "state.json"), []byte(`{"text":"outside"}`), 0600))
	var value map[string]string
	require.Error(t, Read(filepath.Join(link, "state.json"), 1024, &value))
	destination := filepath.Join(parent, "destination.json")
	require.NoError(t, os.Symlink(filepath.Join(outside, "state.json"), destination))
	require.Error(t, Read(destination, 1024, &value))
	require.Error(t, Write(destination, map[string]string{"text": "replaced"}, 1024))
	raw, err := os.ReadFile(filepath.Join(outside, "state.json"))
	require.NoError(t, err)
	require.Equal(t, `{"text":"outside"}`, string(raw))
}
func TestPinnedDirectoryStaysBoundToOriginalAfterPathSwap(t *testing.T) {
	parent := t.TempDir()
	directory := filepath.Join(parent, "private")
	require.NoError(t, os.Mkdir(directory, 0700))
	root, err := OpenPrivateRoot(directory, false)
	require.NoError(t, err)
	defer root.Close()
	retained := filepath.Join(parent, "retained")
	if err := os.Rename(directory, retained); err != nil {
		t.Skipf("open-directory renames unavailable: %v", err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, directory); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	file, err := root.OpenFile("safe", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	require.NoError(t, err)
	_, err = file.WriteString("still pinned")
	require.NoError(t, err)
	require.NoError(t, file.Close())
	raw, err := os.ReadFile(filepath.Join(retained, "safe"))
	require.NoError(t, err)
	require.Equal(t, "still pinned", string(raw))
	_, err = os.Stat(filepath.Join(outside, "safe"))
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = OpenPrivateRoot(directory, false)
	require.Error(t, err)
}
