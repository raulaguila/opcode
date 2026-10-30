package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
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
	walker := workspaceHasher{ctx: ctx, root: root, rootPath: rootPath, digest: sha256.New()}
	if err := fs.WalkDir(root.FS(), ".", walker.visit); err != nil {
		return "", err
	}
	return hex.EncodeToString(walker.digest.Sum(nil)), nil
}

type workspaceHasher struct {
	ctx      context.Context
	root     *os.Root
	rootPath string
	digest   hash.Hash
	count    int
	total    int64
}

func (w *workspaceHasher) visit(path string, entry fs.DirEntry, walkErr error) error {
	if walkErr != nil {
		return walkErr
	}
	if err := w.ctx.Err(); err != nil {
		return err
	}
	if path != "." && w.excluded(path, entry) {
		if entry.IsDir() {
			return fs.SkipDir
		}
		return nil
	}
	if entry.IsDir() {
		return nil
	}
	if entry.Type()&os.ModeSymlink != 0 {
		fmt.Fprintf(w.digest, "link:%s\x00", path)
		return nil
	}
	info, err := entry.Info()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	w.count++
	w.total += info.Size()
	if w.count > 20000 || w.total > 128<<20 || info.Size() > 16<<20 {
		return fmt.Errorf("workspace exceeds validation fingerprint limit (20000 files, 128 MiB total, 16 MiB per file)")
	}
	return w.writeFile(path, info.Size())
}

func (w *workspaceHasher) excluded(path string, entry fs.DirEntry) bool {
	full := filepath.Join(w.rootPath, filepath.FromSlash(path))
	if shell.SensitivePath(full) {
		return true
	}
	if cfg := config.Get(); cfg != nil && cfg.Data.Directory != "" && inside(absolutePath(cfg.Data.Directory), full) {
		return true
	}
	if !entry.IsDir() {
		return false
	}
	switch entry.Name() {
	case "node_modules", "vendor", ".venv", "venv", "__pycache__", "target", "dist", "build", ".cache", ".pytest_cache", ".mypy_cache", ".ruff_cache", "htmlcov", "coverage":
		return true
	}
	return false
}
func (w *workspaceHasher) writeFile(path string, size int64) error {
	file, err := w.root.Open(filepath.FromSlash(path))
	if err != nil {
		return err
	}
	fmt.Fprintf(w.digest, "%d:%s:%d\x00", len(path), path, size)
	_, readErr := io.Copy(w.digest, io.LimitReader(file, 16<<20+1))
	closeErr := file.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	fmt.Fprint(w.digest, "\x00")
	return nil
}

func ProjectContextPaths(question string) []string {
	lower := strings.ToLower(question)
	project := false
	for _, term := range []string{"projeto", "reposit", "codebase", "this project", "current project", "this repo", "estrutura atual", "arquitetura atual"} {
		project = project || strings.Contains(lower, term)
	}
	if !project && !ProjectInquiry(question) {
		return nil
	}
	root, err := os.OpenRoot(config.WorkingDirectory())
	if err != nil {
		return nil
	}
	defer root.Close()
	var explicit []string
	for _, path := range ProjectSourcePaths(question) {
		info, err := root.Lstat(path)
		if err == nil && info.Mode().IsRegular() && !shell.SensitivePath(filepath.Join(config.WorkingDirectory(), path)) {
			explicit = append(explicit, path)
		}
	}
	if len(explicit) > 0 {
		return explicit
	}
	groups := [][]string{{"README.md", "readme.md", "README.rst", "README"}, {"go.mod", "package.json", "pyproject.toml", "Cargo.toml", "pom.xml"}, {"main.go", "src/main.rs", "src/main.ts", "src/main.py", "main.py", "src/index.ts", "index.js"}, {"AGENTS.md"}}
	var paths []string
	for _, group := range groups {
		if path := firstProjectContextPath(root, group); path != "" {
			paths = append(paths, path)
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

func firstProjectContextPath(root *os.Root, paths []string) string {
	for _, path := range paths {
		info, err := root.Lstat(path)
		if err == nil && info.Mode().IsRegular() && !shell.SensitivePath(filepath.Join(config.WorkingDirectory(), path)) {
			return path
		}
	}
	return ""
}
