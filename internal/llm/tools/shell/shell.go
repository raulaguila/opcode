package shell

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/raulaguila/opcode/internal/config"
)

// Buffer bounds memory while retaining both the beginning and end of output.
type Buffer struct {
	mu         sync.Mutex
	head, tail []byte
	total      int
	Limit      int
}

func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	b.total += n
	limit := b.Limit
	if limit <= 0 {
		limit = 30000
	}
	half := limit / 2
	if len(b.head) < half {
		take := min(half-len(b.head), len(p))
		b.head = append(b.head, p[:take]...)
		p = p[take:]
	}
	if len(p) >= half {
		b.tail = append(b.tail[:0], p[len(p)-half:]...)
	} else {
		b.tail = append(b.tail, p...)
		if len(b.tail) > half {
			b.tail = b.tail[len(b.tail)-half:]
		}
	}
	return n, nil
}
func (b *Buffer) Snapshot() (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	truncated := b.total > len(b.head)+len(b.tail)
	text := string(b.head)
	if truncated {
		text += "\n... output truncated ...\n"
	}
	return text + string(b.tail), truncated
}

// Environment intentionally omits credentials, proxy settings, startup hooks
// and user HOME. Only explicit user-global envAllow entries can add variables.
func Environment() []string {
	allowed := []string{"PATH", "LANG", "LC_ALL", "TERM", "TZ", "SYSTEMROOT", "WINDIR", "PATHEXT", "COMSPEC"}
	if cfg := config.Get(); cfg != nil {
		allowed = append(allowed, cfg.Security.EnvAllow...)
	}
	values := map[string]string{"GOFLAGS": "-buildvcs=false", "GIT_TERMINAL_PROMPT": "0", "GIT_EDITOR": "true", "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.DevNull, "HOME": os.DevNull, "TMPDIR": os.TempDir(), "BASH_ENV": os.DevNull, "ENV": os.DevNull, "GOCACHE": filepath.Join(os.TempDir(), "opcode-command-go-build"), "GOMODCACHE": filepath.Join(os.TempDir(), "opcode-command-go-mod")}
	for _, name := range allowed {
		if name != "BASH_ENV" && name != "ENV" && name != "HOME" {
			if value, ok := os.LookupEnv(name); ok {
				values[name] = value
			}
		}
	}
	result := make([]string, 0, len(values))
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	sort.Strings(result)
	return result
}

func Command(ctx context.Context, cwd, command string) (*exec.Cmd, error) {
	return commandProcess(ctx, cwd, command, false, false, false)
}
func GitCommand(ctx context.Context, cwd, command string) (*exec.Cmd, error) {
	return commandProcess(ctx, cwd, command, true, false, false)
}

// WorktreeCommand accepts only typed, bounded worktree mutations. Callers must
// obtain approval for these exact arguments before running the returned command.
// This capability is not available to arbitrary shell commands.
func WorktreeCommand(ctx context.Context, cwd string, args []string) (*exec.Cmd, error) {
	valid := len(args) == 5 && args[0] == "add" && args[1] == "--detach" && args[2] == "--" && args[4] == "HEAD"
	valid = valid || len(args) == 3 && args[0] == "remove" && args[1] == "--"
	if !valid {
		return nil, fmt.Errorf("only managed detached worktree add/remove are supported")
	}
	path := args[len(args)-1]
	if args[0] == "add" {
		path = args[3]
	}
	rel, err := filepath.Rel(filepath.Join(cwd, ".opcode-worktrees"), path)
	if err != nil || rel == "." || filepath.Dir(rel) != "." || strings.HasPrefix(rel, ".") || strings.ContainsAny(rel, "/\\\n\r") {
		return nil, fmt.Errorf("worktree path must be a named direct child of .opcode-worktrees")
	}
	argv := []string{"git", "--no-pager", "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "protocol.file.allow=never", "worktree"}
	argv = append(argv, args...)
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		quoted[i] = "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
	}
	return commandProcess(ctx, cwd, strings.Join(quoted, " "), false, false, true)
}

// PTYCommand retains the same sandbox and environment. The PTY starter creates
// a separate session with its own controlling terminal before executing it.
func PTYCommand(ctx context.Context, cwd, command string) (*exec.Cmd, error) {
	return commandProcess(ctx, cwd, command, false, true, false)
}
func commandProcess(ctx context.Context, cwd, command string, readGit, interactive, writeGit bool) (*exec.Cmd, error) {
	cfg := config.Get()
	path := "/bin/bash"
	args := []string{}
	sandbox := "bubblewrap"
	if cfg != nil {
		if cfg.Shell.Path != "" {
			path = cfg.Shell.Path
		}
		if len(cfg.Shell.Args) > 0 {
			args = append([]string{}, cfg.Shell.Args...)
		}
		sandbox = cfg.Security.Sandbox
	}
	args = append(args, "-c", command)
	var cmd *exec.Cmd
	if sandbox == "none" {
		cmd = exec.CommandContext(ctx, path, args...)
	} else {
		var err error
		cmd, err = sandboxCommand(ctx, cwd, path, args, cfg, readGit, interactive, writeGit)
		if err != nil {
			return nil, err
		}
	}
	cmd.Dir = cwd
	cmd.Env = Environment()
	configureProcess(cmd)
	return cmd, nil
}
func SensitivePath(path string) bool {
	if authStorageContains(path) {
		return true
	}
	if config.LegacySensitivePath(path) {
		return true
	}
	for part := range strings.SplitSeq(filepath.ToSlash(path), "/") {
		lower := strings.ToLower(part)
		if lower == ".git" || lower == ".ssh" || lower == ".aws" || lower == ".gnupg" || lower == ".azure" || lower == ".kube" || lower == ".config" || lower == ".opcode" {
			return true
		}
	}
	base := strings.ToLower(filepath.Base(path))
	if base == ".env" || strings.HasPrefix(base, ".env.") || base == "integration.json" || base == "integration.local.json" || base == ".opcode-integration.json" || base == ".opcode.json" || base == "opcode.json" || base == "credentials" || base == "credentials.json" || base == "application_default_credentials.json" || base == "id_rsa" || base == "id_ed25519" || base == ".npmrc" || base == ".pypirc" || strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key") {
		return true
	}
	if cfg := config.Get(); cfg != nil {
		return privateDataContains(cfg, path) || deniedPathPattern(cfg.Security.DenyPaths, path, base)
	}
	return false
}

