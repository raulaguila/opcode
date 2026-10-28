package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/stretchr/testify/require"
)

func awaitProcess(t *testing.T, manager *ProcessManager, id string, check func(ProcessSnapshot) bool) ProcessSnapshot {
	t.Helper()
	var result ProcessSnapshot
	require.Eventually(t, func() bool {
		var err error
		result, err = manager.Snapshot("test-session", id)
		return err == nil && check(result)
	}, 3*time.Second, 10*time.Millisecond)
	return result
}

func TestPTYInputResizeAndPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTYs require Unix")
	}
	testUnisolated(t)
	manager := NewProcessManager()
	defer manager.Close()
	permissions := &denyingPermissions{}
	params := ProcessParams{Command: `test -t 0 && test -t 1 || exit 9; printf 'READY\n'; IFS= read -r value; stty size; printf 'VALUE:%s\n' "$value"`, PTY: true, Timeout: 5000}
	_, err := manager.Start(toolContext(), permissions, params)
	require.ErrorIs(t, err, permission.ErrorPermissionDenied)
	require.Empty(t, manager.List("test-session"))
	permissions.allow = func(permission.CreatePermissionRequest) bool { return true }
	info, err := manager.Start(toolContext(), permissions, params)
	require.NoError(t, err)
	awaitProcess(t, manager, info.ID, func(s ProcessSnapshot) bool { return s.Running && strings.Contains(s.Stdout, "READY") })
	require.Error(t, manager.Input("other-session", info.ID, "intrusion\n"))
	require.Error(t, manager.Resize("other-session", info.ID, 100, 30))
	require.Error(t, manager.Stop(context.Background(), "other-session", info.ID))
	require.NoError(t, manager.Resize("test-session", info.ID, 100, 30))
	writer := &processTool{name: "process_write", manager: manager, permissions: &denyingPermissions{}}
	_, err = writer.Run(toolContext(), toolInput(map[string]string{"id": info.ID, "data": "denied\n"}))
	require.ErrorIs(t, err, permission.ErrorPermissionDenied)
	require.NoError(t, manager.Input("test-session", info.ID, "resposta\n"))
	result := awaitProcess(t, manager, info.ID, func(s ProcessSnapshot) bool { return !s.Running })
	require.Zero(t, result.ExitCode)
	require.Contains(t, result.Stdout, "VALUE:resposta")
	require.Contains(t, result.Stdout, "30 100")
	require.NotContains(t, result.Stdout, "denied")
	require.Error(t, manager.Input("test-session", info.ID, "after exit\n"))
}

func TestBashOutputIsAvailableBeforeCommandCompletes(t *testing.T) {
	testUnisolated(t)
	manager := NewProcessManager()
	defer manager.Close()
	permissions := &denyingPermissions{allow: func(permission.CreatePermissionRequest) bool { return true }}
	done := make(chan ToolResponse, 1)
	errCh := make(chan error, 1)
	go func() {
		response, err := NewBashTool(permissions, manager).Run(toolContext(), toolInput(BashParams{Command: "printf FIRST; sleep 0.4; printf SECOND"}))
		errCh <- err
		done <- response
	}()
	var id string
	require.Eventually(t, func() bool {
		list := manager.List("test-session")
		if len(list) == 0 {
			return false
		}
		id = list[0].ID
		out, err := manager.Snapshot("test-session", id)
		return err == nil && out.Running && strings.Contains(out.Stdout, "FIRST") && !strings.Contains(out.Stdout, "SECOND")
	}, time.Second, 5*time.Millisecond)
	response := <-done
	require.NoError(t, <-errCh)
	require.Contains(t, response.Content, "FIRSTSECOND")
	var metadata BashResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(response.Metadata), &metadata))
	require.Equal(t, id, metadata.ProcessID)
}

