package desktop

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestResendingThePolicyKeepsWaitingApprovals(t *testing.T) {
	root := t.TempDir()
	settings := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(settings, []byte("{}"), 0o600))
	oldPath := viper.ConfigFileUsed()
	viper.SetConfigFile(settings)
	t.Cleanup(func() { viper.SetConfigFile(oldPath) })
	_, err := config.Load(root, false)
	require.NoError(t, err)
	cfg := config.Get()
	previous := cfg.WorkingDir
	cfg.WorkingDir = root
	t.Cleanup(func() { cfg.WorkingDir = previous })
	policies := permission.NewPermissionService()
	s := &Server{app: &app.App{Permissions: policies}}
	policies.Subscribe(t.Context()) // with nobody listening a request fails closed
	require.NoError(t, s.configure(context.Background(), "s", "", string(permission.ApprovalSandboxed)))
	result := make(chan bool, 1)
	go func() {
		result <- policies.Request(context.Background(), permission.CreatePermissionRequest{SessionID: "s", ToolName: "bash", Action: "execute", Path: t.TempDir()})
	}()
	require.Eventually(t, func() bool {
		return len(policies.(interface {
			PendingRequests(string) []permission.PermissionRequest
		}).PendingRequests("s")) == 1
	}, 2e9, 1e7)
	require.NoError(t, s.configure(context.Background(), "s", "", string(permission.ApprovalSandboxed)))
	require.Empty(t, result, "the same policy again must not deny what waits")
	require.NoError(t, s.configure(context.Background(), "s", "", string(permission.ApprovalPrompt)))
	require.False(t, <-result)
}
