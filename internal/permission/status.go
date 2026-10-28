package permission

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
	for _, id := range s.autoApproveSessions {
		if id == session {
			grants = append(grants, PermissionRequest{SessionID: session, ToolName: "all", Action: "auto_approve", Description: "Sessão explicitamente autoaprovada"})
			break
		}
	}
	return grants
}
