package ui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestChangesUndoAndRedoATurnThroughApproval(t *testing.T) {
	root := t.TempDir()
	settings := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(settings, []byte(`{}`), 0600))
	oldPath := viper.ConfigFileUsed()
	viper.SetConfigFile(settings)
	t.Cleanup(func() { viper.SetConfigFile(oldPath) })
	_, err := config.Load(root, false)
	require.NoError(t, err)
	useWorkspace(t, root)

	file := filepath.Join(root, "main.go")
	require.NoError(t, os.WriteFile(file, []byte("after\n"), 0600))
	require.NoError(t, reviewStore().Record("s", "turn-1", []tools.FileChange{{Path: file, Before: new("before\n"), After: new("after\n")}}))

	perms := permission.NewPermissionService()
	perms.AutoApproveSession("s")
	m := New(nil)
	m.app = &app.App{Permissions: perms}
	m.session = session.Session{ID: "s"}
	m.width, m.height = 130, 40

	m.openChanges()
	view := ansi.Strip(m.View().Content)
	require.Contains(t, view, "main.go")
	require.Contains(t, view, "Enter undoes:")
	cmd := m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd)
	m.Update(cmd())
	raw, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, "before\n", string(raw))
	require.Contains(t, m.info, "undone")

	m.Update(m.undoLast(true)())
	raw, err = os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, "after\n", string(raw))
	require.Contains(t, m.info, "redone")
	require.Nil(t, m.undoLast(true), "nothing is left to redo")
}

// useWorkspace points the loaded config at root for one test: Load keeps the
// first test's folder, data directory and servers.
func useWorkspace(t *testing.T, root string) {
	cfg := config.Get()
	dir, data, servers := cfg.WorkingDir, cfg.Data.Directory, cfg.LSP
	cfg.WorkingDir, cfg.Data.Directory, cfg.LSP = root, filepath.Join(root, ".opcode"), nil
	t.Cleanup(func() { cfg.WorkingDir, cfg.Data.Directory, cfg.LSP = dir, data, servers })
}

func TestQuitAsksFirst(t *testing.T) {
	m := New(nil)
	m.width, m.height = 100, 30
	require.Nil(t, m.key(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}))
	require.Contains(t, ansi.Strip(m.View().Content), "Quit opcode?")
	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	require.Nil(t, m.dialog, "Esc stays")
	m.key(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	cmd := m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.IsType(t, tea.QuitMsg{}, cmd())
}
