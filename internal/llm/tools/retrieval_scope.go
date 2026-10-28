package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools/shell"
	"github.com/raulaguila/opcode/internal/lsp"
)

type retrievalScope struct {
	Path      string `json:"path"`
	Glob      string `json:"glob"`
	Language  string `json:"language"`
	Extension string `json:"extension"`
}

func scopeParameters() map[string]any {
	return map[string]any{
		"path":      map[string]any{"type": "string", "description": "File or directory scope; defaults to the workspace."},
		"glob":      map[string]any{"type": "string", "description": "Path pattern, e.g. **/*.go."},
		"language":  map[string]any{"type": "string", "description": "Language name or alias, e.g. python, go, ts, cpp."},
		"extension": map[string]any{"type": "string", "description": "File extension, e.g. .go."},
	}
}

func (s *retrievalScope) validate() error {
	for _, value := range []string{s.Path, s.Glob, s.Language, s.Extension} {
		if len(value) > 500 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return fmt.Errorf("filters must be at most 500 bytes without control characters")
		}
	}
	if _, err := doublestar.Match(s.Glob, "probe"); err != nil {
		return fmt.Errorf("invalid glob filter")
	}
	s.Language = strings.ToLower(strings.TrimSpace(s.Language))
	aliases := map[string]string{"js": "javascript", "node": "javascript", "nodejs": "javascript", "ts": "typescript", "py": "python", "golang": "go", "c++": "cpp", "c#": "csharp", "rs": "rust", "shell": "bash", "shellscript": "bash", "protobuf": "proto"}
	if alias := aliases[s.Language]; alias != "" {
		s.Language = alias
	}
	if s.Extension != "" {
		s.Extension = "." + strings.TrimLeft(strings.ToLower(s.Extension), ".")
		if strings.ContainsAny(s.Extension, "/\\*?[") || s.Extension == "." {
			return fmt.Errorf("extension must be a file suffix")
		}
	}
	return nil
}

func sourceLanguage(path string) string {
	switch strings.ToLower(filepath.Base(path)) {
	case "dockerfile", "containerfile":
		return "dockerfile"
	case "makefile", "gnumakefile":
		return "make"
	case "cmakelists.txt":
		return "cmake"
	}
	extra := map[string]string{
		".pyi": "python", ".mjs": "javascript", ".cjs": "javascript", ".mts": "typescript", ".cts": "typescript",
		".h": "c", ".hpp": "cpp", ".hh": "cpp", ".hxx": "cpp", ".kt": "kotlin", ".kts": "kotlin",
		".zig": "zig", ".vue": "vue", ".svelte": "svelte", ".proto": "proto", ".graphql": "graphql", ".gql": "graphql",
		".tf": "hcl", ".hcl": "hcl", ".toml": "toml", ".ml": "ocaml", ".mli": "ocaml", ".jl": "julia",
		".nim": "nim", ".sol": "solidity", ".cljc": "clojure", ".cljs": "clojure", ".edn": "clojure",
	}
	if language := extra[strings.ToLower(filepath.Ext(path))]; language != "" {
		return language
	}
	language := string(lsp.DetectLanguageID(path))
	switch language {
	case "javascriptreact":
		return "javascript"
	case "typescriptreact":
		return "tsx"
	case "shellscript":
		return "bash"
	case "makefile":
		return "make"
	case "":
		return "text"
	}
	return language
}

func (s retrievalScope) matches(path string) bool {
	path = filepath.ToSlash(path)
	if s.Glob != "" {
		match, _ := doublestar.Match(s.Glob, path)
		baseMatch, _ := doublestar.Match(s.Glob, filepath.Base(path))
		if !match && !baseMatch {
			return false
		}
	}
	return (s.Language == "" || s.Language == sourceLanguage(path)) && (s.Extension == "" || strings.ToLower(filepath.Ext(path)) == s.Extension)
}

func retrievalPathAllowed(root, path string, ignore []string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsLocal(rel) || shell.SensitivePath(path) {
		return false
	}
	for _, pattern := range ignore {
		if match, _ := doublestar.Match(pattern, filepath.ToSlash(rel)); match {
			return false
		}
	}
	if cfg := config.Get(); cfg != nil && cfg.Data.Directory != "" && inside(absolutePath(cfg.Data.Directory), path) {
		return false
	}
	partPath := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		partPath = filepath.Join(partPath, part)
		if shouldSkip(partPath, ignore) || part == ".opcode-worktrees" || part == ".venv" || part == "venv" {
			return false
		}
	}
	resolved, err := canonicalPath(path)
	return err == nil && resolved == path
}

func scopedFilePaths(ctx context.Context, root string, scope retrievalScope, ignore []string) ([]string, bool, error) {
	paths, truncated, err := findFiles(ctx, root, "", 20000)
	if err != nil {
		return nil, false, err
	}
	files := []string{}
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		rel, _ := filepath.Rel(root, path)
		if !retrievalPathAllowed(root, path, ignore) || !scope.matches(rel) {
			continue
		}
		info, err := os.Lstat(path)
		if err == nil && info.Mode().IsRegular() {
			files = append(files, filepath.ToSlash(rel))
		}
	}
	sort.Strings(files)
	return files, truncated, nil
}
