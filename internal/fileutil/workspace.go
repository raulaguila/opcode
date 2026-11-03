package fileutil

import (
	"bufio"
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// workspaceLimit bounds the listing: a workspace opened at a home folder can
// hold millions of files.
const workspaceLimit = 20000

// WorkspaceFiles lists up to workspaceLimit files under root by name, through
// ripgrep when it is installed (which honors .gitignore), else by walking
// the tree.
func WorkspaceFiles(root string) []string {
	var list []string
	if cmd := GetRgCmd(""); cmd != nil {
		cmd.Dir = root
		if out, err := cmd.StdoutPipe(); err == nil && cmd.Start() == nil {
			scanner := bufio.NewScanner(out)
			scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
				if i := bytes.IndexByte(data, 0); i >= 0 {
					return i + 1, data[:i], nil
				}
				if atEOF && len(data) > 0 {
					return len(data), data, nil
				}
				return 0, nil, nil
			})
			for len(list) < workspaceLimit && scanner.Scan() {
				if path := strings.TrimPrefix(scanner.Text(), "./"); path != "" {
					list = append(list, path)
				}
			}
			_ = cmd.Process.Kill() // stops a listing past the limit; harmless after it ends
			_ = cmd.Wait()
		}
	}
	if list == nil {
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || len(list) >= workspaceLimit {
				return filepath.SkipDir
			}
			name := entry.Name()
			if entry.IsDir() && path != root && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor") {
				return filepath.SkipDir
			}
			if !entry.IsDir() {
				if rel, err := filepath.Rel(root, path); err == nil {
					list = append(list, rel)
				}
			}
			return nil
		})
	}
	return list
}

// MatchFiles returns up to limit paths containing query, best first: a file
// name that starts with the query leads, then shorter and earlier matches.
func MatchFiles(all []string, query string, limit int) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	type scored struct {
		path  string
		score int
	}
	var hits []scored
	for _, path := range all {
		lower := strings.ToLower(path)
		at := strings.Index(lower, query)
		if query != "" && at < 0 {
			continue
		}
		score := len(path)
		if base := strings.ToLower(filepath.Base(path)); strings.HasPrefix(base, query) {
			score -= 1000
		}
		hits = append(hits, scored{path, score + at})
	}
	slices.SortStableFunc(hits, func(a, b scored) int { return a.score - b.score })
	out := []string{}
	for _, hit := range hits[:min(len(hits), limit)] {
		out = append(out, hit.path)
	}
	return out
}

// HomeRelative writes a path under the home directory as ~/….
func HomeRelative(path string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

var branches struct {
	sync.Mutex
	dir, name string
	at        time.Time
}

// GitBranch is the branch checked out in dir (a short commit when detached),
// read from .git/HEAD and following a worktree's gitdir file. Screens ask on
// every frame, so the answer is kept for two seconds.
func GitBranch(dir string) string {
	branches.Lock()
	defer branches.Unlock()
	if branches.dir == dir && time.Since(branches.at) < 2*time.Second {
		return branches.name
	}
	branches.dir, branches.name, branches.at = dir, readBranch(dir), time.Now()
	return branches.name
}

func readBranch(dir string) string {
	git := filepath.Join(dir, ".git")
	if raw, err := os.ReadFile(git); err == nil {
		target, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir: ")
		if !ok {
			return ""
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(dir, target)
		}
		git = target
	}
	raw, err := os.ReadFile(filepath.Join(git, "HEAD"))
	if err != nil {
		return ""
	}
	head := strings.TrimSpace(string(raw))
	if ref, ok := strings.CutPrefix(head, "ref: refs/heads/"); ok {
		return ref
	}
	return head[:min(len(head), 7)]
}
