package dialog

import (
	"io"
	"net/http"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/stretchr/testify/require"
)

func TestGatewayAdvertisedContextAppearsInModelPicker(t *testing.T) {
	m := catalogDialog(t)
	config.Get().ProviderProfiles = map[string]config.ProviderProfile{"office": {Name: "Office gateway", Backend: models.ProviderLocal, Provider: config.Provider{BaseURL: "http://office.invalid/prefix/v1", API: "chat_completions"}}}
	http.DefaultTransport = catalogTestTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"alias","max_model_len":131072,"openai":{"max_model_len":131072}}]}`)), Request: req}, nil
	})
	m.Update(m.Open()())
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	require.NotNil(t, cmd)
	m.Update(cmd())
	require.Contains(t, m.View(), "Office gateway")
	require.Contains(t, m.View(), "128k")
	require.Contains(t, m.View(), "provider metadata")
	require.NotContains(t, m.View(), "fallback estimate")
	require.EqualValues(t, 131072, m.filtered[0].ContextWindow)
}
