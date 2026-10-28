package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools/shell"
	"github.com/stretchr/testify/require"
)

func TestGitToolsPreserveUserChangesAndHideCredentialDiffs(t *testing.T) {
	testUnisolated(t)
	dir := t.TempDir()
	original := config.Get().WorkingDir
	config.Get().WorkingDir = dir
	t.Cleanup(func() { config.Get().WorkingDir = original })
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = shell.Environment()
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
		return string(out)
	}
	git("init", "-q")
	path := filepath.Join(dir, "file ' with spaces.txt")
	require.NoError(t, os.WriteFile(path, []byte("before\n"), 0640))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("PRIVATE=before\n"), 0600))
	git("add", "--all")
	git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "initial")
	require.NoError(t, os.WriteFile(path, []byte("after\n"), 0640))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("PRIVATE=not-for-model\n"), 0600))
	before := git("status", "--porcelain=v1")
	response, err := (&gitTool{name: "git_status"}).Run(toolContext(), toolInput(map[string]any{}))
	require.NoError(t, err)
	require.Contains(t, response.Content, "file ' with spaces.txt")
	response, err = (&gitTool{name: "git_diff"}).Run(toolContext(), toolInput(map[string]any{}))
	require.NoError(t, err)
	require.Contains(t, response.Content, "+after")
	require.NotContains(t, response.Content, "not-for-model")
	response, err = (&gitTool{name: "git_log"}).Run(toolContext(), toolInput(map[string]any{"limit": 1}))
	require.NoError(t, err)
	require.Contains(t, response.Content, "initial")
	require.Equal(t, before, git("status", "--porcelain=v1"))
}

type navigationStub struct {
	method string
	params map[string]any
	raw    json.RawMessage
}

func (n *navigationStub) OpenFile(context.Context, string) error { return nil }
func (n *navigationStub) Call(_ context.Context, method string, params any, result any) error {
	n.method = method
	n.params = params.(map[string]any)
	*result.(*json.RawMessage) = n.raw
	return nil
}
func TestNavigationRequestsAndBounds(t *testing.T) {
	for _, kind := range []string{"symbols", "definition", "references"} {
		t.Run(kind, func(t *testing.T) {
			stub := &navigationStub{raw: json.RawMessage(`{"uri":"file:///example.go","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}}}`)}
			response, err := navigate(context.Background(), stub, kind, "/workspace/file.go", 2, 3)
			require.NoError(t, err)
			require.Contains(t, response.Content, "file:///example.go")
			if kind == "symbols" {
				require.Equal(t, "textDocument/documentSymbol", stub.method)
			} else {
				require.Equal(t, "textDocument/"+kind, stub.method)
				require.Equal(t, map[string]int{"line": 2, "character": 3}, stub.params["position"])
			}
			if kind == "references" {
				require.Equal(t, map[string]bool{"includeDeclaration": true}, stub.params["context"])
			}
		})
	}
	stub := &navigationStub{raw: json.RawMessage("[" + strings.TrimSuffix(strings.Repeat(`{"name":"symbol"},`, 105), ",") + "]")}
	response, err := navigate(context.Background(), stub, "symbols", "/workspace/file.go", 0, 0)
	require.NoError(t, err)
	var result struct {
		Items     []any
		Truncated bool
	}
	require.NoError(t, json.Unmarshal([]byte(response.Content), &result))
	require.Len(t, result.Items, 100)
	require.True(t, result.Truncated)
}
