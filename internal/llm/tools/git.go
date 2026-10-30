package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools/shell"
)

type gitTool struct{ name string }

func NewGitTools() []BaseTool {
	return []BaseTool{&gitTool{"git_status"}, &gitTool{"git_diff"}, &gitTool{"git_log"}}
}
func (g *gitTool) Info() ToolInfo {
	return ToolInfo{Name: g.name, Description: "Read-only Git inspection. Never stages, commits, resets, cleans or modifies user changes. Diff excludes credential paths. Results are bounded.", Parameters: map[string]any{"path": map[string]any{"type": "string", "description": "Optional workspace-relative path filter"}, "staged": map[string]any{"type": "boolean"}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}}}
}
func quoteArg(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
func commandString(args []string) string {
	parts := make([]string, len(args))
	for i, arg := range args {
		parts[i] = quoteArg(arg)
	}
	return strings.Join(parts, " ")
}
func gitOutput(ctx context.Context, args ...string) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	base := []string{"git", "--no-optional-locks", "--no-pager", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "core.pager=cat", "-c", "log.showSignature=false", "-c", "core.worktree=" + config.WorkingDirectory()}
	// Reading config names cannot run a filter. Never return configured command
	// values, and disable drivers before even status/name-only inspects a file.
	names, truncated, err := runGitInspection(ctx, append(append([]string{}, base...), "config", "--null", "--name-only", "--get-regexp", `^filter\..*\.(clean|process|required)$`))
	if err != nil && !gitConfigNoMatches(err) || truncated {
		return "", false, fmt.Errorf("cannot safely inspect Git filter configuration")
	}
	overrides, err := disabledGitFilters(names)
	if err != nil {
		return "", false, err
	}
	base = append(base, overrides...)
	out, truncated, err := runGitInspection(ctx, append(base, args...))
	if err != nil {
		return "", false, fmt.Errorf("git inspection failed (repository unavailable, timeout, sandbox or Git error)")
	}
	return out, truncated, nil
}

func gitConfigNoMatches(err error) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == 1
}

func disabledGitFilters(names string) ([]string, error) {
	seen := map[string]bool{}
	var overrides []string
	for key := range strings.SplitSeq(names, "\x00") {
		if key == "" {
			continue
		}
		last := strings.LastIndexByte(key, '.')
		if !strings.HasPrefix(key, "filter.") || last <= len("filter.") || len(key) > 1024 || strings.ContainsAny(key, "=\r\n") {
			return nil, fmt.Errorf("invalid Git filter configuration")
		}
		prefix := key[:last]
		if seen[prefix] {
			continue
		}
		seen[prefix] = true
		if len(seen) > 256 {
			return nil, fmt.Errorf("too many Git filters for safe inspection")
		}
		overrides = append(overrides, "-c", prefix+".clean=", "-c", prefix+".process=", "-c", prefix+".required=false")
	}
	return overrides, nil
}

func runGitInspection(ctx context.Context, args []string) (string, bool, error) {
	cmd, err := shell.GitCommand(ctx, config.WorkingDirectory(), commandString(args))
	if err != nil {
		return "", false, err
	}
	out, stderr := &shell.Buffer{Limit: 128 << 10}, &shell.Buffer{Limit: 4096}
	cmd.Stdout, cmd.Stderr = out, stderr
	err = cmd.Run()
	text, truncated := out.Snapshot()
	return text, truncated, err
}

