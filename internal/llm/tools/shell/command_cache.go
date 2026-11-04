package shell

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/localstate"
)

const sandboxCachePath = "/tmp/opcode-command-cache"

// commandCacheLimit bounds what one project's Go build cache may keep on disk.
const commandCacheLimit = 4 << 30

func commandCacheRoot(cwd string) string { return filepath.Join(cwd, ".opcode", "command-cache") }

// CommandCacheSize is the bytes the project's command cache holds, 0 when absent.
func CommandCacheSize(cwd string) int64 {
	var total int64
	_ = filepath.WalkDir(commandCacheRoot(cwd), func(_ string, entry fs.DirEntry, err error) error {
		if err == nil && entry.Type().IsRegular() {
			if info, err := entry.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

// TrimCommandCache empties the cache above the limit; the next command rebuilds
// what it needs. Go keeps module folders read-only, which blocks a plain removal.
func TrimCommandCache(cwd string) (bool, error) {
	if CommandCacheSize(cwd) <= commandCacheLimit {
		return false, nil
	}
	return true, removeCommandCache(cwd)
}

func removeCommandCache(cwd string) error {
	root := commandCacheRoot(cwd)
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry.IsDir() {
			_ = os.Chmod(path, 0o700)
		}
		return nil
	})
	return os.RemoveAll(root)
}

func appendCommandCache(args []string, directory string, cfg *config.Config) ([]string, error) {
	if directory == "" {
		return args, nil
	}
	masks, err := credentialMasksAt(directory, sandboxCachePath, false, false, cfg)
	if err != nil {
		return nil, fmt.Errorf("cannot inspect command cache credential masks: %w", err)
	}
	args = append(args, "--bind", directory, sandboxCachePath)
	return append(args, masks...), nil
}

func processCommandCache(ctx context.Context, cwd string, git bool) (string, error) {
	enabled, _ := ctx.Value(commandCacheKey{}).(bool)
	if !enabled || git {
		return "", nil
	}
	return prepareCommandCache(cwd)
}

func prepareCommandCache(cwd string) (string, error) {
	canonical, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return "", err
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return "", err
	}
	directory := filepath.Join(canonical, ".opcode", "command-cache", "go")
	root, err := localstate.OpenPrivateRoot(directory, true)
	if err != nil {
		return "", fmt.Errorf("cannot prepare private command cache: %w", err)
	}
	defer root.Close()
	// Builds in the project never show up as changes to commit.
	ignore := filepath.Join(commandCacheRoot(canonical), ".gitignore")
	if _, err := os.Lstat(ignore); os.IsNotExist(err) {
		if err := os.WriteFile(ignore, []byte("*\n"), 0o600); err != nil {
			return "", fmt.Errorf("cannot prepare private command cache: %w", err)
		}
	}
	return directory, nil
}

func commandCacheEnvironment(env []string, directory string) []string {
	paths := map[string]string{"GOCACHE": filepath.Join(directory, "build"), "GOPATH": filepath.Join(directory, "path"), "GOMODCACHE": filepath.Join(directory, "path", "pkg", "mod")}
	for i, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		path, ok := paths[key]
		if !ok {
			continue
		}
		// Explicit user-global envAllow overrides keep their existing semantics.
		if cfg := config.Get(); cfg != nil && slices.Contains(cfg.Security.EnvAllow, key) {
			if _, set := os.LookupEnv(key); set {
				continue
			}
		}
		env[i] = key + "=" + path
	}
	return env
}
