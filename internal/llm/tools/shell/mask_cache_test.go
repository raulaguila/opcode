package shell

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCredentialMasksReuseWalkUntilADirectoryChanges(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "node_modules", "pkg")
	require.NoError(t, os.MkdirAll(nested, 0o700))
	first := filepath.Join(root, ".env")
	require.NoError(t, os.WriteFile(first, nil, 0o600))
	past := time.Now().Add(-time.Hour)
	for _, dir := range []string{nested, filepath.Dir(nested), root} {
		require.NoError(t, os.Chtimes(dir, past, past))
	}

	masks, err := credentialMasks(root, false, false, nil)
	require.NoError(t, err)
	require.True(t, slices.Contains(masks, first))
	maskCacheMu.Lock()
	_, cached := maskCache[root]
	maskCacheMu.Unlock()
	require.True(t, cached, "a stable walk is reused")

	// A credential added deep inside a dependency must be masked on the next command.
	second := filepath.Join(nested, ".env")
	require.NoError(t, os.WriteFile(second, nil, 0o600))
	masks, err = credentialMasks(root, false, false, nil)
	require.NoError(t, err)
	require.True(t, slices.Contains(masks, second))
	maskCacheMu.Lock()
	_, cached = maskCache[root]
	maskCacheMu.Unlock()
	require.False(t, cached, "a walk racing recent changes is not reused")
}
