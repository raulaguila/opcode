package dialog

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestCompatiblePresetTLSAndUniqueIdentity(t *testing.T) {
	m := settingsDialog(t)
	config.Get().ProviderProfiles["openai-compatible"] = config.ProviderProfile{Name: "Existing", Backend: models.ProviderLocal}
	m.screen = "presets"
	for i, p := range config.ProviderPresets {
		if p.ID == "openai-compatible" {
			m.selected = i
		}
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, "openai-compatible-2", m.fields[0].Value())
	require.Equal(t, "chat_completions", m.fields[5].Value())
	require.Equal(t, "false", m.fields[9].Value())
	m.fields[providerAuthField].SetValue("none")
	m.fields[3].SetValue("https://gateway.invalid/v1")
	m.fields[9].SetValue("true")
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	require.Contains(t, m.View(), "TLS")
	require.LessOrEqual(t, lipgloss.Height(m.View()), 18)
	id, p, err := m.readProfile()
	require.NoError(t, err)
	require.True(t, p.TLSInsecure)
	require.NoError(t, config.SaveProviderProfile(id, p))
	raw, err := os.ReadFile(viper.ConfigFileUsed())
	require.NoError(t, err)
	require.True(t, strings.Contains(string(raw), `"tlsInsecure": true`) || strings.Contains(string(raw), `"tlsInsecure":true`))
	require.Equal(t, "Existing", config.Get().ProviderProfiles["openai-compatible"].Name)
}
