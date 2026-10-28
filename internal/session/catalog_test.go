package session

import (
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatalogPersistsSessionsWithoutRestoringGrants(t *testing.T) {
	root := t.TempDir()
	store := NewCatalog(root, ".opcode")
	require.NoError(t, store.Activate("first"))
	require.NoError(t, store.SaveDraft("first", "unfinished prompt"))
	require.NoError(t, store.Archive("first", true))
	require.NoError(t, store.Activate("second"))
	entries, last, err := NewCatalog(root, ".opcode").Snapshot()
	require.NoError(t, err)
	require.Equal(t, "second", last)
	require.True(t, entries["first"].Archived)
	require.Equal(t, "unfinished prompt", entries["first"].Draft)
	info, err := os.Stat(filepath.Join(root, ".opcode", "session-preferences.json"))
	require.NoError(t, err)
	require.Zero(t, info.Mode().Perm()&0077)
	require.Error(t, store.SaveDraft("first", strings.Repeat("x", 64001)))
	entries, _, err = store.Snapshot()
	require.NoError(t, err)
	require.Equal(t, "unfinished prompt", entries["first"].Draft)
}
func TestCatalogRefusesCorruptionAndWorkspaceMismatch(t *testing.T) {
	root := t.TempDir()
	data := t.TempDir()
	store := NewCatalog(root, data)
	require.NoError(t, store.Activate("s"))
	_, _, err := NewCatalog(t.TempDir(), data).Snapshot()
	require.Error(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(data, "session-preferences.json"), []byte("{bad secret"), 0600))
	require.Error(t, store.Activate("new"))
	body, err := os.ReadFile(filepath.Join(data, "session-preferences.json"))
	require.NoError(t, err)
	require.Equal(t, "{bad secret", string(body))
}
