package dialog

import (
	"io"
	"net/http"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/stretchr/testify/require"
)

func TestProviderNavigationLoadsDistinctCatalogsAndRejectsLateResults(t *testing.T) {
	for _, keys := range []struct {
		name           string
		next, previous tea.KeyMsg
	}{
		{"alt arrows", tea.KeyMsg{Type: tea.KeyRight, Alt: true}, tea.KeyMsg{Type: tea.KeyLeft, Alt: true}},
		{"tab fallback", tea.KeyMsg{Type: tea.KeyTab}, tea.KeyMsg{Type: tea.KeyShiftTab}},
	} {
		t.Run(keys.name, func(t *testing.T) {
			m := catalogDialog(t)
			cfg := config.Get()
			cfg.ProviderProfiles = map[string]config.ProviderProfile{
				"office":   {Name: "Office", Backend: models.ProviderLocal, Provider: config.Provider{BaseURL: "http://office.invalid/v1", API: "chat_completions"}},
				"disabled": {Backend: models.ProviderLocal, Provider: config.Provider{Disabled: true}},
			}
			active := cfg.Agents[config.AgentCoder]
			http.DefaultTransport = catalogTestTransport(func(req *http.Request) (*http.Response, error) {
				status, body := 404, `{}`
				if req.URL.Path == "/v1/models" {
					status = 200
					if req.URL.Host == "office.invalid" {
						body = `{"data":[{"id":"office-model"}]}`
					} else {
						require.Equal(t, "local.invalid", req.URL.Host)
						body = `{"data":[{"id":"local-model"}]}`
					}
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})
			m.Update(m.Open()())
			require.Equal(t, models.ProviderLocal, m.provider)
			require.Len(t, m.availableProviders, 2)
			m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("local")})
			_, cmd := m.Update(keys.next)
			require.Equal(t, models.ModelProvider("profile:office"), m.provider)
			require.Empty(t, m.search.Value(), "previous provider's filter must not hide the new catalog")
			require.NotNil(t, cmd)
			m.Update(cmd())
			require.Len(t, m.filtered, 1)
			require.Equal(t, "office-model", m.filtered[0].APIModel)
			require.Contains(t, m.View(), "Office")
			require.NotContains(t, m.View(), "local-model")
			late := m.load()()
			_, cmd = m.Update(keys.previous)
			require.NotNil(t, cmd)
			m.Update(cmd())
			m.Update(late)
			require.Equal(t, models.ProviderLocal, m.provider)
			require.Len(t, m.filtered, 1)
			require.Equal(t, "local-model", m.filtered[0].APIModel)
			require.NotContains(t, m.View(), "office-model")
			require.Equal(t, active, cfg.Agents[config.AgentCoder], "browsing must not select or persist a model")
		})
	}
}

func TestProviderNavigationExplainsWhenOnlyOneConnectionIsEnabled(t *testing.T) {
	m := catalogDialog(t)
	config.Get().ProviderProfiles = nil
	m.Update(m.Open()())
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight, Alt: true})
	require.Nil(t, cmd)
	require.Contains(t, m.note, "only enabled provider")
	require.Contains(t, m.note, "Ctrl+P")
	require.Equal(t, models.ProviderLocal, m.provider)
	require.Len(t, m.filtered, 2)
}
