package dialog

import (
	"io"
	"net/http"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/stretchr/testify/require"
)

func pickerClickLabel(t *testing.T, view, label string) tea.MouseClickMsg {
	t.Helper()
	for y, row := range strings.Split(ansi.Strip(view), "\n") {
		if before, _, found := strings.Cut(row, label); found {
			return tea.MouseClickMsg{X: ansi.StringWidth(before), Y: y, Button: tea.MouseLeft}
		}
	}
	t.Fatalf("visible action %q missing from:\n%s", label, ansi.Strip(view))
	return tea.MouseClickMsg{}
}

func TestModelPickerHidesOnlyExplicitEmbeddingEvidence(t *testing.T) {
	m := populatedModelDialog(t)
	embedding := false
	m.models[0].SupportsChat = &embedding
	m.models[1].APIModel = "nomic-embed-text-cloud"
	unsupported := false
	m.models[1].SupportsTools = &unsupported
	m.filter()
	require.Len(t, m.filtered, 15)
	require.NotContains(t, m.filtered, m.models[0])
	require.Contains(t, m.filtered, m.models[1], "unknown chat capability and no tools must remain selectable")
	m.filtered = []models.Model{m.models[0]}
	m.selectedIdx = 0
	require.Nil(t, m.choose(), "known embedding metadata cannot escape through a stale highlight")
}

func TestFastModelSelectionRechecksEmbeddingEvidence(t *testing.T) {
	m := catalogDialog(t)
	http.DefaultTransport = catalogTestTransport(func(req *http.Request) (*http.Response, error) {
		body := `{"data":[{"id":"fixture"}]}`
		if req.URL.Path == "/api/show" {
			body = `{"capabilities":["embedding"]}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	m.Update(m.Open()())
	chosen := m.choose()
	require.NotNil(t, chosen)
	result := chosen()
	require.IsType(t, modelMetadataLoadedMsg{}, result, "embedding evidence must never become ModelSelectedMsg")
	m.Update(result)
	require.Empty(t, m.filtered)
	require.False(t, m.selecting)
	require.True(t, m.search.Focused())
	require.Contains(t, ansi.Strip(m.View()), "Embedding-only model hidden")
}

func TestModelContextCompactsEqualLimitsAndDetailsPreserveProvenance(t *testing.T) {
	m := populatedModelDialog(t)
	m.models[0].ContextWindow, m.models[0].ContextSource = 65536, "provider"
	m.filter()
	m.SetActiveContext(m.models[0].ID, 65536, "ollama:loaded")
	compact := strings.Join(m.modelDetailLines(), "\n")
	require.Contains(t, compact, "Context in use: 64k")
	require.NotContains(t, compact, "Effective:")
	m.Update(pickerClickLabel(t, m.View(), "Ctrl+D details"))
	details := strings.Join(m.modelDetailLines(), "\n")
	require.Contains(t, details, "Context: 64k (65536)")
	require.Contains(t, details, "advertised")
	require.Contains(t, details, "Effective: 64k (65536)")
	require.Contains(t, details, "server allocation")
	m.details = false
	m.SetActiveContext(m.models[0].ID, 32768, "configuration")
	different := strings.Join(m.modelDetailLines(), "\n")
	require.Contains(t, different, "Context: 64k")
	require.Contains(t, different, "Effective: 32k")
}

func TestModelHostingAndDetailsFollowSelectedLocale(t *testing.T) {
	m := populatedModelDialog(t)
	m.models[0].Hosting = "cloud"
	m.models[1].Hosting = "local"
	m.models[2].APIModel = "misleading-cloud-name"
	m.filter()
	for _, tc := range []struct{ language, cloud, details string }{{"en", "Cloud", "Ctrl+D details"}, {"pt", "Nuvem", "Ctrl+D detalhes"}} {
		config.Get().TUI.Language = tc.language
		view := ansi.Strip(m.View())
		require.Contains(t, view, m.modelLabel(m.models[0])+" · "+tc.cloud)
		require.Contains(t, view, m.modelLabel(m.models[1])+" · Local")
		require.NotContains(t, view, m.modelLabel(m.models[2])+" · "+tc.cloud)
		require.Contains(t, view, tc.details)
	}
}

func TestProviderListActionsAreVisibleAndClickableInBothLocales(t *testing.T) {
	for _, tc := range []struct{ language, add, edit, remove, preferences, integrations string }{
		{"en", "[Add]", "[Edit]", "[Delete]", "[Preferences]", "[Integrations]"},
		{"pt", "[Adicionar]", "[Editar]", "[Excluir]", "[Preferências]", "[Integrações]"},
	} {
		t.Run(tc.language, func(t *testing.T) {
			m := settingsDialog(t)
			config.Get().TUI.Language = tc.language
			m.Update(tea.WindowSizeMsg{Width: 36, Height: 20})
			m.Open()
			for _, label := range []string{tc.add, tc.edit, tc.remove, tc.preferences, tc.integrations} {
				require.Contains(t, ansi.Strip(m.View()), label)
			}
			require.LessOrEqual(t, lipgloss.Width(m.View()), 36)
			require.LessOrEqual(t, lipgloss.Height(m.View()), 20)
			m.Update(pickerClickLabel(t, m.View(), tc.add))
			require.Equal(t, "presets", m.screen)
			m.Open()
			m.Update(pickerClickLabel(t, m.View(), tc.edit))
			require.Equal(t, "provider", m.screen)
			require.True(t, m.legacy)
			m.Open()
			m.Update(pickerClickLabel(t, m.View(), tc.remove))
			require.Equal(t, "delete-provider", m.screen)
			require.Contains(t, config.Get().Providers, models.ProviderLocal, "clicking Delete only opens confirmation")
			m.Open()
			m.Update(pickerClickLabel(t, m.View(), tc.preferences))
			require.Equal(t, "settings", m.screen)
			m.Open()
			_, cmd := m.Update(pickerClickLabel(t, m.View(), tc.integrations))
			require.IsType(t, OpenIntegrationDialogMsg{}, cmd())
		})
	}
}

func TestProviderStatusDoesNotImplyHealthAndEditsInvalidateLastTest(t *testing.T) {
	m := settingsDialog(t)
	require.Contains(t, m.providerRowLabel(m.rows[0]), "configured")
	provider := config.Get().Providers[models.ProviderLocal]
	provider.Disabled = true
	config.Get().Providers[models.ProviderLocal] = provider
	require.Contains(t, m.providerRowLabel(m.rows[0]), "disabled")
	require.NotContains(t, m.providerRowLabel(m.rows[0]), "configured")
	m.edit(&m.rows[0])
	m.finishCatalogTest(providerTestMsg{count: 3})
	require.Contains(t, m.note, "Tools not tested")
	m.Update(tea.KeyPressMsg{Text: "changed"})
	require.False(t, m.catalogTested)
	require.NotContains(t, m.note, "3 models")
	require.Contains(t, m.note, "test again")
}
