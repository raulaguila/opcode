package shell

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/localstate"
)

const sandboxCachePath = "/tmp/opcode-command-cache"

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
