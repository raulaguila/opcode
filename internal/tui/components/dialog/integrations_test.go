package dialog

import (
	"context"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestIntegrationDialogTestsCandidateListsMethodsAndPreservesDraft(t *testing.T) {
	_ = settingsDialog(t)
	var tested config.Integration
	m := NewIntegrationDialogCmp(func(ctx context.Context, c config.Integration) ([]string, error) {
		tested = c
		return []string{"Connected", "Tool: search", "Resource: docs://intro"}, nil
	}).(*integrationDialogCmp)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Open()
	m.edit(config.Integration{Kind: "mcp"})
	m.fields[0].SetValue("docs")
	m.fields[2].SetValue("mcp-docs")
	m.fields[3].SetValue(`["--stdio"]`)
	m.fields[5].SetValue(`{"Authorization":"DOCS_AUTH"}`)
	cmd := m.testIntegration()
	require.NotNil(t, cmd)
	m.Update(cmd())
	require.Equal(t, "result", m.screen)
	require.Equal(t, "docs", tested.Name)
	require.Equal(t, []string{"--stdio"}, tested.MCP.Args)
	require.Contains(t, m.View(), "search")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	require.Equal(t, "edit", m.screen)
	require.Equal(t, "mcp-docs", m.fields[2].Value())
	cmd = m.saveIntegration()
	require.NotNil(t, cmd)
	require.IsType(t, IntegrationSettingsSavedMsg{}, cmd())
	data, err := os.ReadFile(viper.ConfigFileUsed())
	require.NoError(t, err)
	require.Contains(t, string(data), "DOCS_AUTH")
	require.Equal(t, "DOCS_AUTH", config.Get().MCPServers["docs"].HeadersEnv["Authorization"])
}
func TestIntegrationDialogCancellationAndInvalidArgumentsKeepEdits(t *testing.T) {
	_ = settingsDialog(t)
	m := NewIntegrationDialogCmp(func(ctx context.Context, _ config.Integration) ([]string, error) { <-ctx.Done(); return nil, ctx.Err() }).(*integrationDialogCmp)
	m.Open()
	m.edit(config.Integration{Kind: "lsp"})
	m.fields[0].SetValue("go")
	m.fields[1].SetValue("gopls")
	m.fields[2].SetValue(`not json`)
	require.Nil(t, m.testIntegration())
	require.Contains(t, m.note, "JSON")
	m.fields[2].SetValue(`[]`)
	cmd := m.testIntegration()
	require.NotNil(t, cmd)
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m.Update(cmd())
	require.Contains(t, m.note, "cancelled")
	require.Equal(t, "gopls", m.fields[1].Value())
	m.Update(tea.WindowSizeMsg{Width: 36, Height: 14})
	for range m.fields {
		m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		require.LessOrEqual(t, lipgloss.Width(m.View()), 36)
		require.LessOrEqual(t, lipgloss.Height(m.View()), 14)
	}
}
func TestIntegrationResultCannotInjectTerminalControls(t *testing.T) {
	_ = settingsDialog(t)
	m := NewIntegrationDialogCmp(nil).(*integrationDialogCmp)
	m.Open()
	m.screen = "result"
	m.result = []string{"server\x1b[2J\nname"}
	view := m.View()
	// Theme formatting may use ANSI; the server's erase-screen sequence must not survive.
	require.False(t, strings.Contains(view, "\x1b[2J"))
}

func TestReadingIntegrationCandidateDoesNotMutateRegisteredMapsOrSlices(t *testing.T) {
	_ = settingsDialog(t)
	original := config.Integration{Kind: "mcp", Name: "docs", MCP: config.MCPServer{Type: config.MCPStdio, Command: "docs-server", Args: []string{"old"}, HeadersEnv: map[string]string{"Authorization": "OLD_AUTH"}, EnvVars: map[string]string{"CUSTOM": "OLD_ENV"}}}
	m := NewIntegrationDialogCmp(nil).(*integrationDialogCmp)
	m.edit(original)
	m.fields[3].SetValue(`["new"]`)
	m.fields[5].SetValue(`{"Authorization":"NEW_AUTH"}`)
	m.fields[6].SetValue(`{"CUSTOM":"NEW_ENV"}`)
	candidate, err := m.read()
	require.NoError(t, err)
	require.Equal(t, "NEW_AUTH", candidate.MCP.HeadersEnv["Authorization"])
	require.Equal(t, "OLD_AUTH", original.MCP.HeadersEnv["Authorization"])
	require.Equal(t, "OLD_ENV", original.MCP.EnvVars["CUSTOM"])
	require.Equal(t, []string{"old"}, original.MCP.Args)
}
