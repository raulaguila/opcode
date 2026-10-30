package permission

import "slices"

// PendingRequests exposes current requests only. Timed-out or cancelled
// requests disappear even if a client disconnects before seeing their result.
func (s *permissionService) PendingRequests(session string) []PermissionRequest {
	return s.pendingSnapshot(session, false)
}

// PendingRequestsAll lets an interactive client reconcile a saturated event
// subscription. It grants no access; replies still resolve the original scope.
func (s *permissionService) PendingRequestsAll() []PermissionRequest {
	return s.pendingSnapshot("", true)
}

func (s *permissionService) pendingSnapshot(session string, all bool) []PermissionRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := []PermissionRequest{}
	for _, p := range s.pendingRequests {
		if (all || p.request.SessionID == session) && p.ctx.Err() == nil {
			result = append(result, p.request)
		}
	}
	return result
}

// SessionGrants is a snapshot for the application's permission inspector.
// It cannot be used to grant access and excludes pending request parameters.
func (s *permissionService) SessionGrants(session string) []PermissionRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	var grants []PermissionRequest
	for _, p := range s.sessionPermissions {
		if p.SessionID == session {
			p.Params = nil
			grants = append(grants, p)
		}
	}
	if slices.Contains(s.autoApproveSessions, session) {
		grants = append(grants, PermissionRequest{SessionID: session, ToolName: "all", Action: "auto_approve", Description: "Sessão explicitamente autoaprovada"})
	}
	return grants
}
