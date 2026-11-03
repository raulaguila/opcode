package shell

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCommandCacheDoesNotPrepareForReadOnlyProbesOrGit(t *testing.T) {
	root := t.TempDir()
	for _, probe := range []struct {
		ctx context.Context
		git bool
	}{{context.Background(), false}, {WithCommandCache(context.Background()), true}} {
		path, err := processCommandCache(probe.ctx, root, probe.git)
		require.NoError(t, err)
		require.Empty(t, path)
	}
	_, err := os.Stat(filepath.Join(root, ".opcode"))
	require.True(t, os.IsNotExist(err))
}

func TestCommandCachePreservesExplicitEnvironmentOverrides(t *testing.T) {
	cfg, err := config.LoadOffline(t.TempDir())
	require.NoError(t, err)
	old := cfg.Security
	t.Cleanup(func() { cfg.Security = old })
	cfg.Security.EnvAllow = []string{"GOCACHE"}
	custom := filepath.Join(os.TempDir(), "opcode-command-go-build")
	t.Setenv("GOCACHE", custom)
	directory := t.TempDir()
	env := commandCacheEnvironment(Environment(), directory)
	require.Contains(t, env, "GOCACHE="+custom)
	require.Contains(t, env, "GOPATH="+filepath.Join(directory, "path"))
	require.Contains(t, env, "GOMODCACHE="+filepath.Join(directory, "path", "pkg", "mod"))
	require.Contains(t, env, "HOME="+os.DevNull)
}

func TestCommandCacheIsProjectScopedAndRejectsSymlinks(t *testing.T) {
	first, err := prepareCommandCache(t.TempDir())
	require.NoError(t, err)
	second, err := prepareCommandCache(t.TempDir())
	require.NoError(t, err)
	require.NotEqual(t, first, second)
	root := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root, ".opcode")); err != nil {
		t.Skip("symlinks unavailable")
	}
	_, err = prepareCommandCache(root)
	require.ErrorContains(t, err, "symbolic links")
}

func TestSandboxCacheMountAndNetworkGrantDoNotExposePrivateParent(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux sandbox only")
	}
	root := t.TempDir()
	cache, err := prepareCommandCache(root)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(cache, ".env"), []byte("cache-secret"), 0600))
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "bwrap"), []byte("#!/bin/sh\nexit 0\n"), 0700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx := WithApprovedNetwork(context.Background())
	command, err := sandboxCommand(ctx, root, "/bin/sh", []string{"-c", "true"}, nil, false, false, false, cache)
	require.NoError(t, err)
	require.Contains(t, command.Args, "--share-net")
	require.Contains(t, command.Args, "--unshare-all")
	require.Contains(t, command.Args, cache)
	require.Contains(t, command.Args, sandboxCachePath)
	require.Contains(t, command.Args, filepath.Join(sandboxCachePath, ".env"), "credential paths remain masked inside the mounted cache")
	require.NotContains(t, command.Args, filepath.Dir(cache), "only the cache, not private application data, is mounted")
	_, marker := context.Background().Value(commandNetworkKey{}).(bool)
	require.False(t, marker)
}
