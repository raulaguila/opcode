package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/stretchr/testify/require"
)

// grep, glob, ls and the project map all reach rg through the sandbox, so a
// host that refuses it must not leave the agent unable to read the project.
func TestSearchWorksWhenTheSandboxCannotStart(t *testing.T) {
	if cfg := config.Get(); cfg != nil {
		old := cfg.Security.Sandbox
		cfg.Security.Sandbox = "bubblewrap"
		t.Cleanup(func() { cfg.Security.Sandbox = old })
	}
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "bwrap"), []byte("#!/bin/sh\necho 'bwrap: setting up uid map: Permission denied' >&2\nexit 1\n"), 0o700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package main // needle\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("TOKEN=needle\n"), 0o600))

	files, _, err := findFiles(context.Background(), root, "", 100)
	require.NoError(t, err)
	require.Equal(t, []string{filepath.Join(root, "main.go")}, files, "the fallback keeps hiding credentials")

	matches, _, err := searchFiles(context.Background(), "needle", root, "", 100)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.Equal(t, filepath.Join(root, "main.go"), matches[0].path)
}
