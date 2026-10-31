package dialog

import (
	"errors"
	"net/http"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/stretchr/testify/require"
)

func TestUnreachableProviderSwitchesImmediatelyToEmptyFailedList(t *testing.T) {
	m := catalogDialog(t)
	config.Get().ProviderProfiles = map[string]config.ProviderProfile{"office": {Name: "Office offline", Backend: models.ProviderLocal, Provider: config.Provider{BaseURL: "http://offline.invalid/v1"}}}
	m.Update(m.Open()())
	active := config.Get().Agents[config.AgentCoder]
	require.NotEmpty(t, m.filtered)
	http.DefaultTransport = catalogTestTransport(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline fixture") })
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	require.NotNil(t, cmd)
	require.Equal(t, models.ModelProvider("profile:office"), m.provider)
	require.Empty(t, m.filtered, "no inherited or previous provider models while loading")
	require.Contains(t, m.View(), "Office offline")
	m.Update(cmd())
	require.True(t, m.catalogFailed)
	require.Empty(t, m.filtered)
	require.Contains(t, m.View(), "Provider query failed")
	require.Contains(t, m.View(), "Empty list: query failed")
	require.Nil(t, m.choose(), "a failed list cannot choose a stale model")
	require.Equal(t, active, config.Get().Agents[config.AgentCoder])
	// Retry remains empty until a new successful response, then clears the error.
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	require.NotNil(t, cmd)
	require.False(t, m.catalogFailed)
	m.Update(ModelCatalogLoadedMsg{request: m.request, models: []models.Model{{ID: "profile.office.fresh", APIModel: "fresh"}}})
	require.Len(t, m.filtered, 1)
	require.False(t, m.catalogFailed)
	require.NotContains(t, m.View(), "query failed")
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	m.Update(cmd())
	require.Empty(t, m.filtered, "failure during refresh also discards the previous list")
	require.True(t, m.catalogFailed)
}
