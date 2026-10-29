package dialog

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/stretchr/testify/require"
)

func TestModelFuzzySearchRanksExactAndSupportsUnicode(t *testing.T) {
	exact, ok := modelMatchScore("llama", "llama3.1:8b")
	require.True(t, ok)
	fuzzy, ok := modelMatchScore("llm8b", "llama3.1:8b")
	require.True(t, ok)
	require.Greater(t, fuzzy, exact)
	_, ok = modelMatchScore("çã", "Explicação")
	require.True(t, ok)
	_, ok = modelMatchScore("qwen", "llama3.1:8b")
	require.False(t, ok)
	m := catalogDialog(t)
	m.Update(m.Open()())
	m.move(1)
	selected := m.filtered[m.selectedIdx].ID
	refresh := m.load()
	require.Len(t, m.filtered, 2)
	require.Equal(t, selected, m.filtered[m.selectedIdx].ID)
	m.Update(refresh())
	require.Equal(t, selected, m.filtered[m.selectedIdx].ID)
	failed := m.load()
	require.NotNil(t, failed)
	m.Update(ModelCatalogLoadedMsg{request: m.request, err: context.DeadlineExceeded})
	require.Len(t, m.filtered, 2)
	require.Equal(t, selected, m.filtered[m.selectedIdx].ID)
	m.query = "llm8b"
	m.filter()
	require.Len(t, m.filtered, 1)
	require.Equal(t, "llama3.1:8b", m.filtered[0].APIModel)
}

func TestSelectedModelMetadataIsDebouncedCancelledAndNeverGenerates(t *testing.T) {
	m := catalogDialog(t)
	shows := 0
	http.DefaultTransport = catalogTestTransport(func(req *http.Request) (*http.Response, error) {
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		status, body := 404, `{}`
		switch req.URL.Path {
		case "/v1/models":
			status, body = 200, `{"data":[{"id":"llama3.1:8b"},{"id":"qwen:7b"}]}`
		case "/api/show":
			shows++
			status, body = 200, `{"model_info":{"general.architecture":"llama","llama.context_length":131072}}`
		case "/api/chat", "/api/generate", "/api/pull":
			t.Fatalf("metadata attempted generation/download: %s", req.URL.Path)
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	_, first := m.Update(m.Open()())
	require.Zero(t, shows, "catalog must not read metadata for every model")
	require.NotNil(t, first)
	_, second := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	require.NotNil(t, second)
	stale := first().(modelMetadataLoadedMsg)
	require.ErrorIs(t, stale.err, context.Canceled)
	m.Update(stale)
	require.Zero(t, shows, "moving during debounce cancels the previous request")
	m.Update(second())
	require.Equal(t, 1, shows)
	selected := m.filtered[m.selectedIdx]
	require.EqualValues(t, 131072, selected.ContextWindow)
	require.NotContains(t, models.SupportedModels, selected.ID, "UI must not publish metadata to registry")
	m.move(-1)
	late := m.scheduleMetadata()
	require.NotNil(t, late)
	m.Close()
	m.Update(late())
	require.Equal(t, 1, shows, "closed dialog must not issue queued metadata requests")
}

func TestFastSelectionResolvesMetadataAndCancellationDiscardsSelection(t *testing.T) {
	m := catalogDialog(t)
	http.DefaultTransport = catalogTestTransport(func(req *http.Request) (*http.Response, error) {
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		body := `{"data":[{"id":"llama3.1:8b"}]}`
		if req.URL.Path == "/api/show" {
			body = `{"model_info":{"general.architecture":"llama","llama.context_length":131072}}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	m.Update(m.Open()())
	chosen := m.choose()
	require.NotNil(t, chosen)
	result := chosen().(ModelSelectedMsg)
	require.EqualValues(t, 131072, result.Model.ContextWindow)
	require.Empty(t, config.Get().Models, "metadata must not rewrite settings")
	m.Update(m.Open()())
	cancelled := m.choose()
	m.Close()
	require.Nil(t, cancelled())
}
