package shell

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestNativeSmokeSandboxAndCredentialDenial(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	for _, name := range []string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME"} {
		t.Setenv(name, home)
	}
	t.Setenv("LOCAL_ENDPOINT", "")
	t.Setenv("OPCODE_NATIVE_SMOKE_SECRET", "fake-never-forward-credential")
	file := filepath.Join(root, "config.json")
	require.NoError(t, os.WriteFile(file, []byte(`{"security":{"sandbox":"bubblewrap"}}`), 0600))
	oldFile := viper.ConfigFileUsed()
	viper.SetConfigFile(file)
	t.Cleanup(func() { viper.SetConfigFile(oldFile) })
	cfg, err := config.LoadOffline(root)
	require.NoError(t, err)
	old := cfg.Security
	cfg.Security = config.SecurityConfig{Sandbox: "bubblewrap"}
	t.Cleanup(func() { cfg.Security = old })
	command, err := Command(context.Background(), root, "never execute this fixture")
	if runtime.GOOS != "linux" {
		require.ErrorContains(t, err, "OS sandbox is unavailable")
		require.Nil(t, command)
	} else if err == nil {
		require.Equal(t, "bwrap", filepath.Base(command.Path))
		require.Contains(t, command.Args, "--unshare-all")
	} else {
		require.ErrorContains(t, err, "bubblewrap is required")
	}
	for _, value := range Environment() {
		require.False(t, strings.Contains(value, "fake-never-forward-credential"))
	}
}
