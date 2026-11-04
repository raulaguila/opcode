package shell

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/stretchr/testify/require"
)

func TestProjectConfigIsSourceAndTheHomesIsNot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	file := filepath.Join(root, "config.json")
	require.NoError(t, os.WriteFile(file, []byte(`{"security":{"sandbox":"bubblewrap"}}`), 0o600))
	cfg, err := config.LoadOffline(root)
	require.NoError(t, err)
	old := cfg.WorkingDir
	t.Cleanup(func() { cfg.WorkingDir = old })

	shared := filepath.Join(home, ".config")
	for _, tc := range []struct {
		workspace, path string
		sensitive       bool
	}{
		{"/work/app", "/work/app/.config/tool.yaml", false},
		{filepath.Join(shared, "nvim"), filepath.Join(shared, "nvim", "init.lua"), false},
		{home, filepath.Join(shared, "gh", "hosts.yml"), true},
		{"/work/app", filepath.Join(shared, "gh", "hosts.yml"), true},
		{"/work/app", "/other/.config/token", true},
		{"/work/app", "/work/app/.ssh/config", true},
		{"/work/app", "/work/app/.config/.env", true},
		{"/work/app", "/work/app/.netrc", true},
		{"/work/app", "/work/app/.git-credentials", true},
		{"/work/app", "/work/app/.pgpass", true},
		{"/work/app", "/work/app/id_ecdsa", true},
		{"/work/app", "/work/app/id_dsa", true},
		{"/work/app", "/work/app/deploy/client.p12", true},
		{"/work/app", "/work/app/deploy/client.pfx", true},
		{"/work/app", "/work/app/.docker/config.json", true},
		{"/work/app", "/work/app/docker/compose.yaml", false},
	} {
		cfg.WorkingDir = tc.workspace
		require.Equal(t, tc.sensitive, SensitivePath(tc.path), tc.path)
	}
}

func TestMaskCacheStaysBounded(t *testing.T) {
	maskCacheMu.Lock()
	clear(maskCache)
	maskCacheMu.Unlock()
	root := t.TempDir()
	for range maskCacheLimit + 5 {
		dir, err := os.MkdirTemp(root, "w")
		require.NoError(t, err)
		_, err = credentialMasks(dir, false, false, nil)
		require.NoError(t, err)
	}
	maskCacheMu.Lock()
	defer maskCacheMu.Unlock()
	require.LessOrEqual(t, len(maskCache), maskCacheLimit)
}
