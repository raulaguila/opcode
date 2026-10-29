package tools

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools/shell"
	ignore "github.com/sabhiram/go-gitignore"
)

type ignoreCacheEntry struct {
	stamp   string
	matcher *ignore.GitIgnore
}

var discoveryMu sync.Mutex
var discoveryCache = map[string]ignoreCacheEntry{}

func directoryIgnore(dir, name string) (*ignore.GitIgnore, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat(name)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return nil, fmt.Errorf("ignore policy must be a regular file <=64 KiB")
	}
	path := filepath.Join(dir, name)
	stamp := indexFileStamp(info)
	discoveryMu.Lock()
	cached, ok := discoveryCache[path]
	discoveryMu.Unlock()
	if ok && cached.stamp == stamp {
		return cached.matcher, nil
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(f, 64<<10+1))
	f.Close()
	if err != nil {
		return nil, err
	}
	if len(raw) > 64<<10 {
		return nil, fmt.Errorf("ignore policy exceeds 64 KiB")
	}
	matcher := ignore.CompileIgnoreLines(strings.Split(string(raw), "\n")...)
	discoveryMu.Lock()
	if len(discoveryCache) >= 256 {
		clear(discoveryCache)
	}
	discoveryCache[path] = ignoreCacheEntry{stamp, matcher}
	discoveryMu.Unlock()
	return matcher, nil
}

// ExclusionReason is diagnostic only. Negation can never override fixed secret,
// private storage, symlink or dependency exclusions.
func ExclusionReason(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsLocal(rel) {
		return "outside workspace"
	}
	if shell.SensitivePath(path) {
		return "credential or sensitive path"
	}
	if cfg := config.Get(); cfg != nil && cfg.Data.Directory != "" && inside(absolutePath(cfg.Data.Directory), path) {
		return "private application storage"
	}
	dir := root
	parts := strings.Split(rel, string(filepath.Separator))
	for i, part := range parts {
		full := filepath.Join(dir, part)
		if info, err := os.Lstat(full); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "symlink"
		}
		if shouldSkip(full, nil) || part == ".opcode-worktrees" || part == "venv" || part == ".venv" {
			return "hidden, generated or dependency path"
		}
		suffix := filepath.ToSlash(filepath.Join(parts[i:]...))
		for _, name := range []string{".gitignore", ".opcodeignore"} {
			matcher, e := directoryIgnore(dir, name)
			if e != nil {
				return "unreadable or unsafe ignore policy"
			}
			if matcher != nil && matcher.MatchesPath(suffix) {
				return filepath.ToSlash(filepath.Join(strings.Join(parts[:i], "/"), name))
			}
		}
		if i < len(parts)-1 {
			dir = full
		}
	}
	return ""
}
func listingSnapshot(ctx context.Context, root string, files []string) (string, error) {
	h := sha256.New()
	for _, name := range files {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return "", fmt.Errorf("listing changed; restart from offset 0")
		}
		fmt.Fprintf(h, "%s\x00%s\n", name, indexFileStamp(info))
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
func indexLimits() config.IndexConfig {
	if cfg := config.Get(); cfg != nil {
		return cfg.Index.Defaults()
	}
	return (config.IndexConfig{}).Defaults()
}

func discoveryPolicyRoot(root string) string {
	workspace := config.WorkingDirectory()
	if inside(workspace, root) {
		return workspace
	}
	if info, err := os.Stat(root); err == nil && !info.IsDir() {
		return filepath.Dir(root)
	}
	return root
}
