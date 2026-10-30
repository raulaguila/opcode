package dialog

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/stretchr/testify/require"
)

func TestProviderDraftResumeRetainsModelIdentityAndCurrentGeometry(t *testing.T) {
	m := settingsDialog(t)
	m.edit(nil)
	m.fields[providerNameField].SetValue("Preserved draft")
	request := m.request
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	require.Same(t, m, model)
	require.Equal(t, "list", m.screen)
	m.Update(tea.WindowSizeMsg{Width: 58, Height: 20})
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	require.Same(t, m, model)
	require.Equal(t, "provider", m.screen)
	require.Equal(t, "Preserved draft", m.fields[providerNameField].Value())
	require.Equal(t, 58, m.width)
	require.Equal(t, 20, m.height)
	require.Greater(t, m.request, request)
}

func TestIntegrationArgumentEditorPreservesDraftAndResumesCurrentGeometry(t *testing.T) {
	_ = settingsDialog(t)
	m := NewIntegrationDialogCmp(nil).(*integrationDialogCmp)
	m.edit(config.Integration{Kind: "mcp", Name: "draft", MCP: config.MCPServer{Type: config.MCPStdio, Command: "fixture", Args: []string{"--old"}}})
	m.fields[m.focus].Blur()
	m.focus = 3
	m.fields[m.focus].Focus()
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.Same(t, m, model)
	require.NotNil(t, m.arguments)
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	require.Nil(t, m.arguments)
	require.Equal(t, `["--old"]`, m.fields[3].Value())
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	require.Equal(t, "list", m.screen)
	m.Update(tea.WindowSizeMsg{Width: 61, Height: 22})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	require.Equal(t, "edit", m.screen)
	require.Equal(t, "fixture", m.fields[2].Value())
	require.Equal(t, 61, m.width)
	require.Equal(t, 22, m.height)
}

func TestProviderMouseAndKeyboardFocusUseTheSameVisibleFields(t *testing.T) {
	m := settingsDialog(t)
	m.edit(nil)
	_, _ = m.Update(tea.MouseMsg{X: 4, Y: 5, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	require.Equal(t, providerURLField, m.focus)
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	require.Equal(t, providerAuthField, m.focus)
	m.fields[providerAuthField].SetValue("environment")
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	require.Equal(t, providerEnvironmentField, m.focus)
	_, _, err := m.readProfile()
	require.ErrorContains(t, err, "environment variable")
	require.Equal(t, "provider", m.screen)
}
