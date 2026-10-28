package artifact

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrivatePersistentSessionPagesAndSearch(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "artifacts")
	s := New(dir)
	info, err := s.Put(context.Background(), "session-a", "bash", strings.Repeat("line one\n", 4000)+"needle-at-end")
	require.NoError(t, err)
	stat, err := os.Stat(dir)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0700), stat.Mode().Perm())
	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, files, 1)
	stat, err = files[0].Info()
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), stat.Mode().Perm())
	s = New(dir)
	page, err := s.Read(context.Background(), "session-a", info.ID, 0, 1<<20)
	require.NoError(t, err)
	require.Len(t, page.Content, MaxPage)
	require.True(t, page.More)
	_, err = s.Read(context.Background(), "session-b", info.ID, 0, 20)
	require.Error(t, err)
	matches, err := s.Search(context.Background(), "session-a", info.ID, "needle-at-end", 0, 10)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.Contains(t, matches[0].Content, "needle-at-end")
	_, err = s.Info(context.Background(), "session-a", "../secret")
	require.Error(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.Put(ctx, "session-a", "cancelled", "value")
	require.ErrorIs(t, err, context.Canceled)
}
func TestArtifactQuotaLinksAndTruncation(t *testing.T) {
	s := New(t.TempDir())
	info, err := s.Put(context.Background(), "s", "output", strings.Repeat("x", MaxContent+1))
	require.NoError(t, err)
	require.True(t, info.Truncated)
	require.Equal(t, MaxContent, info.Bytes)
	for range 31 {
		_, err = s.Put(context.Background(), "s", "output", "small")
		require.NoError(t, err)
	}
	_, err = s.Put(context.Background(), "s", "overflow", "small")
	require.ErrorContains(t, err, "quota")
	dir := t.TempDir()
	link := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink(s.directory, link))
	_, err = New(link).Put(context.Background(), "other", "label", "secret")
	require.Error(t, err)
}

func TestArtifactStorageRejectsAncestorSymlink(t *testing.T) {
	outside, parent := t.TempDir(), t.TempDir()
	link := filepath.Join(parent, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := New(filepath.Join(link, "artifacts")).Put(context.Background(), "s", "output", "private")
	require.Error(t, err)
	_, err = os.Stat(filepath.Join(outside, "artifacts"))
	require.ErrorIs(t, err, os.ErrNotExist)
}
