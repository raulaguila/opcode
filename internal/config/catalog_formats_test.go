package config

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/stretchr/testify/require"
)

func TestGatewayContextReachesNamedProfileSelectionAndHonorsManualOverride(t *testing.T) {
	for _, backend := range []models.ModelProvider{models.ProviderLocal, models.ProviderOpenAI} {
		for _, override := range []int64{0, 16384} {
			t.Run(fmt.Sprintf("%s/override=%d", backend, override), func(t *testing.T) {
				isolateModelConfig(t)
				cfg = &Config{
					ProviderProfiles: map[string]ProviderProfile{"office": {Name: "Office", Backend: backend, Provider: Provider{BaseURL: "https://gateway.invalid/prefix/v1", API: "chat_completions", APIKey: "fake-test-key", ContextWindow: override}}},
					Models:           map[models.ModelID]models.Model{}, Agents: map[AgentName]Agent{},
				}
				previous := http.DefaultTransport
				t.Cleanup(func() { http.DefaultTransport = previous })
				http.DefaultTransport = localDiscoveryTransport(func(req *http.Request) (*http.Response, error) {
					require.Equal(t, "/prefix/v1/models", req.URL.Path)
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"models":[{"id":"alias","openai":{"max_model_len":131072}}]}`)), Request: req}, nil
				})
				found, err := ProfileModelCatalog("office")(models.WithoutCatalogMetadata(context.Background()))
				require.NoError(t, err)
				require.Len(t, found, 1)
				selected, _, err := ModelSelection(AgentCoder, found[0])
				require.NoError(t, err)
				require.Equal(t, models.ModelID("profile.office.alias"), selected.ID)
				require.Equal(t, "chat_completions", selected.API)
				require.False(t, selected.ContextEstimated)
				if override == 0 {
					require.EqualValues(t, 131072, selected.ContextWindow)
					require.Equal(t, "provider", selected.ContextSource)
				} else {
					require.EqualValues(t, override, selected.ContextWindow)
					require.Equal(t, "configuration", selected.ContextSource)
				}
			})
		}
	}
}
