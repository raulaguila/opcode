package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools/shell"
	"github.com/raulaguila/opcode/internal/permission"
)

// NewProductivityTools shares process lifetime with the application when supplied.
func NewProductivityTools(permissions permission.Service, managers ...*ProcessManager) []BaseTool {
	return []BaseTool{NewCodeIndexTool(), &formatProjectTool{bash: NewBashTool(permissions, managers...)}, &worktreeTool{permissions: permissions}}
}

type formatProjectTool struct{ bash BaseTool }

func (*formatProjectTool) Info() ToolInfo {
	return ToolInfo{Name: "format_project", Description: "Run an installed formatter on explicit files with normal command approval, sandbox, live output and timeout. Presets: go (gofmt), python (ruff format), javascript/typescript/json (prettier). No installation, network invocation, recursive project glob or package-manager download. Inspect resulting diffs and run configured checks separately; formatting does not certify tests.", Parameters: map[string]any{"preset": map[string]any{"type": "string", "enum": []string{"go", "python", "javascript", "typescript", "json"}}, "files": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1, "maxItems": 64}}, Required: []string{"preset", "files"}}
}
func (f *formatProjectTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var input struct {
		Preset string
		Files  []string
	}
	if json.Unmarshal([]byte(call.Input), &input) != nil || len(input.Files) < 1 || len(input.Files) > 64 {
		return NewTextErrorResponse("provide a preset and 1–64 explicit files"), nil
	}
	var args []string
	switch input.Preset {
	case "go":
		args = []string{"gofmt", "-w"}
	case "python":
		args = []string{"ruff", "format", "--"}
	case "javascript", "typescript", "json":
		args = []string{"prettier", "--write", "--"}
	default:
		return NewTextErrorResponse("unsupported formatting preset"), nil
	}
	if _, err := exec.LookPath(args[0]); err != nil {
		return NewTextErrorResponse("formatter is not installed; configure or install it explicitly outside the agent"), nil
	}
	for _, file := range input.Files {
		path, err := authorizeFile(ctx, file, "write")
		if err != nil {
			return ToolResponse{}, err
		}
		if !inside(config.WorkingDirectory(), path) || shell.SensitivePath(path) || path != absolutePath(file) {
			return NewTextErrorResponse("formatting paths must be regular workspace files without symlinks or secret paths"), nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return ToolResponse{}, err
		}
		if !info.Mode().IsRegular() || info.Size() > 2<<20 {
			return NewTextErrorResponse("formatting requires regular files up to 2 MiB"), nil
		}
		rel, _ := filepath.Rel(config.WorkingDirectory(), path)
		args = append(args, "./"+filepath.ToSlash(rel))
	}
	raw, _ := json.Marshal(BashParams{Command: commandString(args), Timeout: 120000})
	return f.bash.Run(ctx, ToolCall{ID: call.ID, Name: BashToolName, Input: string(raw)})
}
func (f *formatProjectTool) Close() error {
	if c, ok := f.bash.(interface{ Close() error }); ok {
		return c.Close()
	}
	return nil
}
func (f *formatProjectTool) CancelSession(id string) {
	if c, ok := f.bash.(interface{ CancelSession(string) }); ok {
		c.CancelSession(id)
	}
}

type worktreeTool struct{ permissions permission.Service }

func (*worktreeTool) Info() ToolInfo {
	return ToolInfo{Name: "manage_worktrees", Description: "List Git worktrees or create/remove a named detached worktree inside .opcode-worktrees. Creation starts from HEAD, never force/resets or changes the main branch. Removal refuses main/current, dirty, unregistered or external paths. Each mutation needs exact command approval and the normal OS sandbox; a linked worktree whose common Git directory is outside the sandbox may be unavailable.", Parameters: map[string]any{"action": map[string]any{"type": "string", "enum": []string{"list", "create", "remove"}}, "name": map[string]any{"type": "string", "pattern": "^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$"}}, Required: []string{"action"}}
}

var worktreeName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

