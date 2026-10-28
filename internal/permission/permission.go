package permission

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/pubsub"
)

var ErrorPermissionDenied = errors.New("permission denied")

type CreatePermissionRequest struct {
	SessionID   string `json:"session_id"`
	ToolName    string `json:"tool_name"`
	Description string `json:"description"`
	Action      string `json:"action"`
	Params      any    `json:"params"`
	Path        string `json:"path"`
}

type PermissionRequest struct {
	ID            string `json:"id"`
	SessionID     string `json:"session_id"`
	ToolName      string `json:"tool_name"`
	Description   string `json:"description"`
	Action        string `json:"action"`
	Params        any    `json:"params"`
	Path          string `json:"path"`
	parameterHash [32]byte
}

type Service interface {
	pubsub.Suscriber[PermissionRequest]
	GrantPersistant(permission PermissionRequest)
	Grant(permission PermissionRequest)
	Deny(permission PermissionRequest)
	Request(ctx context.Context, opts CreatePermissionRequest) bool
	AutoApproveSession(sessionID string)
	RevokeSession(sessionID string)
}

type permissionService struct {
	*pubsub.Broker[PermissionRequest]

	mu                  sync.Mutex
	sessionPermissions  []PermissionRequest
	pendingRequests     map[string]pendingRequest
	autoApproveSessions []string
}

type pendingRequest struct {
	request  PermissionRequest
	ctx      context.Context
	response chan bool
}

func (s *permissionService) GrantPersistant(permission PermissionRequest) {
	s.respond(permission.ID, true, true)
}

func (s *permissionService) Grant(permission PermissionRequest) {
	s.respond(permission.ID, true, false)
}

func (s *permissionService) Deny(permission PermissionRequest) {
	s.respond(permission.ID, false, false)
}

func (s *permissionService) respond(id string, allow, remember bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending, ok := s.pendingRequests[id]
	if !ok {
		return
	}
	delete(s.pendingRequests, id)
	if pending.ctx.Err() != nil {
		return
	}
	if remember {
		// Remember the original scope, not fields supplied by a modified UI reply.
		s.sessionPermissions = append(s.sessionPermissions, pending.request)
	}
	pending.response <- allow
}

func (s *permissionService) Request(ctx context.Context, opts CreatePermissionRequest) bool {
	if ctx.Err() != nil {
		return false
	}

	// Callers supply a directory scope; taking Dir again would grant its parent.
	dir := opts.Path
	if !strings.HasPrefix(dir, "network:") && !filepath.IsAbs(dir) {
		dir = filepath.Join(config.WorkingDirectory(), dir)
	}
	if !strings.HasPrefix(dir, "network:") {
		dir = filepath.Clean(dir)
	}
	permission := PermissionRequest{
		ID:          uuid.New().String(),
		Path:        dir,
		SessionID:   opts.SessionID,
		ToolName:    opts.ToolName,
		Description: opts.Description,
		Action:      opts.Action,
		Params:      opts.Params,
	}
	encodedParams, err := json.Marshal(opts.Params)
	if err != nil {
		return false
	}
	permission.parameterHash = sha256.Sum256(encodedParams)

	s.mu.Lock()
	if slices.Contains(s.autoApproveSessions, opts.SessionID) {
		s.mu.Unlock()
		return ctx.Err() == nil
	}
	for _, p := range s.sessionPermissions {
		if p.ToolName == permission.ToolName && p.Action == permission.Action && p.SessionID == permission.SessionID && p.Path == permission.Path && p.parameterHash == permission.parameterHash {
			s.mu.Unlock()
			return ctx.Err() == nil
		}
	}
	// Without a UI there is nobody to approve a request. Fail closed unless
	// this session was explicitly approved above.
	if s.GetSubscriberCount() == 0 {
		s.mu.Unlock()
		return false
	}

	respCh := make(chan bool, 1)
	s.pendingRequests[permission.ID] = pendingRequest{permission, ctx, respCh}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pendingRequests, permission.ID)
		s.mu.Unlock()
	}()

	s.Publish(pubsub.CreatedEvent, permission)
	select {
	case <-ctx.Done():
		return false
	case resp := <-respCh:
		return resp && ctx.Err() == nil
	}
}

func (s *permissionService) AutoApproveSession(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.autoApproveSessions = append(s.autoApproveSessions, sessionID)
}

func (s *permissionService) RevokeSession(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionPermissions = slices.DeleteFunc(s.sessionPermissions, func(p PermissionRequest) bool { return p.SessionID == sessionID })
	s.autoApproveSessions = slices.DeleteFunc(s.autoApproveSessions, func(id string) bool { return id == sessionID })
	for id, pending := range s.pendingRequests {
		if pending.request.SessionID == sessionID {
			delete(s.pendingRequests, id)
			select {
			case pending.response <- false:
			default:
			}
		}
	}
}

func NewPermissionService() Service {
	return &permissionService{
		Broker:             pubsub.NewBroker[PermissionRequest](),
		sessionPermissions: make([]PermissionRequest, 0),
		pendingRequests:    make(map[string]pendingRequest),
	}
}
