package permission

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/stretchr/testify/require"
)

func testPermissionService(t *testing.T) (*permissionService, <-chan pubsub.Event[PermissionRequest]) {
	t.Helper()
	s := NewPermissionService().(*permissionService)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return s, s.Subscribe(ctx)
}

func testOptions(t *testing.T) CreatePermissionRequest {
	t.Helper()
	return CreatePermissionRequest{SessionID: "session", ToolName: "write", Action: "write", Path: t.TempDir()}
}

func TestRevocationDropsRememberedAndAutomaticGrantsAndDeniesPending(t *testing.T) {
	s, events := testPermissionService(t)
	opts := testOptions(t)
	request, result, _ := askPermission(t, s, events, opts)
	s.GrantPersistant(request)
	require.True(t, permissionResult(t, result))
	s.RevokeSession(opts.SessionID)
	request, result, _ = askPermission(t, s, events, opts)
	s.RevokeSession(opts.SessionID)
	require.False(t, permissionResult(t, result))
	s.GrantPersistant(request)
	s.AutoApproveSession(opts.SessionID)
	s.RevokeSession(opts.SessionID)
	request, result, _ = askPermission(t, s, events, opts)
	s.Deny(request)
	require.False(t, permissionResult(t, result))
}

func askPermission(t *testing.T, s *permissionService, events <-chan pubsub.Event[PermissionRequest], opts CreatePermissionRequest) (PermissionRequest, <-chan bool, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	result := make(chan bool, 1)
	go func() { result <- s.Request(ctx, opts) }()
	select {
	case event := <-events:
		return event.Payload, result, cancel
	case <-ctx.Done():
		t.Fatal("permission request was not published")
		return PermissionRequest{}, result, cancel
	}
}

func permissionResult(t *testing.T, result <-chan bool) bool {
	t.Helper()
	select {
	case allowed := <-result:
		return allowed
	case <-time.After(time.Second):
		t.Fatal("permission request did not finish")
		return false
	}
}

func TestRequestCancellationAndStaleReply(t *testing.T) {
	s, events := testPermissionService(t)
	request, result, cancel := askPermission(t, s, events, testOptions(t))
	cancel()
	require.False(t, permissionResult(t, result))
	s.GrantPersistant(request)
	require.Empty(t, s.pendingRequests)
	require.Empty(t, s.sessionPermissions, "a stale approval must not create a reusable grant")
}

func TestCancelledContextCannotUseAutomaticApproval(t *testing.T) {
	s := NewPermissionService()
	opts := testOptions(t)
	s.AutoApproveSession(opts.SessionID)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.False(t, s.Request(ctx, opts))
}

func TestRequestWithoutApproverIsDenied(t *testing.T) {
	s := NewPermissionService()
	opts := testOptions(t)
	require.False(t, s.Request(context.Background(), opts))
	s.AutoApproveSession(opts.SessionID)
	require.True(t, s.Request(context.Background(), opts))
	opts.SessionID = "another-session"
	require.False(t, s.Request(context.Background(), opts))
}

func TestPermissionRepliesAreIdempotent(t *testing.T) {
	for _, allow := range []bool{false, true} {
		s, events := testPermissionService(t)
		request, result, _ := askPermission(t, s, events, testOptions(t))
		done := make(chan struct{})
		go func() {
			defer close(done)
			if allow {
				s.Grant(request)
			} else {
				s.Deny(request)
			}
			for range 3 {
				s.Grant(request)
				s.Deny(request)
			}
		}()
		require.Equal(t, allow, permissionResult(t, result))
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("duplicate reply blocked")
		}
	}
}

func TestRememberedPermissionPreservesOriginalScope(t *testing.T) {
	s, events := testPermissionService(t)
	opts := testOptions(t)
	request, result, _ := askPermission(t, s, events, opts)
	require.Equal(t, opts.Path, request.Path)
	request.Path = filepath.Dir(opts.Path)
	request.SessionID = "different-session"
	s.GrantPersistant(request)
	require.True(t, permissionResult(t, result))
	require.Equal(t, opts.Path, s.sessionPermissions[0].Path)
	require.Equal(t, opts.SessionID, s.sessionPermissions[0].SessionID)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.True(t, s.Request(ctx, opts))

	for _, change := range []func(*CreatePermissionRequest){
		func(o *CreatePermissionRequest) { o.Path += "-sibling" },
		func(o *CreatePermissionRequest) { o.Path = filepath.Join(o.Path, "child") },
		func(o *CreatePermissionRequest) { o.Path = filepath.Dir(o.Path) },
		func(o *CreatePermissionRequest) { o.SessionID = "different-session" },
		func(o *CreatePermissionRequest) { o.ToolName = "bash" },
		func(o *CreatePermissionRequest) { o.Action = "delete" },
	} {
		other := opts
		change(&other)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		require.False(t, s.Request(ctx, other), "grant leaked to %+v", other)
		cancel()
	}
}

func TestConcurrentRequestsAndAutomaticApprovals(t *testing.T) {
	s, events := testPermissionService(t)
	opts := testOptions(t)
	const count = 32
	var wg sync.WaitGroup
	for range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.AutoApproveSession("unrelated")
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if !s.Request(ctx, opts) {
				t.Error("approved request was denied")
			}
		}()
	}
	for range count {
		select {
		case event := <-events:
			s.Grant(event.Payload)
		case <-time.After(3 * time.Second):
			t.Fatal("missing concurrent permission request")
		}
	}
	wg.Wait()
}
