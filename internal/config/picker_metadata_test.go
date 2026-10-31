package config

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/stretchr/testify/require"
)

func TestPickerMetadataReachesStandaloneAndNamedConnections(t *testing.T) {
	for _, profile := range []string{"", "office"} {
		t.Run("profile="+profile, func(t *testing.T) {
			isolateModelConfig(t)
			provider := Provider{BaseURL: "http://fixture.invalid/v1", API: "ollama"}
			cfg = &Config{Providers: map[models.ModelProvider]Provider{models.ProviderLocal: provider}, ProviderProfiles: map[string]ProviderProfile{"office": {Backend: models.ProviderLocal, Provider: provider}}}
			previous := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = previous })
			http.DefaultTransport = localDiscoveryTransport(func(req *http.Request) (*http.Response, error) {
				require.Equal(t, "/api/show", req.URL.Path)
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"capabilities":["embedding"],"remote_host":"https://ollama.com"}`)), Request: req}, nil
			})
			model := models.Model{ID: "fixture", APIModel: "fixture", Provider: models.ProviderLocal, ProviderProfile: profile, DefaultMaxTokens: 2048}
			lookup := ModelMetadata(model)
			require.NotNil(t, lookup)
			found, err := lookup(context.Background())
			require.NoError(t, err)
			require.True(t, found.EmbeddingOnly())
			require.Equal(t, "cloud", found.Hosting)
			require.Equal(t, profile, found.ProviderProfile)
		})
	}
}

func TestPickerCatalogMetadataSurvivesSavedModelMerge(t *testing.T) {
	for _, profile := range []string{"", "office"} {
		t.Run("profile="+profile, func(t *testing.T) {
			isolateModelConfig(t)
			provider := Provider{BaseURL: "http://fixture.invalid/v1", API: "ollama"}
			known := models.Model{ID: "saved-fixture", APIModel: "fixture", Provider: models.ProviderLocal, ProviderProfile: profile, ContextWindow: 32768}
			models.SupportedModels[known.ID] = known
			cfg = &Config{Providers: map[models.ModelProvider]Provider{models.ProviderLocal: provider}, ProviderProfiles: map[string]ProviderProfile{"office": {Backend: models.ProviderLocal, Provider: provider}}, Models: map[models.ModelID]models.Model{known.ID: known}, Agents: map[AgentName]Agent{AgentCoder: {Model: known.ID}}}
			previous := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = previous })
			http.DefaultTransport = localDiscoveryTransport(func(req *http.Request) (*http.Response, error) {
				body, status := `{}`, http.StatusNotFound
				if req.URL.Path == "/v1/models" {
					body, status = `{"data":[{"id":"fixture","capabilities":["embedding"],"remote_host":"https://ollama.com"}]}`, http.StatusOK
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})
			lookup := ModelCatalog(models.ProviderLocal)
			if profile != "" {
				lookup = ProfileModelCatalog(profile)
			}
			found, err := lookup(models.WithoutCatalogMetadata(context.Background()))
			require.NoError(t, err)
			require.Len(t, found, 1)
			require.True(t, found[0].EmbeddingOnly())
			require.Equal(t, "cloud", found[0].Hosting)
			require.EqualValues(t, 32768, found[0].ContextWindow, "manual context remains intact")
		})
	}
}

func TestPickerMissingCapabilityDoesNotEraseKnownEvidence(t *testing.T) {
	unsupported, supported := false, true
	known := models.Model{SupportsChat: &unsupported, Hosting: "cloud"}
	snapshot := providerCatalogSnapshot{}
	unknown := snapshot.refreshAutomaticModel(known, models.Model{}, true)
	require.True(t, unknown.EmbeddingOnly())
	require.Equal(t, "cloud", unknown.Hosting)
	updated := snapshot.refreshAutomaticModel(known, models.Model{SupportsChat: &supported, Hosting: "local"}, true)
	require.False(t, updated.EmbeddingOnly(), "fresh explicit completion evidence supersedes the prior report")
	require.Equal(t, "local", updated.Hosting)
}
