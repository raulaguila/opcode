package app

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/lsp"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestLazyLSPMissingExecutableWarnsOnceAndRetriesAfterInstallation(t *testing.T) {
	if config.Get() == nil {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		path := filepath.Join(t.TempDir(), "settings.json")
		require.NoError(t, os.WriteFile(path, []byte(`{}`), 0600))
		viper.SetConfigFile(path)
		_, err := config.LoadOffline(t.TempDir())
		require.NoError(t, err)
	}
	cfg := config.Get()
	previousLSP, previousDir, previousSandbox, previousDebug := cfg.LSP, cfg.WorkingDir, cfg.Security.Sandbox, cfg.DebugLSP
	t.Cleanup(func() {
		cfg.LSP, cfg.WorkingDir, cfg.Security.Sandbox, cfg.DebugLSP = previousLSP, previousDir, previousSandbox, previousDebug
	})
	root := t.TempDir()
	cfg.WorkingDir, cfg.Security.Sandbox, cfg.DebugLSP = root, "none", false
	command := filepath.Join(root, "gopls")
	cfg.LSP = map[string]config.LSPConfig{"secret-config-name": {Command: command, Args: []string{"secret-argument"}, FileTypes: []string{"go"}, RootMarkers: []string{"go.mod"}}}
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example\n"), 0600))
	var output bytes.Buffer
	originalLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(originalLogger) })
	app := &App{LSPClients: map[string]*lsp.Client{}}
	app.initLSPClients(context.Background())
	t.Cleanup(app.lspStop)
	t.Cleanup(app.lspUnregister)
	file := filepath.Join(root, "main.go")
	for range 3 {
		require.ErrorIs(t, lsp.EnsureClients(context.Background(), app.LSPClients, file), lsp.ErrExecutableUnavailable)
	}
	require.Equal(t, 1, strings.Count(output.String(), "level=WARN"), "missing binaries must not spam the status bar")
	require.Contains(t, output.String(), "gopls")
	require.Contains(t, output.String(), "/integrations")
	require.NotContains(t, output.String(), root)
	require.NotContains(t, output.String(), "secret-")
	entry := cfg.LSP["secret-config-name"]
	entry.Args = []string{"changed-secret-argument"}
	cfg.LSP["secret-config-name"] = entry
	require.ErrorIs(t, lsp.EnsureClients(context.Background(), app.LSPClients, file), lsp.ErrExecutableUnavailable)
	require.Equal(t, 2, strings.Count(output.String(), "level=WARN"), "a changed configuration receives one fresh warning")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, lsp.EnsureClients(ctx, app.LSPClients, file), context.Canceled)
	require.Equal(t, 2, strings.Count(output.String(), "level=WARN"))
	// Availability is checked anew: installing the executable permits a launch
	// without restarting the app. This fixture exits instead of speaking LSP.
	require.NoError(t, os.WriteFile(command, []byte("#!/bin/sh\nexit 1\n"), 0700))
	err := lsp.EnsureClients(context.Background(), app.LSPClients, file)
	require.Error(t, err)
	require.NotErrorIs(t, err, lsp.ErrExecutableUnavailable)
	require.Contains(t, output.String(), "server=gopls")
	require.NotContains(t, output.String(), "secret-")
	require.Empty(t, lsp.SnapshotClients(app.LSPClients))
	app.lspStop()
	require.ErrorIs(t, lsp.EnsureClients(context.Background(), app.LSPClients, file), context.Canceled)
}
