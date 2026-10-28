package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools/shell"
)

// WorkspaceDigest covers regular workspace files, including external edits.
// Secrets, links, storage and generated/dependency directories are excluded.
func WorkspaceDigest(ctx context.Context) (string, error) {
	rootPath := config.WorkingDirectory()
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return "", err
	}
	defer root.Close()
	hash := sha256.New()
	count := 0
	var total int64
	err = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		full := filepath.Join(rootPath, filepath.FromSlash(path))
		if path != "." {
			skip := shell.SensitivePath(full)
			if cfg := config.Get(); cfg != nil && cfg.Data.Directory != "" && inside(absolutePath(cfg.Data.Directory), full) {
				skip = true
			}
			if entry.IsDir() {
				switch entry.Name() {
				case "node_modules", "vendor", ".venv", "venv", "__pycache__", "target", "dist", "build", ".cache", ".pytest_cache", ".mypy_cache", ".ruff_cache", "htmlcov", "coverage":
					skip = true
				}
				if skip {
					return fs.SkipDir
				}
			} else if skip {
				return nil
			}
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			fmt.Fprintf(hash, "link:%s\x00", path)
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		count++
		total += info.Size()
		if count > 20000 || total > 128<<20 || info.Size() > 16<<20 {
			return fmt.Errorf("workspace exceeds validation fingerprint limit (20000 files, 128 MiB total, 16 MiB per file)")
		}
		file, err := root.Open(filepath.FromSlash(path))
		if err != nil {
			return err
		}
		fmt.Fprintf(hash, "%d:%s:%d\x00", len(path), path, info.Size())
		_, readErr := io.Copy(hash, io.LimitReader(file, 16<<20+1))
		closeErr := file.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		fmt.Fprint(hash, "\x00")
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func ProjectContextPaths(question string) []string {
	lower := strings.ToLower(question)
	project := false
	for _, term := range []string{"projeto", "reposit", "codebase", "this project", "current project", "this repo", "estrutura atual", "arquitetura atual"} {
		project = project || strings.Contains(lower, term)
	}
	if !project {
		return nil
	}
	root, err := os.OpenRoot(config.WorkingDirectory())
	if err != nil {
		return nil
	}
	defer root.Close()
	groups := [][]string{{"README.md", "readme.md", "README.rst", "README"}, {"go.mod", "package.json", "pyproject.toml", "Cargo.toml", "pom.xml"}, {"main.go", "src/main.rs", "src/main.ts", "src/main.py", "main.py", "src/index.ts", "index.js"}, {"AGENTS.md"}}
	var paths []string
	for _, group := range groups {
		for _, path := range group {
			info, err := root.Lstat(path)
			if err == nil && info.Mode().IsRegular() && !shell.SensitivePath(filepath.Join(config.WorkingDirectory(), path)) {
				paths = append(paths, path)
				break
			}
		}
	}
	return paths
}

// FileFingerprint hashes a protected source without recording edit-read evidence.
func FileFingerprint(ctx context.Context, path string) (string, error) {
	f, err := openFile(ctx, path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(f, 16<<20+1))
	if err != nil {
		return "", err
	}
	if n > 16<<20 {
		return "", fmt.Errorf("file exceeds fingerprint limit")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
