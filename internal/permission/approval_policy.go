package permission

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools/shell"
)

type ApprovalPolicy string

const (
	// ApprovalPrompt confirms every action: a session nobody chose a level for,
	// such as a headless run.
	ApprovalPrompt ApprovalPolicy = "prompt"
	// ApprovalSandboxed frees project edits and commands inside the OS sandbox.
	ApprovalSandboxed ApprovalPolicy = "sandboxed"
	// ApprovalAuto also frees network for restoring declared dependencies.
	ApprovalAuto ApprovalPolicy = "auto"
	// ApprovalFull removes prompts and the sandbox.
	ApprovalFull ApprovalPolicy = "full"
)

var approvalOrder = []ApprovalPolicy{ApprovalPrompt, ApprovalSandboxed, ApprovalAuto, ApprovalFull}

// ApprovalPolicies controls temporary consent, never persisted with a session.
type ApprovalPolicies interface {
	SetApprovalPolicy(sessionID string, policy ApprovalPolicy, root string) error
	ApprovalPolicy(sessionID string) ApprovalPolicy
}

type projectEditApproval struct {
	root     string // symlink-free path of the project
	logical  string // path as configured, which may traverse symlinks
	identity os.FileInfo
	level    ApprovalPolicy
}

func (s *permissionService) SetApprovalPolicy(sessionID string, policy ApprovalPolicy, root string) error {
	if sessionID == "" {
		return errors.New("approval policy requires a session")
	}
	var approval *projectEditApproval
	switch policy {
	case ApprovalPrompt:
	case ApprovalSandboxed, ApprovalAuto, ApprovalFull:
		logical, err := filepath.Abs(root)
		if err != nil {
			return err
		}
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
		approval = &projectEditApproval{root: resolved, logical: filepath.Clean(logical), identity: info, level: policy}
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
	if approval := s.projectEdits[sessionID]; approval != nil {
		return approval.level
	}
	return ApprovalPrompt
}

// FullAccess says this session's commands run outside the sandbox.
func FullAccess(service Service, sessionID string) bool {
	policies, ok := service.(ApprovalPolicies)
	return ok && policies.ApprovalPolicy(sessionID) == ApprovalFull
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

// RequestAtLevel skips the prompt for an action confined by the OS sandbox when
// the session's level reaches level. Without a sandbox nothing confines it.
func RequestAtLevel(ctx context.Context, service Service, opts CreatePermissionRequest, level ApprovalPolicy) bool {
	if service == nil || ctx.Err() != nil {
		return false
	}
	if policies, ok := service.(ApprovalPolicies); ok && shell.SandboxActive() {
		if slices.Index(approvalOrder, policies.ApprovalPolicy(opts.SessionID)) >= slices.Index(approvalOrder, level) {
			return true
		}
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
		return action == "create" || action == "update" || action == "delete"
	}
	return false
}

func (a *projectEditApproval) contains(target string) bool {
	if !filepath.IsAbs(target) {
		target = filepath.Join(config.WorkingDirectory(), target)
	}
	target = filepath.Clean(target)
	// Tools build targets from the configured (logical) directory; map them
	// onto the resolved root so a symlinked project path still matches.
	relative, ok := projectRelative(a.logical, target)
	if !ok {
		relative, ok = projectRelative(a.root, target)
	}
	if !ok {
		return false
	}
	// Both spellings must still name the approved directory: a symlink in the
	// logical path could have been retargeted since consent was given.
	for _, root := range []string{a.root, a.logical} {
		info, err := os.Stat(root)
		if err != nil || !os.SameFile(info, a.identity) {
			return false
		}
	}
	if shell.SensitivePath(target) {
		return false
	}
	return noSymlinkBelow(a.root, filepath.Join(a.root, relative))
}

func projectRelative(root, target string) (string, bool) {
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", false
	}
	return relative, true
}

// noSymlinkBelow rejects any symlink between root (exclusive) and path, so
// project consent never follows a link out of the project.
func noSymlinkBelow(root, path string) bool {
	for path != root {
		info, err := os.Lstat(path)
		if err != nil && !os.IsNotExist(err) {
			return false
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		parent := filepath.Dir(path)
		if parent == path {
			return false
		}
		path = parent
	}
	return true
}
