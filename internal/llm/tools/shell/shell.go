package shell

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
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
	values := map[string]string{"GOFLAGS": "-buildvcs=false -modcacherw", "GIT_TERMINAL_PROMPT": "0", "GIT_EDITOR": "true", "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.DevNull, "HOME": os.DevNull, "TMPDIR": os.TempDir(), "BASH_ENV": os.DevNull, "ENV": os.DevNull, "GOCACHE": filepath.Join(os.TempDir(), "opcode-command-go-build"), "GOMODCACHE": filepath.Join(os.TempDir(), "opcode-command-go-mod")}
	values["GOPATH"] = filepath.Join(os.TempDir(), "opcode-command-go-path")
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

// AlreadySandboxed says this process runs inside a command sandbox.
func AlreadySandboxed() bool {
	_, err := os.Lstat(sandboxMarker)
	return err == nil
}

// SandboxActive says commands here run confined; approval levels that free
// commands rest on it.
func SandboxActive() bool {
	cfg := config.Get()
	return cfg == nil || cfg.Security.Sandbox != "none" || AlreadySandboxed()
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
	// A command inside the sandbox stays confined by it; namespaces rarely nest.
	if on, set := ctx.Value(commandSandboxKey{}).(bool); AlreadySandboxed() || set && !on {
		sandbox = "none"
	}
	cache, err := processCommandCache(ctx, cwd, readGit || writeGit)
	if err != nil {
		return nil, err
	}
	var cmd *exec.Cmd
	if sandbox == "none" {
		cmd = exec.CommandContext(ctx, path, args...)
	} else {
		var err error
		cmd, err = sandboxCommand(ctx, cwd, path, args, cfg, readGit, interactive, writeGit, cache)
		if err != nil {
			return nil, err
		}
	}
	cmd.Dir = cwd
	cmd.Env = Environment()
	if sandbox != "none" {
		cmd.Env = append(slices.DeleteFunc(cmd.Env, func(entry string) bool { return strings.HasPrefix(entry, "HOME=") }), "HOME="+sandboxHome)
	}
	if cache != "" {
		if sandbox != "none" {
			cache = sandboxCachePath
		}
		cmd.Env = commandCacheEnvironment(cmd.Env, cache)
	}
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

func sandboxCommand(ctx context.Context, cwd, path string, args []string, cfg *config.Config, readGit, interactive, writeGit bool, cache string) (*exec.Cmd, error) {
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
	if NetworkEnabled(ctx) {
		wrapped = append(wrapped, "--share-net")
	}
	for _, dir := range []string{"/usr", "/bin", "/lib", "/lib64"} {
		if _, err := os.Stat(dir); err == nil {
			wrapped = append(wrapped, "--ro-bind", dir, dir)
		}
	}
	wrapped = append(wrapped, "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp", "--dir", sandboxHome, "--ro-bind", os.DevNull, sandboxMarker, "--dir", "/etc")
	// Debian's alternatives hold awk, which and others as links under /etc.
	for _, entry := range []string{"/etc/alternatives", "/etc/ssl/certs", "/etc/resolv.conf", "/etc/hosts", "/etc/nsswitch.conf"} {
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
	masks, err := credentialMasks(cwd, readGit, writeGit, cfg)
	if err != nil {
		return nil, fmt.Errorf("cannot inspect sandbox credential masks: %w", err)
	}
	wrapped = append(wrapped, masks...)
	wrapped, err = appendCommandCache(wrapped, cache, cfg)
	if err != nil {
		return nil, err
	}
	wrapped = append(wrapped, "--", path)
	wrapped = append(wrapped, args...)
	return exec.CommandContext(ctx, binary, wrapped...), nil

}

// sandboxHome is empty and vanishes with the command, so tools that need a
// home work without seeing the user's. Creating sandboxMarker on the host
// takes root, so a project cannot fake it to skip the sandbox.
const (
	sandboxHome   = "/tmp/opcode-home"
	sandboxMarker = "/.opcode-sandbox"
)

// maskInspectionLimit bounds the first walk of a workspace; later commands
// reuse its result while no inspected directory changes.
const maskInspectionLimit = 1_000_000

// Credential masks per workspace. A cached walk is reused only while every
// directory it entered keeps its mtime: creating, deleting or renaming an entry
// changes its parent's mtime, so a new credential file forces a fresh walk.
var (
	maskCacheMu sync.Mutex
	maskCache   = map[string]cachedMasks{}
)

type cachedMasks struct {
	key  string
	args []string
	dirs map[string]time.Time
}

func credentialMasks(cwd string, readGit, writeGit bool, cfg *config.Config) ([]string, error) {
	return credentialMasksAt(cwd, cwd, readGit, writeGit, cfg)
}

func credentialMasksAt(cwd, destination string, readGit, writeGit bool, cfg *config.Config) ([]string, error) {
	key := fmt.Sprint(readGit, writeGit) + "\x00" + destination
	if directory, err := config.AuthDirectory(); err == nil {
		key += "\x00" + directory
	}
	if cfg != nil {
		key += fmt.Sprintf("\x00%s\x00%s\x00%q", cfg.WorkingDir, cfg.Data.Directory, cfg.Security.DenyPaths)
	}
	maskCacheMu.Lock()
	cached, ok := maskCache[cwd]
	maskCacheMu.Unlock()
	if ok && cached.key == key && cached.unchanged() {
		return cached.args, nil
	}
	started := time.Now()
	masks := sandboxCredentialMasks{cwd: cwd, destination: destination, readGit: readGit, writeGit: writeGit, dirs: map[string]time.Time{}}
	if err := filepath.WalkDir(cwd, masks.visit); err != nil {
		return nil, err
	}
	// Like racy-git: an mtime this close to the walk may hide a later change
	// within the filesystem's timestamp resolution, so such a walk is not reused.
	stable := true
	for _, modified := range masks.dirs {
		if !modified.Before(started.Add(-2 * time.Second)) {
			stable = false
			break
		}
	}
	maskCacheMu.Lock()
	if stable {
		maskCache[cwd] = cachedMasks{key: key, args: masks.args, dirs: masks.dirs}
	} else {
		delete(maskCache, cwd)
	}
	maskCacheMu.Unlock()
	return masks.args, nil
}

func (c cachedMasks) unchanged() bool {
	for dir, modified := range c.dirs {
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || !info.ModTime().Equal(modified) {
			return false
		}
	}
	return true
}

type sandboxCredentialMasks struct {
	cwd               string
	destination       string
	readGit, writeGit bool
	inspected         int
	args              []string
	dirs              map[string]time.Time
}

// recordDir notes a directory the walk enters, for cache validation.
func (m *sandboxCredentialMasks) recordDir(name string, d os.DirEntry) error {
	if m.dirs == nil || !d.IsDir() {
		return nil
	}
	info, err := d.Info()
	if err != nil {
		return err
	}
	m.dirs[name] = info.ModTime()
	return nil
}

func (m *sandboxCredentialMasks) visit(name string, d os.DirEntry, err error) error {
	if err != nil {
		return err
	}
	m.inspected++
	if m.inspected > maskInspectionLimit {
		return fmt.Errorf("workspace exceeds sandbox inspection limit; choose a narrower working directory")
	}
	if name == m.cwd {
		return m.recordDir(name, d)
	}
	if strings.EqualFold(d.Name(), ".git") && (m.readGit || m.writeGit) && filepath.Dir(name) == m.cwd {
		return m.bindGit(name, d)
	}
	relative, err := filepath.Rel(m.cwd, name)
	if err != nil {
		return err
	}
	// Downloaded modules and build output hold no user secrets, but packages
	// called credentials and test keys: masking them breaks builds.
	if m.destination != m.cwd && (relative == "build" || relative == filepath.Join("path", "pkg", "mod")) {
		return filepath.SkipDir
	}
	target := name
	if m.destination != "" {
		target = filepath.Join(m.destination, relative)
	}
	if !SensitivePath(target) {
		return m.recordDir(name, d)
	}
	if d.IsDir() {
		m.args = append(m.args, "--tmpfs", target)
		return filepath.SkipDir
	}
	m.args = append(m.args, "--ro-bind", os.DevNull, target)
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
