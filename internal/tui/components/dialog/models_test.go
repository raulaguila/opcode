package dialog

import (
	"context"
	"io"
	"net/http"
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

type catalogTestTransport func(*http.Request) (*http.Response, error)

func (f catalogTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func catalogDialog(t *testing.T) *modelDialogCmp {
	t.Helper()
	if config.Get() == nil {
		root := t.TempDir()
		path := filepath.Join(root, "config.json")
		require.NoError(t, os.WriteFile(path, []byte(`{"providers":{"openai":{"apiKey":"test-key"}}}`), 0600))
		viper.SetConfigFile(path)
		_, err := config.Load(root, false)
		require.NoError(t, err)
	}
	cfg := config.Get()
	old := *cfg
	cfg.Providers = map[models.ModelProvider]config.Provider{models.ProviderLocal: {BaseURL: "http://local.invalid/v1", API: "ollama"}}
	cfg.Agents = map[config.AgentName]config.Agent{config.AgentCoder: {Model: models.GPT41}}
	cfg.Models = nil
	previous := http.DefaultTransport
	http.DefaultTransport = catalogTestTransport(func(req *http.Request) (*http.Response, error) {
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		status, body := 404, `{}`
		if req.URL.Path == "/v1/models" {
			status, body = 200, `{"data":[{"id":"llama3.1:8b"},{"id":"qwen:7b"}]}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	m := NewModelDialogCmp().(*modelDialogCmp)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	t.Cleanup(func() { m.Close(); *cfg = old; http.DefaultTransport = previous })
	return m
}
func TestModelDialogDiscoversSearchesAndSelectsWithoutPublishingRegistry(t *testing.T) {
	m := catalogDialog(t)
	cmd := m.Open()
	require.NotNil(t, cmd)
	require.Contains(t, m.View(), "Loading")
	require.Nil(t, m.choose(), "an empty list must never index/panic")
	m.Update(cmd())
	require.Len(t, m.filtered, 2)
	require.NotContains(t, models.SupportedModels, models.ModelID("local.qwen:7b"))
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("qwen")})
	require.Len(t, m.filtered, 1)
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	selected := cmd().(ModelSelectedMsg).Model
	require.Equal(t, "qwen:7b", selected.APIModel)
	require.Equal(t, "ollama", selected.API)
}
func TestModelDialogIgnoresStaleResponsesAndCancelsWhenClosed(t *testing.T) {
	m := catalogDialog(t)
	first := m.Open()
	stale := first()
	refresh := m.load()
	m.Update(stale)
	require.True(t, m.loading)
	require.Empty(t, m.filtered)
	m.Update(refresh())
	require.False(t, m.loading)
	require.Len(t, m.filtered, 2)
	m.Close()
	m.models = nil
	m.filtered = nil
	m.Update(stale)
	require.Empty(t, m.filtered)
	cancelled := m.Open()
	m.Close()
	result := cancelled().(ModelCatalogLoadedMsg)
	require.Error(t, result.err)
	require.ErrorIs(t, result.err, context.Canceled)
}
func TestModelDialogMouseSelectionFilteringAndSmallLayout(t *testing.T) {
	m := catalogDialog(t)
	m.Update(m.Open()())
	_, cmd := m.Update(tea.MouseMsg{X: 4, Y: 6, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	require.NotNil(t, cmd)
	require.Equal(t, "qwen:7b", cmd().(ModelSelectedMsg).Model.APIModel)
	m.Update(m.Open()()) // Selecting closes the dialog; reopen before editing its search.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("missing")})
	require.Contains(t, m.View(), "No models found")
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.Nil(t, cmd)
	m.Update(tea.WindowSizeMsg{Width: 36, Height: 16})
	require.LessOrEqual(t, lipgloss.Width(m.View()), 36)
	require.LessOrEqual(t, lipgloss.Height(m.View()), 16)
	config.Get().Providers = map[models.ModelProvider]config.Provider{}
	require.Nil(t, m.Open())
	require.Contains(t, m.View(), "No provider")
	m.move(1)
	require.Nil(t, m.choose())
}

func TestModelSearchPasteCannotExpandTheDialogOrExceedLimit(t *testing.T) {
	m := catalogDialog(t)
	m.Update(m.Open()())
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("llama\n\x1b[31m" + strings.Repeat("é", 300))})
	require.NotContains(t, m.query, "\n")
	require.NotContains(t, m.query, "\x1b")
	require.LessOrEqual(t, len(m.query), 256)
	require.LessOrEqual(t, lipgloss.Height(m.View()), m.height)
}
