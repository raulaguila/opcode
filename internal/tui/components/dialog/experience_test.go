package dialog

import (
	"context"
	"fmt"
	"github.com/charmbracelet/bubbles/key"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/stretchr/testify/require"
)

func TestSearchableSettingsEditResetAndAdvancedFilter(t *testing.T) {
	m := settingsDialog(t)
	m.edit(nil)
	m.fields[1].SetValue("Draft connection")
	m.settings()
	m.settingsFilter.SetValue("sidebar")
	require.Len(t, m.filteredSettings(), 1)
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, "setting", m.screen)
	m.fields[0].SetValue("hidden")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	require.NotNil(t, cmd)
	require.Equal(t, "hidden", config.Get().TUI.Sidebar)
	m.settingsFilter.SetValue("sidebar")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	require.Equal(t, "hidden", config.Get().TUI.Sidebar)
	m.settingsFilter.SetValue("maxSteps")
	require.Empty(t, m.filteredSettings())
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	require.Len(t, m.filteredSettings(), 1)
}
func TestSettingsGeometryAtSmallAndLargeTerminalSizes(t *testing.T) {
	m := settingsDialog(t)
	for _, size := range []tea.WindowSizeMsg{{Width: 36, Height: 14}, {Width: 80, Height: 24}, {Width: 160, Height: 40}} {
		m.Update(size)
		for _, screen := range []string{"settings", "effective", "presets"} {
			m.settings()
			m.screen = screen
			view := m.View()
			require.LessOrEqual(t, lipgloss.Width(view), size.Width, "%s", screen)
			require.LessOrEqual(t, lipgloss.Height(view), size.Height, "%s", screen)
		}
	}
}
func TestPermissionReviewModesAndFullscreenDoNotApprove(t *testing.T) {
	m := NewPermissionDialogCmp().(*permissionDialogCmp)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.SetPermissions(permission.PermissionRequest{ID: "review", ToolName: tools.EditToolName, Path: "project", Params: tools.EditPermissionsParams{Diff: "--- a/test.go\n+++ b/test.go\n@@ -1 +1 @@\n-old\n+new\n"}})
	require.Contains(t, m.View(), "-old")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	require.Nil(t, cmd)
	require.Equal(t, "unified", m.diffMode)
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	require.Nil(t, cmd)
	require.True(t, m.fullscreen)
	for _, size := range []tea.WindowSizeMsg{{Width: 36, Height: 18}, {Width: 80, Height: 24}, {Width: 160, Height: 40}} {
		m.Update(size)
		require.LessOrEqual(t, lipgloss.Width(m.View()), size.Width)
		require.LessOrEqual(t, lipgloss.Height(m.View()), size.Height)
	}
}
func TestReviewDiffKeepsMultipleFilesAndStripsTerminalControls(t *testing.T) {
	raw := "--- a/one.go\n+++ b/one.go\n@@ -1 +1 @@\n-old\n+new\n--- a/two.go\n+++ b/two.go\n@@ -1 +1 @@\n-a\n+b\x1b[2J\n"
	for _, mode := range []string{"auto", "unified", "split"} {
		view := reviewDiff(raw, 140, mode)
		require.Contains(t, view, "one.go")
		require.Contains(t, view, "two.go")
		require.False(t, strings.Contains(view, "\x1b[2J"))
	}
}

func TestPreferencesAndPermissionControlsUseSelectedLocale(t *testing.T) {
	m := settingsDialog(t)
	config.Get().TUI.Language = "pt"
	m.settings()
	require.Contains(t, m.View(), "Preferências")
	require.NotContains(t, m.View(), "Notifications")
	m.edit(nil)
	require.Contains(t, m.View(), "Configurar provedor")
	require.Contains(t, m.View(), "URL da API")
	require.NotContains(t, m.View(), "ID da conexão")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	require.Contains(t, m.View(), "ID da conexão")
	require.NotContains(t, m.View(), "API key environment variable")
	permissionUI := NewPermissionDialogCmp().(*permissionDialogCmp)
	permissionUI.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	permissionUI.SetPermissions(permission.PermissionRequest{ID: "pt", ToolName: tools.BashToolName, Params: tools.BashPermissionsParams{Command: "echo hello"}})
	require.Contains(t, permissionUI.View(), "Permitir (a)")
	require.Contains(t, permissionUI.View(), "Recusar (d)")
	config.Get().TUI.Language = "en"
	require.Contains(t, permissionUI.View(), "Allow (a)")
}
func TestHelpCanScrollAllBindingsOnSmallTerminal(t *testing.T) {
	m := NewHelpCmp().(*helpCmp)
	m.Update(tea.WindowSizeMsg{Width: 36, Height: 14})
	bindings := []key.Binding{}
	for i := range 30 {
		bindings = append(bindings, key.NewBinding(key.WithKeys(fmt.Sprintf("alt+%d", i)), key.WithHelp(fmt.Sprintf("alt+%d", i), fmt.Sprintf("action %d", i))))
	}
	m.SetBindings(bindings)
	require.LessOrEqual(t, lipgloss.Width(m.View()), 36)
	require.LessOrEqual(t, lipgloss.Height(m.View()), 14)
	m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	require.Contains(t, m.View(), "action 29")
}

func TestModelProbeRequiresExplicitSecondConfirmationAndRejectsStaleResults(t *testing.T) {
	m := catalogDialog(t)
	cmd := m.Open()
	m.Update(cmd())
	require.NotEmpty(t, m.filtered)
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	require.Nil(t, cmd)
	require.True(t, m.probeConfirm)
	require.Contains(t, m.View(), "Provider charges may apply")
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	require.Nil(t, cmd)
	require.False(t, m.probeConfirm)
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	require.True(t, m.probeRunning)
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	note := m.note
	result := cmd().(ModelProbeResultMsg)
	require.ErrorIs(t, result.err, context.Canceled)
	m.Update(result)
	require.Equal(t, note, m.note)
	require.False(t, m.probeRunning)
}
func TestOAuthFormPreservesRegisteredConfigurationAndBoundsAuthenticationView(t *testing.T) {
	_ = settingsDialog(t)
	original := config.Integration{Kind: "mcp", Name: "remote", MCP: config.MCPServer{Type: config.MCPHTTP, URL: "https://mcp.example/mcp", OAuth: &config.MCPOAuthConfig{ClientID: "public-client", Scopes: []string{"tools:read"}}}}
	m := NewIntegrationDialogCmp(nil).(*integrationDialogCmp)
	m.edit(original)
	candidate, err := m.read()
	require.NoError(t, err)
	require.Equal(t, original.MCP.OAuth, candidate.MCP.OAuth)
	m.fields[11].SetValue("tools:write")
	candidate, err = m.read()
	require.NoError(t, err)
	require.Equal(t, []string{"tools:read"}, original.MCP.OAuth.Scopes)
	require.Equal(t, []string{"tools:write"}, candidate.MCP.OAuth.Scopes)
	m.screen = "oauth"
	m.result = []string{"https://auth.example/?state=" + strings.Repeat("x", 2000) + "\x1b[2J"}
	m.Update(tea.WindowSizeMsg{Width: 36, Height: 14})
	view := m.View()
	require.LessOrEqual(t, lipgloss.Width(view), 36)
	require.LessOrEqual(t, lipgloss.Height(view), 14)
	require.NotContains(t, view, "\x1b[2J")
}
