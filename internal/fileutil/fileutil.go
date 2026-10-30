package fileutil

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/raulaguila/opcode/internal/logging"
)

var (
	rgPath  string
	fzfPath string
)

func init() {
	var err error
	rgPath, err = exec.LookPath("rg")
	if err != nil {
		logging.Warn("Ripgrep (rg) not found in $PATH. Some features might be limited or slower.")
		rgPath = ""
	}
	fzfPath, err = exec.LookPath("fzf")
	if err != nil {
		logging.Warn("FZF not found in $PATH. Some features might be limited or slower.")
		fzfPath = ""
	}
}

func GetRgCmd(globPattern string) *exec.Cmd {
	if rgPath == "" {
		return nil
	}
	rgArgs := []string{
		"--files",
		"-L",
		"--null",
	}
	if globPattern != "" {
		if !filepath.IsAbs(globPattern) && !strings.HasPrefix(globPattern, "/") {
			globPattern = "/" + globPattern
		}
		rgArgs = append(rgArgs, "--glob", globPattern)
	}
	cmd := exec.Command(rgPath, rgArgs...)
	cmd.Dir = "."
	return cmd
}

func GetFzfCmd(query string) *exec.Cmd {
	if fzfPath == "" {
		return nil
	}
	fzfArgs := []string{
		"--filter",
		query,
		"--read0",
		"--print0",
	}
	cmd := exec.Command(fzfPath, fzfArgs...)
	cmd.Dir = "."
	return cmd
}

type FileInfo struct {
	Path    string
	ModTime time.Time
}

func SkipHidden(path string) bool {
	// Check for hidden files (starting with a dot)
	base := filepath.Base(path)
	if base != "." && strings.HasPrefix(base, ".") {
		return true
	}

	commonIgnoredDirs := map[string]bool{
		".opcode":          true,
		".opencode":        true,
		"node_modules":     true,
		"vendor":           true,
		"dist":             true,
		"build":            true,
		"target":           true,
		".git":             true,
		".idea":            true,
		".vscode":          true,
		"__pycache__":      true,
		"bin":              true,
		"obj":              true,
		"out":              true,
		"coverage":         true,
		"tmp":              true,
		"temp":             true,
		"logs":             true,
		"generated":        true,
		"bower_components": true,
		"jspm_packages":    true,
	}

	parts := strings.SplitSeq(path, string(os.PathSeparator))
	for part := range parts {
		if commonIgnoredDirs[part] {
			return true
		}
	}
	return false
}

func GlobWithDoublestar(pattern, searchPath string, limit int) ([]string, bool, error) {
	fsys := os.DirFS(searchPath)
	relPattern := strings.TrimPrefix(pattern, "/")
	collector := globCollector{searchPath: searchPath, limit: limit}

	err := doublestar.GlobWalk(fsys, relPattern, func(path string, d fs.DirEntry) error {
		return collector.collect(path, d)
	})
	if err != nil {
		return nil, false, fmt.Errorf("glob walk error: %w", err)
	}

	matches := collector.matches
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].ModTime.After(matches[j].ModTime)
	})

	truncated := false
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
		truncated = true
	}

	results := make([]string, len(matches))
	for i, m := range matches {
		results[i] = m.Path
	}
	return results, truncated, nil
}

type globCollector struct {
	searchPath string
	limit      int
	matches    []FileInfo
}

func (c *globCollector) collect(path string, d fs.DirEntry) error {
	if d.IsDir() {
		return nil
	}
	if SkipHidden(path) {
		return nil
	}
	info, err := d.Info()
	if err != nil {
		return nil
	}
	absPath := path
	if !strings.HasPrefix(absPath, c.searchPath) && c.searchPath != "." {
		absPath = filepath.Join(c.searchPath, absPath)
	} else if !strings.HasPrefix(absPath, "/") && c.searchPath == "." {
		absPath = filepath.Join(c.searchPath, absPath) // Ensure relative paths are joined correctly
	}

	c.matches = append(c.matches, FileInfo{Path: absPath, ModTime: info.ModTime()})
	if c.limit > 0 && len(c.matches) >= c.limit*2 {
		return fs.SkipAll
	}
	return nil
}
