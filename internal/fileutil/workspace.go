package fileutil

import (
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
)

// WorkspaceFiles lists the files under root by name, through ripgrep when it
// is installed (which honors .gitignore), else by walking the tree.
func WorkspaceFiles(root string) []string {
	var list []string
	if cmd := GetRgCmd(""); cmd != nil {
		cmd.Dir = root
		if raw, err := cmd.Output(); err == nil {
			for _, path := range strings.Split(string(raw), "\x00") {
				if path = strings.TrimPrefix(path, "./"); path != "" {
					list = append(list, path)
				}
			}
		}
	}
	if list == nil {
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || len(list) >= 20000 {
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
