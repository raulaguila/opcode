package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPipedInputReadsFilesButNotDevices(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diff")
	require.NoError(t, os.WriteFile(path, []byte("  diff --git a b\n"), 0o600))
	file, err := os.Open(path)
	require.NoError(t, err)
	defer file.Close()
	got, err := pipedInput(file)
	require.NoError(t, err)
	require.Equal(t, "diff --git a b", got)

	null, err := os.Open(os.DevNull)
	require.NoError(t, err)
	defer null.Close()
	got, err = pipedInput(null)
	require.NoError(t, err)
	require.Empty(t, got, "/dev/null is a device: not read, never blocks")

	big := filepath.Join(t.TempDir(), "big")
	require.NoError(t, os.WriteFile(big, []byte(strings.Repeat("x", 1<<20+1)), 0o600))
	file, err = os.Open(big)
	require.NoError(t, err)
	defer file.Close()
	_, err = pipedInput(file)
	require.Error(t, err)
}
