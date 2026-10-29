package lsp

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func processTestConfig(t *testing.T) *config.Config {
	t.Helper()
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
	previousDir, previousSandbox, previousDebug := cfg.WorkingDir, cfg.Security.Sandbox, cfg.DebugLSP
	cfg.WorkingDir, cfg.Security.Sandbox, cfg.DebugLSP = t.TempDir(), "none", false
	t.Cleanup(func() {
		cfg.WorkingDir, cfg.Security.Sandbox, cfg.DebugLSP = previousDir, previousSandbox, previousDebug
	})
	return cfg
}

func TestExecutablePreflightUsesWorkspaceAndRedactsUnavailableCommand(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "bin"), 0700))
	command := filepath.Join(root, "bin", "server")
	require.NoError(t, os.WriteFile(command, []byte("#!/bin/sh\nexit 0\n"), 0700))
	require.NoError(t, CheckExecutable("./bin/server", root))
	require.ErrorIs(t, CheckExecutable("./bin/server", t.TempDir()), ErrExecutableUnavailable)
	require.NoError(t, os.Chmod(command, 0600))
	require.ErrorIs(t, CheckExecutable(command, root), ErrExecutableUnavailable)
	for _, command := range []string{"", "secret-token\x1b[2J/server-token", "server-token\nsecret-token"} {
		err := CheckExecutable(command, root)
		require.ErrorIs(t, err, ErrExecutableUnavailable)
		require.Contains(t, err.Error(), "/integrations")
		require.NotContains(t, err.Error(), "secret-token")
		require.NotContains(t, err.Error(), "\x1b")
		require.NotContains(t, err.Error(), "\n")
	}
	t.Setenv("PATH", root)
	err := CheckExecutable("gopls", root)
	require.ErrorIs(t, err, ErrExecutableUnavailable)
	require.Contains(t, err.Error(), "gopls")
	// A missing server must fail before the sandbox shell is even constructed.
	cfg := processTestConfig(t)
	cfg.Security.Sandbox = "bubblewrap"
	_, err = NewClient(context.Background(), "gopls")
	require.ErrorIs(t, err, ErrExecutableUnavailable)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = NewClient(ctx, "gopls")
	require.ErrorIs(t, err, context.Canceled)
}

func TestClientDrainsLongStderrWithoutTerminalOutputAndReapsOnClose(t *testing.T) {
	cfg := processTestConfig(t)
	const secret = "credential-must-not-reach-terminal"
	output := secret + strings.Repeat("x", 256*1024) + "after-long-line\n" + strings.Repeat("burst-line\n", 10000)
	path := filepath.Join(cfg.WorkingDir, "noisy-server")
	script := "#!/bin/sh\nprintf '%s' '" + output + "' >&2\nwhile IFS= read -r line; do :; done\n"
	require.NoError(t, os.WriteFile(path, []byte(script), 0700))
	terminal, err := os.CreateTemp(t.TempDir(), "terminal")
	require.NoError(t, err)
	defer terminal.Close()
	originalStdout, originalStderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = terminal, terminal
	defer func() { os.Stdout, os.Stderr = originalStdout, originalStderr }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, err := NewClient(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = client.Close()
		if t.Failed() {
			t.Logf("stderr status: %+v; expected bytes: %d; process: %v", client.StderrStatus(), len(output), client.Cmd.ProcessState)
		}
	})
	require.Eventually(t, func() bool { return client.StderrStatus().Bytes >= int64(len(output)) }, 3*time.Second, 5*time.Millisecond)
	// A previously closed stdin must not skip Wait or leave the drain alive.
	require.NoError(t, client.stdin.Close())
	var workers sync.WaitGroup
	for range 4 {
		workers.Add(1)
		go func() { defer workers.Done(); require.NoError(t, client.Close()) }()
	}
	workers.Wait()
	require.NotNil(t, client.Cmd.ProcessState)
	require.True(t, client.StderrStatus().Done)
	require.False(t, client.StderrStatus().ReadFailed)
	data, err := os.ReadFile(terminal.Name())
	require.NoError(t, err)
	require.Empty(t, string(data), "LSP output must never bypass the renderer")
}

func TestClientStderrDrainStopsOnCancellationAndUnexpectedReadFailure(t *testing.T) {
	t.Run("canceled process", func(t *testing.T) {
		cfg := processTestConfig(t)
		path := filepath.Join(cfg.WorkingDir, "waiting-server")
		require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\nwhile IFS= read -r line; do :; done\n"), 0700))
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		client, err := NewClient(ctx, path)
		require.NoError(t, err)
		cancel()
		select {
		case <-client.stderrDone:
		case <-time.After(time.Second):
			t.Fatal("stderr drain did not stop after cancellation")
		}
		err = client.Close()
		require.Equal(t, err, client.Close(), "close result is stable")
		require.NotNil(t, client.Cmd.ProcessState)
		require.False(t, client.StderrStatus().ReadFailed)
	})
	t.Run("read failure", func(t *testing.T) {
		client := &Client{stderr: io.NopCloser(failedReader{}), stderrDone: make(chan struct{})}
		client.drainStderr(context.Background())
		require.True(t, client.StderrStatus().Done)
		require.True(t, client.StderrStatus().ReadFailed)
	})
}

type failedReader struct{}

func (failedReader) Read([]byte) (int, error) { return 0, errors.New("secret read failure") }
