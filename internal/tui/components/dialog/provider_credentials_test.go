package dialog

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestProviderDirectKeyIsMaskedUsedByCatalogAndSavedPrivately(t *testing.T) {
	m := settingsDialog(t)
	m.edit(nil)
	m.fields[0].SetValue("direct")
	m.fields[5].SetValue("chat_completions")
	m.fields[3].SetValue("http://gateway.invalid/v1")
	m.fields[providerAuthField].SetValue("api_key")
	m.fields[4].SetValue("UNUSED_CREDENTIAL_VARIABLE")
	const secret = "fake-direct-key-for-ui-regression"
	m.fields[m.focus].Blur()
	m.focus = providerKeyField
	m.fields[m.focus].Focus()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(secret), Paste: true})
	require.Equal(t, textinput.EchoPassword, m.fields[providerKeyField].EchoMode)
	require.NotContains(t, m.View(), secret)
	require.Contains(t, m.View(), "•")
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	requests := 0
	http.DefaultTransport = catalogTestTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		require.Equal(t, "Bearer "+secret, r.Header.Get("Authorization"))
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"fixture"}]}`)), Request: r}, nil
	})
	cmd := m.test()
	require.NotNil(t, cmd)
	m.Update(cmd())
	require.Greater(t, requests, 0)
	require.Contains(t, m.note, "1 models")
	require.NotContains(t, m.View(), secret)
	cmd = m.save()
	require.NotNil(t, cmd)
	raw, err := os.ReadFile(viper.ConfigFileUsed())
	require.NoError(t, err)
	var stored config.Config
	require.NoError(t, json.Unmarshal(raw, &stored))
	require.Equal(t, secret, stored.ProviderProfiles["direct"].APIKey)
	require.Empty(t, stored.ProviderProfiles["direct"].APIKeyEnv)
	info, err := os.Stat(viper.ConfigFileUsed())
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	redacted, err := json.Marshal(config.RedactedDocument())
	require.NoError(t, err)
	require.NotContains(t, string(redacted), secret)
	m.edit(&providerRow{id: "direct"})
	require.Equal(t, "api_key", m.fields[providerAuthField].Value())
	require.Equal(t, secret, m.fields[providerKeyField].Value())
	require.False(t, m.dirty())
	require.NotContains(t, m.View(), secret)
}

func TestProviderCredentialModeSwitchNeverCopiesEnvironmentSecret(t *testing.T) {
	m := settingsDialog(t)
	t.Setenv("UI_CREDENTIAL_TEST", "fake-resolved-secret-for-ui-test")
	m.edit(nil)
	m.fields[0].SetValue("switch")
	m.fields[providerAuthField].SetValue("environment")
	m.fields[4].SetValue("UI_CREDENTIAL_TEST")
	require.NotNil(t, m.save())
	m.edit(&providerRow{id: "switch"})
	require.Equal(t, "environment", m.fields[providerAuthField].Value())
	require.Empty(t, m.fields[providerKeyField].Value())
	require.NotContains(t, m.View(), "fake-resolved-secret-for-ui-test")
	m.fields[m.focus].Blur()
	m.focus = providerAuthField
	m.fields[m.focus].Focus()
	// environment -> vault -> none -> api_key uses the real typed selector.
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	require.Equal(t, "api_key", m.fields[providerAuthField].Value())
	require.Nil(t, m.save(), "direct mode must require an explicit key")
	m.fields[providerKeyField].SetValue("fake-new-direct-credential")
	require.NotNil(t, m.save())
	m.edit(&providerRow{id: "switch"})
	_, profile, err := m.readProfile()
	require.NoError(t, err)
	require.Equal(t, "fake-new-direct-credential", profile.APIKey)
	require.Empty(t, profile.APIKeyEnv)
	m.fields[providerAuthField].SetValue("environment")
	m.fields[4].SetValue("UI_CREDENTIAL_TEST")
	require.NotNil(t, m.save())
	raw, err := os.ReadFile(viper.ConfigFileUsed())
	require.NoError(t, err)
	require.NotContains(t, string(raw), "fake-new-direct-credential")
	require.NotContains(t, string(raw), "fake-resolved-secret-for-ui-test")
	m.edit(&providerRow{id: "switch"})
	m.fields[providerAuthField].SetValue("none")
	require.NotNil(t, m.save())
	require.Empty(t, config.Get().ProviderProfiles["switch"].APIKey)
	require.Empty(t, config.Get().ProviderProfiles["switch"].APIKeyEnv)
}

func TestLegacyDirectKeySurvivesEditAndSupportsRotation(t *testing.T) {
	m := settingsDialog(t)
	secret := "fake-legacy-" + strings.Repeat("x", 2048)
	require.NoError(t, config.SaveProvider(models.ProviderLocal, config.Provider{BaseURL: "http://local.invalid/v1", API: "chat_completions", APIKey: secret}))
	m.edit(&providerRow{id: "local", legacy: true})
	require.Equal(t, secret, m.fields[providerKeyField].Value(), "existing long keys must not be silently truncated")
	require.NotNil(t, m.save())
	require.Equal(t, secret, config.StoredProviderKey("local", true))
	m.fields[providerKeyField].SetValue("fake-rotated-legacy-key")
	require.NotNil(t, m.save())
	require.Equal(t, "fake-rotated-legacy-key", config.StoredProviderKey("local", true))
}

func TestProviderCredentialModesFitCompactTerminalsAndKeepDraftOnEscape(t *testing.T) {
	m := settingsDialog(t)
	for _, lang := range []string{"en", "pt"} {
		config.Get().TUI.Language = lang
		for _, size := range []tea.WindowSizeMsg{{Width: 36, Height: 16}, {Width: 60, Height: 20}, {Width: 100, Height: 30}} {
			m.Update(size)
			m.edit(nil)
			m.fields[providerKeyField].SetValue("fake-private-unsaved-key")
			m.fields[4].SetValue("UI_VARIABLE")
			m.fields[9].SetValue("true")
			for _, mode := range []string{"none", "api_key", "environment", "vault"} {
				m.fields[providerAuthField].SetValue(mode)
				for _, advanced := range []bool{false, true} {
					m.advanced = advanced
					for _, i := range m.fieldIndices() {
						m.fields[m.focus].Blur()
						m.focus = i
						m.fields[i].Focus()
						view := m.View()
						require.LessOrEqual(t, lipgloss.Width(view), size.Width)
						require.LessOrEqual(t, lipgloss.Height(view), size.Height)
						require.NotContains(t, view, "fake-private-unsaved-key")
					}
				}
			}
		}
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	require.Equal(t, "fake-private-unsaved-key", m.fields[providerKeyField].Value())
	raw, err := os.ReadFile(viper.ConfigFileUsed())
	require.NoError(t, err)
	require.NotContains(t, string(raw), "fake-private-unsaved-key")
}
