package dialog

import (
	"errors"
	"os"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestDeleteProviderRequiresExplicitConfirmationAndHonorsBusyGuard(t *testing.T) {
	m := settingsDialog(t)
	require.NoError(t, config.SaveProviderProfile("office", config.ProviderProfile{Name: "Office", Backend: models.ProviderLocal, BaseURL: "http://office.invalid/v1", APIKey: "fake-private-key"}))
	m.Open()
	m.Update(tea.KeyPressMsg{Text: "d"})
	require.Equal(t, "delete-provider", m.screen)
	require.Contains(t, m.View(), "Office")
	require.NotContains(t, m.View(), "fake-private-key")
	for _, lang := range []string{"en", "pt"} {
		config.Get().TUI.Language = lang
		m.Update(tea.WindowSizeMsg{Width: 36, Height: 16})
		require.LessOrEqual(t, lipgloss.Width(m.View()), 36)
		require.LessOrEqual(t, lipgloss.Height(m.View()), 16)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // Cancel is the initial selection.
	require.Contains(t, config.Get().ProviderProfiles, "office")
	require.Equal(t, "list", m.screen)
	m.startDelete()
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.allowSave = func() error { return errors.New("agent is busy") }
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Contains(t, m.note, "agent is busy")
	require.Contains(t, config.Get().ProviderProfiles, "office")
	m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	require.True(t, m.noteFull)
	require.Contains(t, m.View(), config.Text("Detalhes", "Details"))
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	require.False(t, m.noteFull)
	require.Equal(t, "delete-provider", m.screen)
	m.allowSave = nil
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd)
	require.True(t, cmd().(ProviderSettingsSavedMsg).Deleted)
	require.NotContains(t, config.Get().ProviderProfiles, "office")
	raw, err := os.ReadFile(viper.ConfigFileUsed())
	require.NoError(t, err)
	require.NotContains(t, string(raw), "fake-private-key")
	require.Equal(t, "list", m.screen)
}
