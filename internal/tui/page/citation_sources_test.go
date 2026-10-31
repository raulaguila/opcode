package page

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/stretchr/testify/require"
)

func citationSourceWorkspace(t *testing.T) (*chatPage, string) {
	t.Helper()
	ensureSetupTestConfig(t)
	cfg := config.Get()
	old := cfg.WorkingDir
	t.Cleanup(func() { cfg.WorkingDir = old })
	root := t.TempDir()
	cfg.WorkingDir = root
	for _, path := range []string{"a/main.go", "b/main.go", "src/only.go"} {
		full := filepath.Join(root, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0700))
		require.NoError(t, os.WriteFile(full, []byte(strings.Repeat("line\n", 80)), 0600))
	}
	p, _ := workspacePage(t)
	return p, root
}

func TestSourceBrowserGroupsFilesKeepsEveryRangeAndRestoresResponseActions(t *testing.T) {
	p, _ := citationSourceWorkspace(t)
	raw := "Read `a/main.go:1-2`, `a/main.go:6-8`, `a/main.go:1-2`, b/main.go:3-4 and src/only.go:7."
	p.openResponseActions(raw)
	require.Equal(t, raw, p.workspace.actions[0].text, "copy response remains unchanged")
	p.workspace.selected = len(p.workspace.actions) - 1
	p.workspaceChoose()
	require.Equal(t, "sources", p.workspace.kind)
	require.Len(t, p.workspace.actions, 4)
	require.True(t, strings.HasPrefix(p.workspace.actions[0].label, "a/main.go"))
	require.True(t, strings.HasPrefix(p.workspace.actions[1].label, "b/main.go"), "ambiguous basenames keep their full repository path")
	require.True(t, strings.HasPrefix(p.workspace.actions[2].label, "only.go"), "unique basenames stay compact")
	p.workspace.selected = 0
	p.workspaceChoose()
	require.Equal(t, "source", p.workspace.kind)
	require.Contains(t, p.workspace.body, "a/main.go:1-2")
	require.Contains(t, p.workspace.body, "a/main.go:6-8")
	require.Len(t, p.workspace.actions, 3)
	require.Equal(t, 6, p.workspace.actions[1].line)
	require.Equal(t, "a/main.go:1-2\na/main.go:6-8", p.workspace.actions[2].text)
	p.workspace.selected = 2
	copy := p.workspaceChoose()().(util.CopyTextMsg)
	require.Equal(t, "a/main.go:1-2\na/main.go:6-8", copy.Text)
	p.backWorkspace()
	require.Equal(t, "sources", p.workspace.kind)
	p.backWorkspace()
	require.Equal(t, "actions", p.workspace.kind)
	require.Equal(t, raw, p.workspace.actions[0].text)
}

func TestSourceBrowserKeepsMoreThanFortyDistinctReferencedRanges(t *testing.T) {
	citationSourceWorkspace(t)
	var text strings.Builder
	for i := 1; i <= 60; i++ {
		fmt.Fprintf(&text, "`a/main.go:%d` ", i)
	}
	sources := responseSources(text.String())
	require.Len(t, sources, 1)
	require.Len(t, sources[0].ranges, 60)
	require.Empty(t, responseSources("https://example.test/a/main.go:1 `a/main.go:8-2` `missing.go:1`"))
}

func TestSourceBrowserFileOpenStillRequiresExternalReadPermission(t *testing.T) {
	p, _ := citationSourceWorkspace(t)
	external := filepath.Join(t.TempDir(), "outside.go")
	require.NoError(t, os.WriteFile(external, []byte("private-preview-marker"), 0600))
	text := "`" + external + ":1`"
	p.openCitationSources(text)
	p.workspaceChoose()
	require.Equal(t, "source", p.workspace.kind)
	cmd := p.workspaceChoose()
	require.NotNil(t, cmd)
	result := cmd().(WorkspaceResultMsg)
	require.Error(t, result.err, "external preview remains fail closed without a permission service")
	require.NotContains(t, result.body, "private-preview-marker")
}

func TestSourceReferencesDoNotInventALineForBareFileNames(t *testing.T) {
	citationSourceWorkspace(t)
	sources := responseSources("See `a/main.go` and `a/main.go:6-8`.")
	require.Len(t, sources, 1)
	require.Equal(t, "a/main.go\na/main.go:6-8", sourceReferences(sources))
}
