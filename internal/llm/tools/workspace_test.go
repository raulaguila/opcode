package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"encoding/json"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceFingerprintTracksSameSizeEditsAndSkipsSecretsAndStorage(t *testing.T) {
	old := config.Get().WorkingDir
	root := t.TempDir()
	config.Get().WorkingDir = root
	t.Cleanup(func() { config.Get().WorkingDir = old })
	path := filepath.Join(root, "code.go")
	require.NoError(t, os.WriteFile(path, []byte("one"), 0600))
	first, err := WorkspaceDigest(context.Background())
	require.NoError(t, err)
	stamp, err := os.Stat(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("two"), 0600))
	require.NoError(t, os.Chtimes(path, stamp.ModTime(), stamp.ModTime()))
	second, err := WorkspaceDigest(context.Background())
	require.NoError(t, err)
	require.NotEqual(t, first, second)
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("never include"), 0600))
	require.NoError(t, os.Mkdir(filepath.Join(root, ".opcode"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".opcode", "db"), []byte("mutable state"), 0600))
	third, err := WorkspaceDigest(context.Background())
	require.NoError(t, err)
	require.Equal(t, second, third)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = WorkspaceDigest(ctx)
	require.ErrorIs(t, err, context.Canceled)
}
func TestOverviewPrefetchUsesOnlyExistingSafePaths(t *testing.T) {
	old := config.Get().WorkingDir
	root := t.TempDir()
	config.Get().WorkingDir = root
	t.Cleanup(func() { config.Get().WorkingDir = old })
	for _, name := range []string{"README.md", "package.json", "AGENTS.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte("contents"), 0600))
	}
	require.Nil(t, ProjectContextPaths("Olá"))
	require.ElementsMatch(t, []string{"README.md", "package.json", "AGENTS.md"}, ProjectContextPaths("Explique este projeto"))
	require.NoError(t, os.WriteFile(filepath.Join(root, "counter.py"), []byte("def add(a,b): return a - b"), 0600))
	require.Equal(t, []string{"counter.py"}, ProjectContextPaths("Localize no projeto a causa de add retornar -1. Leia counter.py e explique."), "a named source takes precedence over the generic README")
}

func TestFileMutationObserverRunsOnCommitAndFailureRestoresFiles(t *testing.T) {
	dir := workspaceTestDir(t)
	path := filepath.Join(dir, "source.go")
	before := "before"
	require.NoError(t, os.WriteFile(path, []byte(before), 0600))
	called := 0
	ctx := WithMutationObserver(toolContext(), func() error { called++; return fmt.Errorf("task storage unavailable") })
	require.ErrorContains(t, atomicWriteFile(ctx, path, "after", &before), "task storage unavailable")
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "before", string(contents))
	require.Equal(t, 1, called)
	ctx = WithMutationObserver(toolContext(), func() error { called++; return nil })
	require.NoError(t, atomicWriteFile(ctx, path, "after", &before))
	require.Equal(t, 2, called)
}

func TestConfiguredChecksRejectCredentialsBeforeApprovalAndReturnValidJSON(t *testing.T) {
	secret := "fixture-api-key-not-real-123456"
	t.Setenv("OPENAI_API_KEY", secret)
	old := config.Get().Checks
	config.Get().Checks = map[string]config.CheckConfig{"test": {Command: "printf " + secret}}
	t.Cleanup(func() { config.Get().Checks = old })
	p := &denyingPermissions{}
	check := NewChecksTool(p)
	defer check.(*checksTool).Close()
	response, err := SecureTool(check, p).Run(toolContext(), ToolCall{Input: `{}`})
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.True(t, json.Valid([]byte(response.Content)))
	require.NotContains(t, response.Content, secret)
	require.Empty(t, p.requests)
}
