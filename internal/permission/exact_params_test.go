package permission

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRememberedCommandApprovalCoversExactArgumentsOnly(t *testing.T) {
	s, events := testPermissionService(t)
	opts := testOptions(t)
	opts.ToolName = "bash"
	opts.Action = "execute"
	opts.Params = map[string]string{"command": "go test ./..."}
	request, result, _ := askPermission(t, s, events, opts)
	s.GrantPersistant(request)
	require.True(t, permissionResult(t, result))
	require.True(t, s.Request(context.Background(), opts))
	opts.Params = map[string]string{"command": "rm -rf ./important"}
	request, result, _ = askPermission(t, s, events, opts)
	s.Deny(request)
	require.False(t, permissionResult(t, result))
	opts.Params = make(chan int)
	require.False(t, s.Request(context.Background(), opts))
}
