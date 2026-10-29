package dialog

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	"os"
	"strings"
	"testing"
)

func TestGenericPermissionShowsTargetAndRedactsSecretsWithoutChangingRequest(t *testing.T) {
	settingsDialog(t)
	t.Setenv("OPCODE_TEST_SECRET", "private-permission-secret")
	params := map[string]any{"command": "/bin/server", "args": []string{"--language", "go"}, "token": "private-permission-secret"}
	request := permission.PermissionRequest{ID: "p", ToolName: "test_lsp_connection", Path: "/workspace", Description: "Start server", Params: params}
	m := NewPermissionDialogCmp().(*permissionDialogCmp)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 36})
	m.SetPermissions(request)
	view := ansi.Strip(m.View())
	for _, want := range []string{"/bin/server", "--language", "/workspace", "Action parameters"} {
		require.Contains(t, view, want)
	}
	require.NotContains(t, view, "private-permission-secret")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	response := cmd().(PermissionResponseMsg)
	require.Equal(t, PermissionAllowForSession, response.Action)
	require.Equal(t, request, response.Permission, "rendering must not change the approved operation")
}
func TestChoiceFieldsAndArgumentsPreserveUnsavedConfiguration(t *testing.T) {
	m := settingsDialog(t)
	m.edit(nil)
	m.advanced = true
	m.focus = 8
	before, err := os.ReadFile(viper.ConfigFileUsed())
	require.NoError(t, err)
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	require.Equal(t, "true", m.fields[8].Value())
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("invalid")})
	require.Equal(t, "true", m.fields[8].Value())
	require.Contains(t, ansi.Strip(m.View()), "‹ On ›")
	after, err := os.ReadFile(viper.ConfigFileUsed())
	require.NoError(t, err)
	require.Equal(t, before, after)
	args, err := newArgumentList(`["--name","two words","$(not a shell)"]`)
	require.NoError(t, err)
	args.selected = 1
	args.focus()
	args.fields[1].SetValue("ação 漢字")
	require.JSONEq(t, `["--name","ação 漢字","$(not a shell)"]`, args.value())
	done, save, _ := args.update(tea.KeyMsg{Type: tea.KeyEsc})
	require.True(t, done)
	require.False(t, save)
	args.update(tea.KeyMsg{Type: tea.KeyCtrlN})
	require.Len(t, args.fields, 4)
	args.update(tea.KeyMsg{Type: tea.KeyBackspace, Alt: true})
	require.Len(t, args.fields, 3)
}
func TestThemePreviewDoesNotWriteConfigurationAndEscRestores(t *testing.T) {
	settingsDialog(t)
	original := theme.CurrentThemeName()
	t.Cleanup(func() { _ = theme.PreviewTheme(original) })
	before, err := os.ReadFile(viper.ConfigFileUsed())
	require.NoError(t, err)
	m := NewThemeDialogCmp().(*themeDialogCmp)
	m.Init()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	require.NotNil(t, cmd)
	changed := cmd().(ThemeChangedMsg)
	require.True(t, changed.Preview)
	require.NotEqual(t, original, changed.ThemeName)
	after, err := os.ReadFile(viper.ConfigFileUsed())
	require.NoError(t, err)
	require.Equal(t, before, after)
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	restore := cmd().(CloseThemeDialogMsg)
	require.Equal(t, original, restore.RestoreTheme)
}
func TestArgumentEditorPreservesExistingLongValuesAndResetsOnReopen(t *testing.T) {
	settingsDialog(t)
	value := strings.Repeat("x", 5000)
	args, err := newArgumentList(`["` + value + `"]`)
	require.NoError(t, err)
	require.Equal(t, value, args.fields[0].Value())
	m := NewIntegrationDialogCmp(nil).(*integrationDialogCmp)
	m.arguments = args
	m.Open()
	require.Nil(t, m.arguments)
	require.Equal(t, "list", m.screen)
}
func TestControlsHaveOneLanguage(t *testing.T) {
	settingsDialog(t)
	for _, lang := range []string{"pt", "en"} {
		config.Get().TUI.Language = lang
		m := NewProviderDialogCmp().(*providerDialogCmp)
		m.edit(nil)
		m.advanced = true
		view := ansi.Strip(m.View())
		if lang == "pt" {
			require.Contains(t, view, "Configurar provedor")
			require.NotContains(t, view, "Configure provider")
		} else {
			require.Contains(t, view, "Configure provider")
		}
		require.False(t, strings.Contains(view, "Disabled (true/false)"))
	}
}