type PersistentShell struct{ cwd string }

// Kept as a compatibility entry point; each command gets an independent process.
func GetPersistentShell(cwd string) *PersistentShell { return &PersistentShell{cwd: cwd} }
func (s *PersistentShell) Exec(ctx context.Context, command string, timeoutMs int) (string, string, int, bool, error) {
	if timeoutMs <= 0 {
		timeoutMs = 60000
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	cmd, err := Command(ctx, s.cwd, command)
	if err != nil {
		return "", "", -1, false, err
	}
	out, stderr := &Buffer{}, &Buffer{}
	cmd.Stdout = out
	cmd.Stderr = stderr
	err = cmd.Run()
	code := 0
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	} else if err != nil {
		code = -1
	}
	stdout, _ := out.Snapshot()
	errText, _ := stderr.Snapshot()
	if _, ok := err.(*exec.ExitError); ok {
		err = nil
	}
	return stdout, errText, code, ctx.Err() != nil, err
}
func (s *PersistentShell) Close() {}

func sandboxCommand(ctx context.Context, cwd, path string, args []string, cfg *config.Config, readGit, interactive, writeGit bool) (*exec.Cmd, error) {
	if runtime.GOOS != "linux" {
		return nil, fmt.Errorf("OS sandbox is unavailable; explicitly configure security.sandbox=none in user-global configuration to run without it")
	}
	binary, err := exec.LookPath("bwrap")
	if err != nil {
		return nil, fmt.Errorf("bubblewrap is required by the execution policy")
	}
	wrapped := []string{"--die-with-parent", "--unshare-all"}
	if !interactive {
		wrapped = append(wrapped, "--new-session")
	}
	if cfg != nil && cfg.Security.AllowNetwork {
		wrapped = append(wrapped, "--share-net")
	}
	for _, dir := range []string{"/usr", "/bin", "/lib", "/lib64"} {
		if _, err := os.Stat(dir); err == nil {
			wrapped = append(wrapped, "--ro-bind", dir, dir)
		}
	}
	wrapped = append(wrapped, "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp", "--dir", "/etc")
	for _, entry := range []string{"/etc/ssl/certs", "/etc/resolv.conf", "/etc/hosts", "/etc/nsswitch.conf"} {
		if _, err := os.Stat(entry); err == nil {
			wrapped = append(wrapped, "--ro-bind", entry, entry)
		}
	}
	wrapped, err = appendSandboxReadOnlyPaths(wrapped, cfg)
	if err != nil {
		return nil, err
	}
	workspaceBind := "--bind"
	if readGit && !writeGit {
		workspaceBind = "--ro-bind"
	}
	wrapped = append(wrapped, workspaceBind, cwd, cwd, "--chdir", cwd)
	// Hide credential files inside the otherwise writable workspace, too.
	masks := sandboxCredentialMasks{cwd: cwd, readGit: readGit, writeGit: writeGit}
	if err := filepath.WalkDir(cwd, masks.visit); err != nil {
		return nil, fmt.Errorf("cannot inspect sandbox credential masks: %w", err)
	}
	wrapped = append(wrapped, masks.args...)
	wrapped = append(wrapped, "--", path)
	wrapped = append(wrapped, args...)
	return exec.CommandContext(ctx, binary, wrapped...), nil

}

