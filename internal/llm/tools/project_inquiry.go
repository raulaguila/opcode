package tools

import (
	"path/filepath"
	"regexp"

	"github.com/raulaguila/opcode/internal/config"
)

var projectSourcePath = regexp.MustCompile(`[[:alnum:]_./-]+\.[[:alpha:]][[:alnum:]_]*`)

// ProjectSourcePaths only identifies literal relative source locators. The
// caller still checks regular files, sensitive paths and the read policy.
func ProjectSourcePaths(question string) []string {
	var paths []string
	seen := map[string]bool{}
	for _, path := range projectSourcePath.FindAllString(question, 8) {
		if filepath.IsAbs(path) {
			if relative, err := filepath.Rel(config.WorkingDirectory(), path); err == nil {
				path = relative
			}
		}
		if filepath.IsLocal(path) && !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	return paths
}
