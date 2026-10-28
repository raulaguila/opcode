package app

import (
	"context"
	"github.com/spf13/viper"
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/stretchr/testify/require"
)

func TestIntegrationProbeRequiresPermissionBeforeStartingExecutable(t *testing.T) {
	if config.Get() == nil {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("LOCAL_ENDPOINT", "")
		path := filepath.Join(t.TempDir(), "settings.json")
		require.NoError(t, os.WriteFile(path, []byte(`{}`), 0600))
		viper.SetConfigFile(path)
		_, err := config.Load(t.TempDir(), false)
		require.NoError(t, err)
	}
	app := &App{Permissions: permission.NewPermissionService()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := app.ProbeIntegration(ctx, config.Integration{Kind: "lsp", Name: "go", LSP: config.LSPConfig{Command: "must-never-be-launched"}})
	require.ErrorIs(t, err, context.Canceled)
	_, err = app.ProbeIntegration(context.Background(), config.Integration{Kind: "lsp", Name: "go", LSP: config.LSPConfig{Command: "must-never-be-launched"}})
	require.ErrorIs(t, err, permission.ErrorPermissionDenied)
}
