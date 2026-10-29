package agent

import (
	"context"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestDiscoveredGatewayContextFromSelectionThroughFirstRequest(t *testing.T) {
	for _, learned := range []int64{0, 4096} {
		name := "fresh discovery"
		if learned > 0 {
			name = "existing learned limit"
		}
		t.Run(name, func(t *testing.T) {
			a, _, _, root := runtimeAgent(t, workflow.Ask)
			registry := maps.Clone(models.SupportedModels)
			previousPath := viper.ConfigFileUsed()
			t.Cleanup(func() { models.SupportedModels = registry; viper.SetConfigFile(previousPath) })
			path := filepath.Join(root, "global.json")
			require.NoError(t, os.WriteFile(path, []byte(`{}`), 0600))
			viper.SetConfigFile(path)
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/gateway/v1/models":
					w.Header().Set("Content-Type", "application/json")
					io.WriteString(w, `{"data":[{"id":"team-model","max_model_len":131072}]}`)
				case "/gateway/v1/chat/completions":
					requests.Add(1)
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":20,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n")
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			cfg := config.Get()
			cfg.Data.Directory = t.TempDir()
			cfg.Models = map[models.ModelID]models.Model{}
			cfg.Agents = map[config.AgentName]config.Agent{}
			cfg.ProviderProfiles = map[string]config.ProviderProfile{"office": {Backend: models.ProviderLocal, Provider: config.Provider{BaseURL: server.URL + "/gateway/v1", API: "chat_completions"}}}
			found, err := config.ProfileModelCatalog("office")(models.WithoutCatalogMetadata(context.Background()))
			require.NoError(t, err)
			require.Len(t, found, 1)
			require.EqualValues(t, 131072, found[0].ContextWindow)
			if learned > 0 {
				require.NoError(t, saveContextCalibration(found[0], learned))
			}
			selected, err := a.SelectModel(config.AgentCoder, found[0])
			require.NoError(t, err)
			// Persisted automatic settings omit a hardcoded context; the registry
			// and active provider are separate snapshots with different purposes.
			require.Zero(t, cfg.Models[selected.ID].ContextWindow)
			require.EqualValues(t, 131072, models.SupportedModels[selected.ID].ContextWindow)
			want := int64(131072)
			if learned > 0 {
				want = learned
			}
			require.Equal(t, want, selected.ContextWindow)
			metrics, err := a.Usage(context.Background(), "s")
			require.NoError(t, err)
			require.Equal(t, selected.ID, metrics.ActiveModel)
			require.Equal(t, want, metrics.ActiveContextWindow)
			require.Zero(t, metrics.ContextWindow, "no request was budgeted yet")
			done, err := a.Run(context.Background(), "s", "Responda somente OK.")
			require.NoError(t, err)
			result := finishGeneration(t, done)
			if result.Error != nil {
				require.True(t, learned > 0 && errors.Is(result.Error, ErrContextLimit), "unexpected failure: %v", result.Error)
			} else {
				require.EqualValues(t, 1, requests.Load())
			}
			status, err := a.ContextStatus(context.Background(), "s")
			require.NoError(t, err)
			require.Equal(t, want, status.ContextWindow)
			if learned == 0 {
				require.Equal(t, "provider", status.ContextSource)
			} else {
				require.Equal(t, "learned after provider context/memory failure", status.ContextSource)
			}
		})
	}
}
