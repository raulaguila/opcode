package dialog

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/stretchr/testify/require"
)

func TestSummarizeDiffNamesDeletedDefinitions(t *testing.T) {
	raw := "--- a/store.go\n+++ b/store.go\n@@ -1,4 +1,5 @@\n-func (s *Store) Done(id string) {\n+func (s *Store) Remove(id string) {\n+\treturn\n-func keep() {}\n+func keep() { return }\n"
	added, removed, lost := summarizeDiff(raw)
	require.Equal(t, 3, added)
	require.Equal(t, 2, removed)
	require.Equal(t, []string{"Done"}, lost, "a definition rewritten in place is not lost")
}

func TestPermissionDialogSummarizesEditsFitsShortCommandsAndNotesRepeats(t *testing.T) {
	m := NewPermissionDialogCmp().(*permissionDialogCmp)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m.SetPermissions(permission.PermissionRequest{ID: "edit", ToolName: tools.EditToolName, Path: "project", Params: tools.EditPermissionsParams{FilePath: "/p/store.go", Diff: "--- a/store.go\n+++ b/store.go\n@@ -1 +1 @@\n-func Done() {}\n+func Remove() {}\n"}})
	view := ansi.Strip(m.View())
	require.Contains(t, view, "store.go · +1 −1")
	require.Contains(t, view, "Done")

	bash := permission.PermissionRequest{ID: "b1", ToolName: tools.BashToolName, Path: "project", Params: tools.BashPermissionsParams{Command: "go test ./..."}}
	m.SetPermissions(bash)
	view = m.View()
	require.Less(t, len(strings.Split(view, "\n")), 25, "a one-line command must not fill a 40-row screen")
	require.NotContains(t, ansi.Strip(view), "apareceu")
	bash.ID = "b2"
	m.SetPermissions(bash)
	require.Contains(t, ansi.Strip(m.View()), "1×")
	require.Equal(t, 2, m.selectedOption, "repeats never preselect consent")
}

func TestPreferenceValuesUseInterfaceLanguage(t *testing.T) {
	require.Equal(t, config.Text("oculta", "hidden"), localizedSettingValue("tui.sidebar", "hidden"))
	require.Equal(t, config.Text("não", "no"), localizedSettingValue("preferences.resume", false))
	require.Equal(t, config.Text("Perguntar", "Ask"), localizedSettingValue("runtime.mode", "ask"))
	require.Equal(t, config.Text("automática", "automatic"), localizedSettingValue("tui.proseWidth", 0))
	require.Equal(t, "120", localizedSettingValue("tui.proseWidth", 120))
}
