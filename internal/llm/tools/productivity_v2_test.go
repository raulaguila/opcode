package tools

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/stretchr/testify/require"
)

func productivityV2Workspace(t *testing.T) string {
	t.Helper()
	testUnisolated(t)
	dir := t.TempDir()
	previousWD, previousData := config.Get().WorkingDir, config.Get().Data
	config.Get().WorkingDir = dir
	config.Get().Data.Directory = filepath.Join(t.TempDir(), "private")
	t.Cleanup(func() { config.Get().WorkingDir = previousWD; config.Get().Data = previousData })
	return dir
}
func testGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := command.CombinedOutput()
	require.NoError(t, err, string(out))
	return string(out)
}
func TestCodeIndexIncrementalIgnorePrivacyAndRemoval(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg required")
	}
	dir := productivityV2Workspace(t)
	testGit(t, dir, "init", "-q")
	t.Setenv("OPCODE_INDEX_API_KEY", "index-private-known-credential")
	for name, content := range map[string]string{"normal.go": "package demo\nfunc UniqueDeclaration() {}\n// index-private-known-credential\n", ".gitignore": "ignored.go\n", "ignored.go": "UniqueDeclaration ignored private\n", ".env": "UniqueDeclaration credential file\n"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0600))
	}
	out := filepath.Join(t.TempDir(), "external.go")
	require.NoError(t, os.WriteFile(out, []byte("UniqueDeclaration external private"), 0600))
	require.NoError(t, os.Symlink(out, filepath.Join(dir, "linked.go")))
	tool := SecureTool(NewCodeIndexTool(), &denyingPermissions{})
	response, err := tool.Run(toolContext(), toolInput(map[string]any{"query": "UniqueDeclaration"}))
	require.NoError(t, err)
	require.False(t, response.IsError)
	require.Contains(t, response.Content, "normal.go")
	require.NotContains(t, response.Content, "ignored private")
	require.NotContains(t, response.Content, "external private")
	require.NotContains(t, response.Content, "credential file")
	require.NotContains(t, response.Content, "index-private-known-credential")
	response, err = tool.Run(toolContext(), toolInput(map[string]any{"query": "UniqueDeclaration"}))
	require.NoError(t, err)
	require.Contains(t, response.Content, `"updated_files":0`)
	config.Get().Security.DenyPaths = []string{"normal.go"}
	response, err = tool.Run(toolContext(), toolInput(map[string]any{"query": "UniqueDeclaration"}))
	require.NoError(t, err)
	require.Contains(t, response.Content, `"results":[]`)
	config.Get().Security.DenyPaths = nil
	indexPath := filepath.Join(config.Get().Data.Directory, "code-index", indexDigest(dir)+"-v2.sqlite")
	info, err := os.Stat(indexPath)
	require.NoError(t, err)
	require.EqualValues(t, 0600, info.Mode().Perm())
	db, err := sql.Open("sqlite3", indexPath)
	require.NoError(t, err)
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM chunks WHERE content LIKE '%index-private-known-credential%'`).Scan(&count))
	require.Zero(t, count)
	db.Close()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "normal.go"), []byte("package demo\nfunc ReplacementDeclaration() {}"), 0600))
	response, err = tool.Run(toolContext(), toolInput(map[string]any{"query": "UniqueDeclaration"}))
	require.NoError(t, err)
	require.NotContains(t, response.Content, "func UniqueDeclaration", "fuzzy matches may remain, but stale declarations must not")
	require.NoError(t, os.Remove(filepath.Join(dir, "normal.go")))
	response, err = tool.Run(toolContext(), toolInput(map[string]any{"query": "ReplacementDeclaration"}))
	require.NoError(t, err)
	require.Contains(t, response.Content, `"results":[]`)
}
func TestCodeIndexCancellationAndInputBounds(t *testing.T) {
	productivityV2Workspace(t)
	ctx, cancel := context.WithCancel(toolContext())
	cancel()
	_, err := NewCodeIndexTool().Run(ctx, toolInput(map[string]any{"query": "symbol"}))
	require.ErrorIs(t, err, context.Canceled)
	for _, args := range []map[string]any{{"query": strings.Repeat("x", 513)}, {"query": "word", "limit": 51}, {"query": "\" OR *"}} {
		response, err := NewCodeIndexTool().Run(toolContext(), toolInput(args))
		if args["query"] == "\" OR *" {
			require.NoError(t, err)
			continue
		}
		require.NoError(t, err)
		require.True(t, response.IsError)
	}
}

func TestCodeIndexPrivateDirectoryRejectsAncestorSymlinks(t *testing.T) {
	dir := productivityV2Workspace(t)
	outside := t.TempDir()
	link := filepath.Join(t.TempDir(), "storage-link")
	require.NoError(t, os.Symlink(outside, link))
	config.Get().Data.Directory = filepath.Join(link, "not-created")
	_, err := openCodeIndex(context.Background(), dir)
	require.Error(t, err)
	require.NoDirExists(t, filepath.Join(outside, "not-created"))
}
func TestFormattingDeniedLeavesFileUntouched(t *testing.T) {
	dir := productivityV2Workspace(t)
	path := filepath.Join(dir, "code.go")
	before := []byte("package test\nfunc demo( ){ }\n")
	require.NoError(t, os.WriteFile(path, before, 0600))
	permissions := &denyingPermissions{}
	tool := NewProductivityTools(permissions)[1]
	defer tool.(*formatProjectTool).Close()
	_, err := SecureTool(tool, permissions).Run(toolContext(), toolInput(map[string]any{"preset": "go", "files": []string{"code.go"}}))
	require.ErrorIs(t, err, permission.ErrorPermissionDenied)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Len(t, permissions.requests, 1)
	require.Contains(t, permissions.requests[0].Description, "gofmt")
}
func TestWorktreeApprovalDirtyRemovalAndNoForce(t *testing.T) {
	dir := productivityV2Workspace(t)
	testGit(t, dir, "init", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("initial"), 0600))
	testGit(t, dir, "add", "file.txt")
	testGit(t, dir, "-c", "user.name=Tests", "-c", "user.email=tests@example.invalid", "commit", "-qm", "initial")
	permissions := &denyingPermissions{}
	tool := SecureTool(&worktreeTool{permissions: permissions}, permissions)
	_, err := tool.Run(toolContext(), toolInput(map[string]any{"action": "create", "name": "demo"}))
	require.ErrorIs(t, err, permission.ErrorPermissionDenied)
	path := filepath.Join(dir, ".opcode-worktrees", "demo")
	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err))
	permissions.allow = func(permission.CreatePermissionRequest) bool { return true }
	response, err := tool.Run(toolContext(), toolInput(map[string]any{"action": "create", "name": "demo"}))
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	require.FileExists(t, filepath.Join(path, "file.txt"))
	require.NoError(t, os.WriteFile(filepath.Join(path, "file.txt"), []byte("user changes"), 0600))
	response, err = tool.Run(toolContext(), toolInput(map[string]any{"action": "remove", "name": "demo"}))
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.FileExists(t, filepath.Join(path, "file.txt"))
	require.NoError(t, os.WriteFile(filepath.Join(path, "file.txt"), []byte("initial"), 0600))
	response, err = tool.Run(toolContext(), toolInput(map[string]any{"action": "remove", "name": "demo"}))
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	require.NoDirExists(t, path)
	response, err = tool.Run(toolContext(), toolInput(map[string]any{"action": "remove", "name": "../../"}))
	require.NoError(t, err)
	require.True(t, response.IsError)
	testGit(t, dir, "config", "filter.unsafe.smudge", "touch unexpected")
	response, err = tool.Run(toolContext(), toolInput(map[string]any{"action": "create", "name": "blocked"}))
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Contains(t, response.Content, "filters")
	require.NoFileExists(t, filepath.Join(dir, "unexpected"))
}

func TestCodeIndexMissingRipgrepDoesNotCreateIndex(t *testing.T) {
	productivityV2Workspace(t)
	t.Setenv("PATH", t.TempDir())
	response, err := NewCodeIndexTool().Run(toolContext(), toolInput(map[string]any{"query": "symbol"}))
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Contains(t, response.Content, "ripgrep (rg) is required")
	require.NoDirExists(t, filepath.Join(config.Get().Data.Directory, "code-index"))
	ctx, cancel := context.WithCancel(toolContext())
	cancel()
	_, err = NewCodeIndexTool().Run(ctx, toolInput(map[string]any{"query": "symbol"}))
	require.ErrorIs(t, err, context.Canceled)
}