func (w *worktreeTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var input struct{ Action, Name string }
	if json.Unmarshal([]byte(call.Input), &input) != nil {
		return NewTextErrorResponse("invalid worktree parameters"), nil
	}
	listing, truncated, err := gitOutput(ctx, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return ToolResponse{}, err
	}
	if truncated {
		return NewTextErrorResponse("worktree listing exceeds output limit"), nil
	}
	if input.Action == "list" {
		entries := []map[string]any{}
		for _, field := range strings.Split(listing, "\x00") {
			if strings.HasPrefix(field, "worktree ") {
				if len(entries) == 256 {
					return jsonToolResult(map[string]any{"worktrees": entries, "truncated": true}), nil
				}
				entries = append(entries, map[string]any{"path": strings.TrimPrefix(field, "worktree ")})
			} else if len(entries) > 0 {
				key, value, found := strings.Cut(field, " ")
				if key == "HEAD" || key == "branch" {
					entries[len(entries)-1][key] = value
				} else if key == "detached" || key == "locked" || key == "prunable" {
					if found {
						entries[len(entries)-1][key] = value
					} else {
						entries[len(entries)-1][key] = true
					}
				}
			}
		}
		return jsonToolResult(map[string]any{"worktrees": entries, "truncated": false}), nil
	}
	if input.Action != "create" && input.Action != "remove" || !worktreeName.MatchString(input.Name) {
		return NewTextErrorResponse("choose create/remove and a 1–64 character worktree name (letters, digits, underscores, hyphens)"), nil
	}
	root, err := canonicalPath(config.WorkingDirectory())
	if err != nil {
		return ToolResponse{}, err
	}
	path := filepath.Join(root, ".opcode-worktrees", input.Name)
	resolved, err := canonicalPath(path)
	if err != nil {
		return ToolResponse{}, err
	}
	if resolved != path {
		return NewTextErrorResponse("managed worktree paths must not contain symlinks"), nil
	}
	args := []string{"add", "--detach", "--", path, "HEAD"}
	if input.Action == "create" {
		// Checkout filters can execute arbitrary configured programs. Managed
		// worktrees deliberately refuse those repositories instead of silently
		// executing a filter that is absent from the approved argv.
		if err := checkWorktreeFilters(ctx); err != nil {
			return NewTextErrorResponse(err.Error()), nil
		}
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			return NewTextErrorResponse("worktree destination already exists or cannot be inspected"), nil
		}
	} else {
		registered := false
		first := ""
		for _, field := range strings.Split(listing, "\x00") {
			if strings.HasPrefix(field, "worktree ") {
				entry := strings.TrimPrefix(field, "worktree ")
				if first == "" {
					first = entry
				}
				if entry == path {
					registered = true
				}
			}
		}
		if !registered || path == root || path == first {
			return NewTextErrorResponse("only registered managed worktrees other than main/current may be removed"), nil
		}
		// Git remove itself performs the final dirty/locked/submodule preflight;
		// no --force is available, including after approval or concurrent edits.
		args = []string{"remove", "--", path}
	}
	session, _ := GetContextValues(ctx)
	if session == "" || w.permissions == nil || !w.permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: session, ToolName: "manage_worktrees", Path: path, Action: "execute", Description: "Git worktree " + input.Action + " (detached; no force/reset)", Params: map[string]any{"git_worktree_args": args}}) {
		return ToolResponse{}, permission.ErrorPermissionDenied
	}
	if err := ctx.Err(); err != nil {
		return ToolResponse{}, err
	}
	if current, err := canonicalPath(path); err != nil || current != path {
		return NewTextErrorResponse("worktree path changed while awaiting approval"), nil
	}
	if input.Action == "create" {
		if err := checkWorktreeFilters(ctx); err != nil {
			return NewTextErrorResponse(err.Error()), nil
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command, err := shell.WorktreeCommand(ctx, root, args)
	if err != nil {
		return ToolResponse{}, err
	}
	out, stderr := &shell.Buffer{Limit: 16000}, &shell.Buffer{Limit: 4096}
	command.Stdout = out
	command.Stderr = stderr
	err = command.Run()
	text, cut := out.Snapshot()
	errorText, errorCut := stderr.Snapshot()
	if ctx.Err() != nil {
		return ToolResponse{}, ctx.Err()
	}
	response := jsonToolResult(map[string]any{"action": input.Action, "path": path, "output": text, "stderr": errorText, "truncated": cut || errorCut, "success": err == nil})
	response.IsError = err != nil
	if err != nil && text == "" && errorText == "" {
		response.Content = fmt.Sprintf("worktree %s failed under the execution policy", input.Action)
	}
	return response, nil
}

func checkWorktreeFilters(ctx context.Context) error {
	filters, truncated, err := gitOutput(ctx, "config", "--name-only", "--list")
	if err != nil {
		return err
	}
	if truncated {
		return fmt.Errorf("repository configuration exceeds safe inspection limit")
	}
	for _, key := range strings.Split(filters, "\n") {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(key)), "filter.") {
			return fmt.Errorf("repository checkout filters require manual worktree creation; no filter was executed")
		}
	}
	return nil
}

// NewWorktreeTool exposes the same protected implementation to user-driven UI.
func NewWorktreeTool(permissions permission.Service) BaseTool {
	return &worktreeTool{permissions: permissions}
}

type Worktree struct {
	Path     string `json:"path"`
	Branch   string `json:"branch"`
	HEAD     string `json:"HEAD"`
	Detached bool   `json:"detached"`
}

func ListWorktrees(ctx context.Context) ([]Worktree, error) {
	response, err := (&worktreeTool{}).Run(ctx, ToolCall{Input: `{"action":"list"}`})
	if err != nil {
		return nil, err
	}
	if response.IsError {
		return nil, fmt.Errorf("worktree listing failed")
	}
	var value struct {
		Worktrees []Worktree `json:"worktrees"`
		Truncated bool       `json:"truncated"`
	}
	if json.Unmarshal([]byte(response.Content), &value) != nil || value.Truncated {
		return nil, fmt.Errorf("invalid or truncated worktree listing")
	}
	return value.Worktrees, nil
}
