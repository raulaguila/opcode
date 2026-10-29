package dialog

import (
	"errors"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestModelCatalogOpensSavedConnectionAndReturnsAfterEditing(t *testing.T) {
	m := catalogDialog(t)
	config.Get().ProviderProfiles = map[string]config.ProviderProfile{"office": {Name: "Office", Backend: models.ProviderLocal, Provider: config.Provider{BaseURL: "http://office.invalid/v1", API: "chat_completions"}}}
	cmd := m.OpenForProvider("profile:office")
	require.NotNil(t, cmd)
	require.Equal(t, models.ModelProvider("profile:office"), m.provider)
	require.Empty(t, m.filtered, "old catalog must not survive a connection switch")
	m.Update(ModelCatalogLoadedMsg{request: m.request, err: errors.New("provider unavailable")})
	require.Contains(t, ansi.Strip(m.View()), "Office")
	require.Contains(t, ansi.Strip(m.View()), "query failed")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("qwen")})
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	open := cmd().(OpenProviderDialogMsg)
	require.Equal(t, models.ModelProvider("profile:office"), open.Provider)
	require.True(t, open.ReturnToModels)
	cmd = m.Resume()
	require.Equal(t, "qwen", m.query)
	require.Equal(t, models.ModelProvider("profile:office"), m.provider)
	require.NotNil(t, cmd)
	m.Close()
}

func TestProviderSaveEventCarriesExactDestination(t *testing.T) {
	m := settingsDialog(t)
	m.edit(nil)
	m.fields[0].SetValue("office")
	m.fields[1].SetValue("Office")
	m.fields[3].SetValue("http://office.invalid/v1")
	msg := m.save()().(ProviderSettingsSavedMsg)
	require.Equal(t, models.ModelProvider("profile:office"), msg.Provider)
	m.OpenForProvider("profile:office")
	require.Equal(t, "provider", m.screen)
	require.Equal(t, "office", m.originalID)
	require.Equal(t, "Office", m.fields[1].Value())
	require.False(t, m.legacy)
	m.OpenForProvider(models.ProviderLocal)
	require.True(t, m.legacy)
	require.Equal(t, "local", m.originalID)
	msg = m.save()().(ProviderSettingsSavedMsg)
	require.Equal(t, models.ProviderLocal, msg.Provider)
}

func TestModelContextSeparatesCatalogAndActiveLimitsWithoutProbing(t *testing.T) {
	m := populatedModelDialog(t)
	m.models[0].ContextWindow = 131072
	m.models[0].ContextSource = "provider"
	m.filter()
	m.SetActiveContext(m.models[0].ID, 32768, "ollama:loaded")
	lines := strings.Join(m.modelDetailLines(), "\n")
	require.Contains(t, lines, "Context: 128k")
	require.Contains(t, lines, "advertised")
	require.Contains(t, lines, "Effective: 32k")
	require.Contains(t, lines, "server allocation")
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	require.Contains(t, strings.Join(m.modelDetailLines(), "\n"), "Effective: available after activation")
	require.NotContains(t, strings.Join(m.modelDetailLines(), "\n"), "32k")
	m.target = config.AgentTitle
	m.selectedIdx = 0
	require.Contains(t, strings.Join(m.modelDetailLines(), "\n"), "Effective: available after activation")
	m.target = config.AgentCoder
	m.SetActiveContext("", 0, "")
	require.NotContains(t, strings.Join(m.modelDetailLines(), "\n"), "32k")
	for _, size := range []tea.WindowSizeMsg{{Width: 36, Height: 12}, {Width: 80, Height: 24}} {
		m.SetActiveContext(m.models[0].ID, 32768, "configuration")
		m.Update(size)
		require.Contains(t, ansi.Strip(m.View()), "Effective:")
	}
}
