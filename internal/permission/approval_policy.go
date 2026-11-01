package permission

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools/shell"
)

type ApprovalPolicy string

const (
	ApprovalPrompt       ApprovalPolicy = "prompt"
	ApprovalProjectEdits ApprovalPolicy = "project_edits"
)

// ApprovalPolicies controls temporary consent, never persisted with a session.
type ApprovalPolicies interface {
	SetApprovalPolicy(sessionID string, policy ApprovalPolicy, root string) error
	ApprovalPolicy(sessionID string) ApprovalPolicy
}

type projectEditApproval struct {
	root     string
	identity os.FileInfo
}

func (s *permissionService) SetApprovalPolicy(sessionID string, policy ApprovalPolicy, root string) error {
	if sessionID == "" {
		return errors.New("approval policy requires a session")
	}
	var approval *projectEditApproval
	switch policy {
	case ApprovalPrompt:
	case ApprovalProjectEdits:
		resolved, err := filepath.EvalSymlinks(root)
		if err != nil {
			return err
		}
		resolved, err = filepath.Abs(resolved)
		if err != nil {
			return err
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return errors.New("approval policy requires a project directory")
		}
		approval = &projectEditApproval{root: resolved, identity: info}
	default:
		return errors.New("invalid approval policy")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revokeSession(sessionID)
	if approval != nil {
		if s.projectEdits == nil {
			s.projectEdits = make(map[string]*projectEditApproval)
		}
		s.projectEdits[sessionID] = approval
	}
	return nil
}

func (s *permissionService) ApprovalPolicy(sessionID string) ApprovalPolicy {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.projectEdits[sessionID] != nil {
		return ApprovalProjectEdits
	}
	return ApprovalPrompt
}

// RequestProjectEdit is reserved for native file tools; generic requests never
// inherit file-edit consent, even when they claim the same tool name or action.
func RequestProjectEdit(ctx context.Context, service Service, opts CreatePermissionRequest, targetPath string) bool {
	if service == nil || ctx.Err() != nil {
		return false
	}
	if native, ok := service.(interface {
		projectEditAllowed(context.Context, CreatePermissionRequest, string) bool
	}); ok && native.projectEditAllowed(ctx, opts, targetPath) {
		return true
	}
	return service.Request(ctx, opts)
}

func (s *permissionService) projectEditAllowed(ctx context.Context, opts CreatePermissionRequest, target string) bool {
	if !nativeEditAction(opts.ToolName, opts.Action) {
		return false
	}
	s.mu.Lock()
	approval := s.projectEdits[opts.SessionID]
	s.mu.Unlock()
	if approval == nil {
		return false
	}
	if _, err := json.Marshal(opts.Params); err != nil {
		return false
	}
	if !approval.contains(target) {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return ctx.Err() == nil && s.projectEdits[opts.SessionID] == approval
}

func nativeEditAction(tool, action string) bool {
	switch tool {
	case "write", "edit":
		return action == "write"
	case "patch":
		return action == "create" || action == "update"
	}
	return false
}

func (a *projectEditApproval) contains(target string) bool {
	if !filepath.IsAbs(target) {
		target = filepath.Join(config.WorkingDirectory(), target)
	}
	target = filepath.Clean(target)
	relative, err := filepath.Rel(a.root, target)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return false
	}
	info, err := os.Stat(a.root)
	if err != nil || !os.SameFile(info, a.identity) || shell.SensitivePath(target) {
		return false
	}
	return noSymlinkPath(target)
}

func noSymlinkPath(path string) bool {
	for {
		info, err := os.Lstat(path)
		if err != nil && !os.IsNotExist(err) {
			return false
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		parent := filepath.Dir(path)
		if parent == path {
			return true
		}
		path = parent
	}
}
