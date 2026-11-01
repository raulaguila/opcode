package permission

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func editPolicyFixture(t *testing.T) (*permissionService, string, CreatePermissionRequest) {
	t.Helper()
	root := t.TempDir()
	s := NewPermissionService().(*permissionService)
	require.NoError(t, s.SetApprovalPolicy("session", ApprovalProjectEdits, root))
	opts := CreatePermissionRequest{SessionID: "session", ToolName: "write", Action: "write", Path: root, Params: map[string]string{"diff": "new content"}}
	return s, root, opts
}

func TestProjectEditConsentIsTemporaryAndDoesNotAuthorizeGenericRequests(t *testing.T) {
	s, root, opts := editPolicyFixture(t)
	path := filepath.Join(root, "nested", "new.go")
	require.Equal(t, ApprovalProjectEdits, s.ApprovalPolicy(opts.SessionID))
	require.True(t, RequestProjectEdit(context.Background(), s, opts, path))
	require.False(t, s.Request(context.Background(), opts), "matching metadata is not native edit consent")
	other := opts
	other.SessionID = "other"
	require.False(t, RequestProjectEdit(context.Background(), s, other, path))
	s.RevokeSession(opts.SessionID)
	require.Equal(t, ApprovalPrompt, s.ApprovalPolicy(opts.SessionID))
	require.False(t, RequestProjectEdit(context.Background(), s, opts, path))
	require.Equal(t, ApprovalPrompt, NewPermissionService().(ApprovalPolicies).ApprovalPolicy(opts.SessionID))
}

func TestProjectEditConsentRejectsSensitiveExternalAndSymlinkPaths(t *testing.T) {
	s, root, opts := editPolicyFixture(t)
	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "outside-link")))
	require.NoError(t, os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "dangling-link")))
	require.NoError(t, os.Symlink(root, filepath.Join(root, "inside-link")))
	paths := []string{
		root, root + "-sibling/file.go", filepath.Join(root, "..", "outside.go"),
		filepath.Join(outside, "external.go"), filepath.Join(root, ".env"),
		filepath.Join(root, ".git", "config"), filepath.Join(root, "nested", "secret.key"),
		filepath.Join(root, "outside-link", "new.go"), filepath.Join(root, "inside-link", "new.go"),
		filepath.Join(root, "dangling-link"),
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) { require.False(t, RequestProjectEdit(context.Background(), s, opts, path)) })
	}
}

func TestProjectEditConsentRequiresNativeActionAndValidLiveRequest(t *testing.T) {
	s, root, opts := editPolicyFixture(t)
	path := filepath.Join(root, "new.go")
	for _, pair := range [][2]string{{"patch", "create"}, {"patch", "update"}, {"edit", "write"}, {"write", "write"}} {
		opts.ToolName, opts.Action = pair[0], pair[1]
		require.True(t, RequestProjectEdit(context.Background(), s, opts, path))
	}
	for _, pair := range [][2]string{{"patch", "delete"}, {"patch", "move"}, {"bash", "execute"}, {"mcp_write", "write"}, {"write", "write_external"}, {"network", "connect"}} {
		opts.ToolName, opts.Action = pair[0], pair[1]
		require.False(t, RequestProjectEdit(context.Background(), s, opts, path))
	}
	opts.ToolName, opts.Action = "write", "write"
	opts.Params = make(chan bool)
	require.False(t, RequestProjectEdit(context.Background(), s, opts, path))
	opts.Params = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.False(t, RequestProjectEdit(ctx, s, opts, path))
}

func TestSwitchToPromptRevokesBroadAndRememberedConsent(t *testing.T) {
	s, events := testPermissionService(t)
	opts := testOptions(t)
	request, result, _ := askPermission(t, s, events, opts)
	s.GrantPersistant(request)
	require.True(t, permissionResult(t, result))
	s.AutoApproveSession(opts.SessionID)
	require.NoError(t, s.SetApprovalPolicy(opts.SessionID, ApprovalPrompt, ""))
	require.Empty(t, s.SessionGrants(opts.SessionID))
	request, result, _ = askPermission(t, s, events, opts)
	s.Deny(request)
	require.False(t, permissionResult(t, result))
}

func TestInvalidPolicyPreservesConsentAndReplacedRootRequiresApproval(t *testing.T) {
	s, root, opts := editPolicyFixture(t)
	require.Error(t, s.SetApprovalPolicy(opts.SessionID, "unknown", root))
	require.Error(t, s.SetApprovalPolicy(opts.SessionID, ApprovalProjectEdits, filepath.Join(root, "absent")))
	require.Error(t, s.SetApprovalPolicy("", ApprovalProjectEdits, root))
	require.Equal(t, ApprovalProjectEdits, s.ApprovalPolicy(opts.SessionID))
	original := root + "-original"
	require.NoError(t, os.Rename(root, original))
	t.Cleanup(func() { os.RemoveAll(original) })
	require.NoError(t, os.Mkdir(root, 0o700))
	require.False(t, RequestProjectEdit(context.Background(), s, opts, filepath.Join(root, "new.go")))
}

func TestProjectEditFallbackCanStillBeExplicitlyApproved(t *testing.T) {
	s, events := testPermissionService(t)
	root := t.TempDir()
	require.NoError(t, s.SetApprovalPolicy("session", ApprovalProjectEdits, root))
	opts := CreatePermissionRequest{SessionID: "session", ToolName: "write", Action: "write", Path: root}
	result := make(chan bool, 1)
	go func() { result <- RequestProjectEdit(context.Background(), s, opts, filepath.Join(root, ".env")) }()
	request := <-events
	s.Grant(request.Payload)
	require.True(t, permissionResult(t, result))
	require.Empty(t, s.SessionGrants("session"))
}

func TestChangingPolicyDeniesPendingInsteadOfApprovingIt(t *testing.T) {
	s, events := testPermissionService(t)
	opts := testOptions(t)
	request, result, _ := askPermission(t, s, events, opts)
	require.NoError(t, s.SetApprovalPolicy(opts.SessionID, ApprovalProjectEdits, opts.Path))
	require.False(t, permissionResult(t, result))
	s.GrantPersistant(request)
	require.Empty(t, s.SessionGrants(opts.SessionID), "stale replies must not broaden the selected policy")
	require.True(t, RequestProjectEdit(context.Background(), s, opts, filepath.Join(opts.Path, "new.go")))
}

func TestProjectEditConsentWorksThroughSymlinkedProjectPath(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	require.NoError(t, os.Mkdir(real, 0o700))
	link := filepath.Join(base, "link")
	require.NoError(t, os.Symlink(real, link))
	s := NewPermissionService().(*permissionService)
	require.NoError(t, s.SetApprovalPolicy("session", ApprovalProjectEdits, link))
	opts := CreatePermissionRequest{SessionID: "session", ToolName: "write", Action: "write", Path: link, Params: map[string]string{"diff": "x"}}

	require.True(t, RequestProjectEdit(context.Background(), s, opts, filepath.Join(link, "src", "main.go")))
	require.True(t, RequestProjectEdit(context.Background(), s, opts, filepath.Join(real, "src", "main.go")))

	// Retargeting the project link after consent must not redirect edits.
	other := filepath.Join(base, "other")
	require.NoError(t, os.Mkdir(other, 0o700))
	require.NoError(t, os.Remove(link))
	require.NoError(t, os.Symlink(other, link))
	require.False(t, s.projectEditAllowed(context.Background(), opts, filepath.Join(link, "src", "main.go")))
}