type sandboxCredentialMasks struct {
	cwd               string
	readGit, writeGit bool
	inspected         int
	args              []string
}

func (m *sandboxCredentialMasks) visit(name string, d os.DirEntry, err error) error {
	if err != nil {
		return err
	}
	m.inspected++
	if m.inspected > 100000 {
		return fmt.Errorf("workspace exceeds sandbox inspection limit; choose a narrower working directory")
	}
	if name == m.cwd {
		return nil
	}
	if strings.EqualFold(d.Name(), ".git") && (m.readGit || m.writeGit) && filepath.Dir(name) == m.cwd {
		return m.bindGit(name, d)
	}
	if !SensitivePath(name) {
		return nil
	}
	if d.IsDir() {
		m.args = append(m.args, "--tmpfs", name)
		return filepath.SkipDir
	}
	m.args = append(m.args, "--ro-bind", os.DevNull, name)
	return nil
}
func (m *sandboxCredentialMasks) bindGit(name string, d os.DirEntry) error {
	bind := "--ro-bind"
	if m.writeGit {
		bind = "--bind"
	}
	m.args = append(m.args, bind, name, name)
	if d.IsDir() {
		return filepath.SkipDir
	}
	return nil
}

func authStorageContains(path string) bool {
	if directory, err := config.AuthDirectory(); err == nil {
		candidate, _ := filepath.Abs(path)
		if relative, err := filepath.Rel(directory, candidate); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative) {
			return true
		}
	}
	return false
}

func privateDataContains(cfg *config.Config, path string) bool {
	if cfg.Data.Directory != "" {
		directory := cfg.Data.Directory
		if !filepath.IsAbs(directory) {
			directory = filepath.Join(config.WorkingDirectory(), directory)
		}
		candidate := path
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(config.WorkingDirectory(), candidate)
		}
		if relative, err := filepath.Rel(filepath.Clean(directory), filepath.Clean(candidate)); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative) {
			return true
		}
	}
	return false
}

func deniedPathPattern(patterns []string, path, base string) bool {
	for _, pattern := range patterns {
		if matched, _ := filepath.Match(pattern, path); matched {
			return true
		}
		if matched, _ := filepath.Match(pattern, base); matched {
			return true
		}
	}
	return false
}

func appendSandboxReadOnlyPaths(wrapped []string, cfg *config.Config) ([]string, error) {
	if cfg != nil {
		for _, entry := range cfg.Security.ReadOnlyPaths {
			if !filepath.IsAbs(entry) {
				return nil, fmt.Errorf("security.readOnlyPaths requires absolute paths")
			}
			wrapped = append(wrapped, "--ro-bind", entry, entry)
		}
	}
	return wrapped, nil
}
