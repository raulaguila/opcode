package desktop

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestChangesListTurnsNewestFirstWithTheirUndoDiff(t *testing.T) {
	root := t.TempDir()
	settings := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(settings, []byte(`{}`), 0600))
	oldPath := viper.ConfigFileUsed()
	viper.SetConfigFile(settings)
	t.Cleanup(func() { viper.SetConfigFile(oldPath) })
	_, err := config.Load(root, false)
	require.NoError(t, err)
	cfg := config.Get()
	dir, data := cfg.WorkingDir, cfg.Data.Directory // Load keeps the first test's folder
	cfg.WorkingDir, cfg.Data.Directory = root, filepath.Join(root, ".opcode")
	t.Cleanup(func() { cfg.WorkingDir, cfg.Data.Directory = dir, data })
	a, b := filepath.Join(root, "a.go"), filepath.Join(root, "b.go")
	require.NoError(t, reviewStore().Record("s", "t1", []tools.FileChange{{Path: a, Before: new("one\n"), After: new("two\n")}}))
	require.NoError(t, reviewStore().Record("s", "t2", []tools.FileChange{{Path: b, Before: nil, After: new("new\n")}}))

	r := httptest.NewRequest("GET", "/api/sessions/s/changes", nil)
	r.SetPathValue("id", "s")
	w := httptest.NewRecorder()
	(&Server{}).changes(w, r)
	var list []Change
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list, 2)
	require.Equal(t, "t2", list[0].ID, "newest first")
	require.Equal(t, []string{"b.go"}, list[0].Files)
	require.Contains(t, list[1].Diff, "-two")
	require.Contains(t, list[1].Diff, "+one", "the diff is what undo would apply")
	require.NotEmpty(t, list[1].Version)
}