func (g *gitTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var input struct {
		Path   string
		Staged bool
		Limit  int
	}
	if json.Unmarshal([]byte(call.Input), &input) != nil {
		return NewTextErrorResponse("invalid Git parameters"), nil
	}
	path := "."
	if input.Path != "" {
		abs := absolutePath(input.Path)
		if !inside(config.WorkingDirectory(), abs) || shell.SensitivePath(abs) {
			return NewTextErrorResponse("Git path must stay within the workspace and exclude credential files"), nil
		}
		path, _ = filepath.Rel(config.WorkingDirectory(), abs)
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 20
	}
	limit = min(limit, 100)
	switch g.name {
	case "git_status":
		return inspectGitStatus(ctx, path, limit)
	case "git_diff":
		return inspectGitDiff(ctx, input.Staged, path, limit)
	case "git_log":
		return inspectGitLog(ctx, path, limit)
	}
	return NewTextErrorResponse("unsupported Git operation"), nil
}
func jsonToolResult(value any) ToolResponse {
	raw, err := json.Marshal(value)
	if err != nil {
		return NewTextErrorResponse("cannot encode tool result")
	}
	return NewTextResponse(string(raw))
}

func inspectGitDiff(ctx context.Context, staged bool, path string, limit int) (ToolResponse, error) {
	args := []string{"diff", "--no-ext-diff", "--no-textconv"}
	if staged {
		args = append(args, "--cached")
	}
	names, cut, err := gitOutput(ctx, append(append([]string{}, args...), "--name-only", "-z", "--", path)...)
	if err != nil {
		return ToolResponse{}, err
	}
	if cut {
		return NewTextErrorResponse("too many changed paths; choose a narrower path"), nil
	}
	paths := []string{}
	for name := range strings.SplitSeq(names, "\x00") {
		if name != "" && !shell.SensitivePath(absolutePath(name)) {
			if _, err := authorizeFile(ctx, name, "read"); err != nil {
				continue
			}
			paths = append(paths, name)
		}
	}
	if len(paths) == 0 {
		return jsonToolResult(map[string]any{"diff": "", "truncated": false}), nil
	}
	truncated := len(paths) > limit
	if truncated {
		paths = paths[:limit]
	}
	args = append(args, "--unified=3", "--")
	args = append(args, paths...)
	raw, cut, err := gitOutput(ctx, args...)
	if err != nil {
		return ToolResponse{}, err
	}
	return jsonToolResult(map[string]any{"diff": raw, "truncated": truncated || cut}), nil
}

func inspectGitStatus(ctx context.Context, path string, limit int) (ToolResponse, error) {
	raw, truncated, err := gitOutput(ctx, "status", "--porcelain=v1", "-z", "--untracked-files=normal", "--", path)
	if err != nil {
		return ToolResponse{}, err
	}
	if truncated {
		return NewTextErrorResponse("Git status exceeds output limit; select a narrower path"), nil
	}
	type status struct {
		Status   string `json:"status"`
		Path     string `json:"path"`
		Original string `json:"original_path,omitempty"`
	}
	entries := []status{}
	fields := strings.Split(raw, "\x00")
	for i := 0; i < len(fields); i++ {
		field := fields[i]
		if len(field) < 3 {
			continue
		}
		entry := status{Status: field[:2], Path: field[3:]}
		if strings.ContainsAny(entry.Status, "RC") && i+1 < len(fields) {
			i++
			entry.Original = fields[i]
		}
		if len(entries) == limit {
			truncated = true
			break
		}
		entries = append(entries, entry)
	}
	return jsonToolResult(map[string]any{"entries": entries, "truncated": truncated}), nil
}

func inspectGitLog(ctx context.Context, path string, limit int) (ToolResponse, error) {
	raw, cut, err := gitOutput(ctx, "log", "-n", strconv.Itoa(limit), "--format=%h%x00%ct%x00%s%x00", "--", path)
	if err != nil {
		return ToolResponse{}, err
	}
	if cut {
		return NewTextErrorResponse("Git log exceeds output limit; reduce limit"), nil
	}
	entries := []map[string]string{}
	fields := strings.Split(raw, "\x00")
	for i := 0; i+2 < len(fields); i += 3 {
		entries = append(entries, map[string]string{"commit": strings.TrimSpace(fields[i]), "timestamp": fields[i+1], "subject": fields[i+2]})
	}
	return jsonToolResult(map[string]any{"entries": entries, "truncated": cut}), nil
}
