package dialog

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/tui/util"
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
	require.Contains(t, m.View(), "Network: check server and connection")
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

func TestCatalogFailuresHaveLocalizedActionsAndSanitizedDetails(t *testing.T) {
	for _, backend := range []models.ModelProvider{models.ProviderLocal, models.ProviderOpenAI} {
		for _, tc := range []struct {
			name, en, pt string
			status       int
			cause        error
		}{
			{"authentication", "Authentication: check key", "Autenticação: reveja chave", 401, nil},
			{"forbidden", "Authentication: check key", "Autenticação: reveja chave", 403, nil},
			{"TLS", "TLS: check certificate", "TLS: confira certificado", 0, x509.UnknownAuthorityError{}},
			{"network", "Network: check server", "Rede: confira servidor", 0, errors.New("secret-sentinel")},
			{"timeout", "Timeout: retry", "Tempo esgotado: tente", 0, context.DeadlineExceeded},
			{"format", "Format: check catalog", "Formato: confira catálogo", 200, nil},
		} {
			for _, language := range []string{"en", "pt"} {
				t.Run(string(backend)+"/"+tc.name+"/"+language, func(t *testing.T) {
					m := catalogDialog(t)
					config.Get().TUI.Language = language
					config.Get().ProviderProfiles = map[string]config.ProviderProfile{"office": {Name: "Office", Backend: backend, Provider: config.Provider{BaseURL: "https://private-sentinel.invalid/prefix/v1", APIKey: "secret-sentinel"}}}
					http.DefaultTransport = catalogTestTransport(func(req *http.Request) (*http.Response, error) {
						if tc.cause != nil {
							return nil, &url.Error{Op: "Get", URL: "https://private-sentinel.invalid/?key=secret-sentinel", Err: tc.cause}
						}
						return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("secret-sentinel")), Request: req}, nil
					})
					active := config.Get().Agents[config.AgentCoder]
					result := m.OpenForProvider("profile:office")().(ModelCatalogLoadedMsg)
					require.Error(t, result.err)
					require.NotContains(t, result.err.Error(), "sentinel")
					_, cmd := m.Update(result)
					require.NotNil(t, cmd)
					info := cmd().(util.InfoMsg)
					want := map[string]string{"en": tc.en, "pt": tc.pt}[language]
					require.Contains(t, m.View(), want)
					require.Contains(t, info.Details, want)
					require.NotContains(t, m.View()+info.Msg+info.Details, "sentinel")
					require.Equal(t, models.ModelProvider("profile:office"), m.provider)
					require.Empty(t, m.filtered)
					require.Nil(t, m.choose())
					require.Equal(t, active, config.Get().Agents[config.AgentCoder])
					provider := NewProviderDialogCmp().(*providerDialogCmp)
					provider.finishCatalogTest(providerTestMsg{err: result.err})
					require.Equal(t, m.note, provider.note)
				})
			}
		}
	}
}
