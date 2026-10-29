package dialog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func settingsDialog(t *testing.T) *providerDialogCmp {
	t.Helper()
	_ = catalogDialog(t)
	c := config.Get()
	old := *c
	path := viper.ConfigFileUsed()
	next := filepath.Join(t.TempDir(), "settings.json")
	require.NoError(t, os.WriteFile(next, []byte(`{"future":{"keep":true}}`), 0600))
	viper.SetConfigFile(next)
	c.WorkingDir = t.TempDir()
	c.ProviderProfiles = map[string]config.ProviderProfile{}
	c.Preferences = config.Preferences{}
	c.Runtime = (config.RuntimeConfig{}).Defaults()
	m := NewProviderDialogCmp().(*providerDialogCmp)
	m.Update(tea.WindowSizeMsg{Width: 92, Height: 28})
	m.Open()
	t.Cleanup(func() { m.Close(); *c = old; viper.SetConfigFile(path) })
	return m
}
func TestProviderDialogTestsAndSavesNamedConnectionWithoutCredentials(t *testing.T) {
	m := settingsDialog(t)
	t.Setenv("TEST_CONNECTION_KEY", "resolved-private-key")
	m.edit(nil)
	m.fields[0].SetValue("studio")
	m.fields[1].SetValue("Studio")
	m.fields[3].SetValue("http://local.invalid/v1")
	m.fields[providerAuthField].SetValue("environment")
	m.fields[4].SetValue("TEST_CONNECTION_KEY")
	cmd := m.test()
	require.NotNil(t, cmd)
	require.True(t, m.testing)
	m.Update(cmd())
	require.False(t, m.testing)
	require.Contains(t, m.note, "2 models")
	cmd = m.save()
	require.NotNil(t, cmd)
	require.True(t, cmd().(ProviderSettingsSavedMsg).ProviderChanged)
	require.Equal(t, models.ProviderLocal, config.Get().ProviderProfiles["studio"].Backend)
	data, err := os.ReadFile(viper.ConfigFileUsed())
	require.NoError(t, err)
	require.NotContains(t, string(data), "resolved-private-key")
	require.Contains(t, string(data), "TEST_CONNECTION_KEY")
	require.Contains(t, string(data), "future")
}
func TestProviderDialogCancellationAndLateResultDoNotDiscardDraft(t *testing.T) {
	m := settingsDialog(t)
	m.edit(nil)
	m.fields[3].SetValue("http://local.invalid/v1")
	cmd := m.test()
	require.NotNil(t, cmd)
	require.True(t, m.testing)
	_, closed := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	require.Nil(t, closed)
	require.False(t, m.testing)
	result := cmd().(providerTestMsg)
	require.ErrorIs(t, result.err, context.Canceled)
	m.Update(result)
	require.Contains(t, m.note, "cancelled")
	require.Equal(t, "http://local.invalid/v1", m.fields[3].Value())
	require.Equal(t, "provider", m.screen)
}
func TestProviderDialogInvalidConfigOrBusyGuardPreserveFieldsAndDisk(t *testing.T) {
	m := settingsDialog(t)
	m.edit(nil)
	m.fields[providerAuthField].SetValue("environment")
	m.fields[4].SetValue("NONEXISTENT_OPCODE_TEST_CREDENTIAL")
	t.Setenv("NONEXISTENT_OPCODE_TEST_CREDENTIAL", "")
	require.Nil(t, m.save())
	require.Contains(t, m.note, "not set")
	require.Equal(t, "provider", m.screen)
	m.allowSave = func() error { return errors.New("wait for the current run") }
	require.Nil(t, m.save())
	require.Contains(t, m.note, "wait for")
	data, err := os.ReadFile(viper.ConfigFileUsed())
	require.NoError(t, err)
	require.NotContains(t, string(data), "providerProfiles")
}
func TestProviderPreferencesAreEditableAndBoundedOnSmallScreen(t *testing.T) {
	m := settingsDialog(t)
	m.preferences()
	m.fields[0].SetValue("plan")
	m.fields[1].SetValue("12")
	m.fields[6].SetValue("true")
	m.fields[7].SetValue("false")
	cmd := m.save()
	require.NotNil(t, cmd)
	require.False(t, cmd().(ProviderSettingsSavedMsg).ProviderChanged)
	require.Equal(t, "plan", config.Get().Runtime.Mode)
	require.Equal(t, 12, config.Get().Runtime.MaxSteps)
	require.True(t, config.Get().Preferences.ResumeLast)
	require.False(t, config.Get().Preferences.ProcessVisible())
	m.Update(tea.WindowSizeMsg{Width: 36, Height: 16})
	m.preferences()
	for range m.fields {
		m.Update(tea.KeyMsg{Type: tea.KeyTab})
		require.LessOrEqual(t, lipgloss.Width(m.View()), 36)
		require.LessOrEqual(t, lipgloss.Height(m.View()), 16)
	}
	require.NotContains(t, strings.ToLower(m.View()), "resolved-private-key")
}
func TestModelPickerShowsNamedProfilesFavoritesAndIndependentRoles(t *testing.T) {
	m := catalogDialog(t)
	c := config.Get()
	c.ProviderProfiles = map[string]config.ProviderProfile{"studio": {Name: "Studio", Backend: models.ProviderLocal, Provider: config.Provider{BaseURL: "http://local.invalid/v1", API: "ollama"}}}
	c.Preferences = config.Preferences{FavoriteModels: []models.ModelID{"profile.studio.qwen:7b"}}
	model := models.Model{ID: "profile.studio.active", Provider: models.ProviderLocal, ProviderProfile: "studio", APIModel: "active", ContextWindow: 16384}
	models.SupportedModels[model.ID] = model
	t.Cleanup(func() { delete(models.SupportedModels, model.ID) })
	c.Agents[config.AgentCoder] = config.Agent{Model: model.ID}
	cmd := m.Open()
	require.Equal(t, models.ModelProvider("profile:studio"), m.provider)
	m.Update(cmd())
	require.Equal(t, "qwen:7b", m.filtered[0].APIModel)
	require.Contains(t, m.View(), "Studio")
	require.Contains(t, m.View(), "fallback estimate")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	require.Contains(t, m.View(), "fallback estimate")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	require.Equal(t, config.AgentTask, m.target)
	selected := m.choose()().(ModelSelectedMsg)
	require.Equal(t, config.AgentTask, selected.Agent)
	require.NotEmpty(t, selected.Model.ProviderProfile)
}