func TestPTYCancellationAndShutdownReleaseReaders(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTYs require Unix")
	}
	testUnisolated(t)
	manager := NewProcessManager()
	defer manager.Close()
	parent, cancel := context.WithCancel(toolContext())
	permissions := &denyingPermissions{allow: func(permission.CreatePermissionRequest) bool { return true }}
	info, err := manager.Start(parent, permissions, ProcessParams{Command: "printf READY; sleep 60", PTY: true})
	require.NoError(t, err)
	cancel() // Detached processes survive the provider turn.
	awaitProcess(t, manager, info.ID, func(s ProcessSnapshot) bool { return s.Running && strings.Contains(s.Stdout, "READY") })
	manager.CancelSession("test-session")
	result := awaitProcess(t, manager, info.ID, func(s ProcessSnapshot) bool { return !s.Running })
	require.NotZero(t, result.ExitCode)
	require.NoError(t, manager.Close())
}

func TestLiveOutputRedactsSplitAndWrappedCredentials(t *testing.T) {
	secret := "unit-private-key-12345678"
	t.Setenv("LIVE_TEST_API_KEY", secret)
	require.NotContains(t, redactLive("prefix "+secret[:12], true), secret[:12])
	require.NotContains(t, redactLive("prefix "+secret, true), secret)
	wrapped := "unit-private-\nkey-12345678    \n             \n"
	masked := redactScreen(wrapped, true)
	require.NotContains(t, masked, "unit-private-")
	require.NotContains(t, masked, "key-12345678")
	require.NotContains(t, cleanProcessOutput("safe\x1b]52;c;clipboard-data\x07\x1b[2Jend"), "\x1b")
	require.NotContains(t, cleanProcessOutput("safe\x1b]52;c;clipboard-data\x07end"), "clipboard-data")
}

func TestPTYRetainsBubblewrapIsolation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Bubblewrap requires Linux")
	}
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("Bubblewrap unavailable")
	}
	cfg := config.Get()
	original, wd := cfg.Security, cfg.WorkingDir
	t.Cleanup(func() { cfg.Security = original; cfg.WorkingDir = wd })
	cfg.Security = config.SecurityConfig{Sandbox: "bubblewrap"}
	cfg.WorkingDir = t.TempDir()
	t.Setenv("OPENAI_API_KEY", "synthetic-private-test-key")
	require.NoError(t, os.WriteFile(filepath.Join(cfg.WorkingDir, ".env"), []byte("workspace-secret"), 0600))
	outside := filepath.Join(t.TempDir(), "outside")
	require.NoError(t, os.WriteFile(outside, []byte("outside-secret"), 0600))
	manager := NewProcessManager()
	defer manager.Close()
	permissions := &denyingPermissions{allow: func(permission.CreatePermissionRequest) bool { return true }}
	command := fmt.Sprintf(`test -t 0 && test -t 1 || exit 2; test "${OPENAI_API_KEY-unset}" = unset || exit 3; test ! -s .env || exit 4; test ! -e %q || exit 5; printf 'ISOLATED\n'; read -r answer; printf 'ANSWER:%%s\n' "$answer"`, outside)
	info, err := manager.Start(toolContext(), permissions, ProcessParams{Command: command, PTY: true, Timeout: 5000})
	require.NoError(t, err)
	result := awaitProcess(t, manager, info.ID, func(s ProcessSnapshot) bool { return !s.Running || strings.Contains(s.Stdout, "ISOLATED") })
	if !result.Running && (strings.Contains(result.Stdout, "Operation not permitted") || strings.Contains(result.Stdout, "No permissions to create a new namespace")) {
		t.Skip("execution environment blocks user namespaces")
	}
	require.True(t, result.Running, "sandbox failed: %s", result.Stdout)
	require.NoError(t, manager.Input("test-session", info.ID, "approved\n"))
	result = awaitProcess(t, manager, info.ID, func(s ProcessSnapshot) bool { return !s.Running })
	require.Zero(t, result.ExitCode)
	require.Contains(t, result.Stdout, "ANSWER:approved")
	require.NotContains(t, result.Stdout, "workspace-secret")
	require.NotContains(t, result.Stdout, "outside-secret")
}
