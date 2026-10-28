package config

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestCatalogSnapshotPreservesExplicitModelsAndProviderDefaults(t *testing.T) {
	isolateModelConfig(t)
	p := Provider{BaseURL: "http://local.invalid/v1", API: "ollama", ContextWindow: 16384, DefaultMaxTokens: 2048}
	explicit := models.Model{ID: "my.llama", Provider: models.ProviderLocal, APIModel: "llama3.1:8b", API: "ollama", Name: "My Llama", ContextWindow: 32768, DefaultMaxTokens: 20000, CanReason: true}
	require.NoError(t, registerModels(map[models.ModelID]models.Model{explicit.ID: explicit}))
	cfg = &Config{Providers: map[models.ModelProvider]Provider{models.ProviderLocal: p}, Models: map[models.ModelID]models.Model{explicit.ID: explicit}, Agents: map[AgentName]Agent{AgentCoder: {Model: explicit.ID}}}
	fetch := ModelCatalog(models.ProviderLocal)
	// Mutations after capture must not affect a running request.
	cfg.Providers[models.ProviderLocal] = Provider{Disabled: true, BaseURL: "https://wrong.invalid"}
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	http.DefaultTransport = localDiscoveryTransport(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, "local.invalid", req.URL.Host)
		status, body := 404, `{}`
		if req.URL.Path == "/v1/models" {
			status, body = 200, `{"data":[{"id":"llama3.1:8b"},{"id":"other:7b"}]}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	found, err := fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, found, 2)
	require.Equal(t, explicit, found[0])
	require.Equal(t, "ollama", found[1].API)
	require.EqualValues(t, 16384, found[1].ContextWindow)
	require.EqualValues(t, 2048, found[1].DefaultMaxTokens)
	require.NotContains(t, models.SupportedModels, found[1].ID, "the network worker cannot publish into the shared registry")
}

func TestNamedEnvironmentKeyLoadsWithProviderOnlyConfiguration(t *testing.T) {
	isolateModelConfig(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LOCAL_ENDPOINT", "")
	t.Setenv("OPCODE_TEST_PROVIDER_KEY", "private-environment-value")
	root := t.TempDir()
	path := filepath.Join(root, "settings.json")
	data := `{"providers":{"openai":{"apiKeyEnv":"OPCODE_TEST_PROVIDER_KEY","baseURL":"https://provider.invalid/v1","api":"responses"}},"future":{"large":9007199254740993}}`
	require.NoError(t, os.WriteFile(path, []byte(data), 0600))
	viper.SetConfigFile(path)
	loaded, err := Load(root, false)
	require.NoError(t, err)
	require.NoError(t, loaded.ValidateStartup())
	require.Equal(t, "private-environment-value", loaded.Providers[models.ProviderOpenAI].APIKey)
	require.NoError(t, UpdateAgentModel(AgentCoder, models.GPT41))
	saved, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(saved), "private-environment-value")
	require.Contains(t, string(saved), "OPCODE_TEST_PROVIDER_KEY")
	require.Contains(t, string(saved), "9007199254740993")
	// Existing model definitions still win; provider defaults apply to discovery.
}

func TestSelectedDiscoveredModelPersistsThroughRestartInProject(t *testing.T) {
	isolateModelConfig(t)
	t.Setenv("LOCAL_ENDPOINT", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	path := filepath.Join(root, ".opcode.json")
	old := models.Model{ID: "local.old:8b", Provider: models.ProviderLocal, APIModel: "old:8b", API: "ollama", ContextWindow: 16384, DefaultMaxTokens: 2048}
	file := map[string]any{"providers": map[string]any{"local": map[string]any{"baseURL": "http://local.invalid/v1", "api": "ollama", "futureProviderField": true}}, "models": map[string]any{string(old.ID): old}, "agents": map[string]any{"coder": map[string]any{"model": old.ID, "futureAgentField": "keep"}}, "futureField": map[string]any{"keep": "yes"}}
	data, err := json.Marshal(file)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0644))
	global := filepath.Join(t.TempDir(), "global.json")
	require.NoError(t, os.WriteFile(global, []byte(`{}`), 0600))
	viper.SetConfigFile(global)
	loaded, err := Load(root, false)
	require.NoError(t, err)
	candidate := models.Model{ID: "local.new.model:7b", Name: "New", Provider: models.ProviderLocal, APIModel: "new.model:7b", API: "ollama", ContextWindow: 8192, DefaultMaxTokens: 1024}
	require.NoError(t, UpdateAgentModelDefinition(AgentCoder, candidate))
	require.Equal(t, candidate.ID, loaded.Agents[AgentCoder].Model)
	saved, err := os.ReadFile(path)
	require.NoError(t, err)
	for _, value := range []string{"futureField", "futureProviderField", "futureAgentField"} {
		require.Contains(t, string(saved), value)
	}
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	unchanged, err := os.ReadFile(global)
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(unchanged))
	cfg = nil
	viper.Reset()
	viper.SetConfigFile(global)
	delete(models.SupportedModels, candidate.ID)
	reloaded, err := Load(root, false)
	require.NoError(t, err)
	require.Equal(t, candidate.ID, reloaded.Agents[AgentCoder].Model)
	require.Equal(t, candidate, models.SupportedModels[candidate.ID])
	// Failed persistence must leave both the active selection and registry intact.
	viper.SetConfigFile(filepath.Join(root, "missing", "config.json"))
	reloaded.WorkingDir = t.TempDir()
	candidate.ID = "local.failed"
	require.Error(t, UpdateAgentModelDefinition(AgentCoder, candidate))
	require.Equal(t, models.ModelID("local.new.model:7b"), reloaded.Agents[AgentCoder].Model)
	require.NotContains(t, models.SupportedModels, candidate.ID)
}

func TestAutomaticLocalContextRefreshesAfterRestartAndHonorsOverrides(t *testing.T) {
	for _, profile := range []bool{false, true} {
		for _, override := range []int64{0, 16384} {
			t.Run(fmt.Sprintf("profile=%t/override=%d", profile, override), func(t *testing.T) {
				isolateMigrationConfig(t)
				root := t.TempDir()
				p := Provider{BaseURL: "http://local.invalid/v1", API: "ollama", ContextWindow: override}
				model := models.Model{ID: "local.active", Provider: models.ProviderLocal, APIModel: "active", API: "ollama", ContextWindow: 4096, ContextAuto: true, DefaultMaxTokens: 2048}
				document := Config{Models: map[models.ModelID]models.Model{model.ID: model}, Agents: map[AgentName]Agent{AgentCoder: {Model: model.ID}}}
				if profile {
					model.ID, model.ProviderProfile = "profile.home.active", "home"
					document.Models = map[models.ModelID]models.Model{model.ID: model}
					document.Agents[AgentCoder] = Agent{Model: model.ID}
					document.ProviderProfiles = map[string]ProviderProfile{"home": {Backend: models.ProviderLocal, Provider: p}}
				} else {
					document.Providers = map[models.ModelProvider]Provider{models.ProviderLocal: p}
				}
				data, err := json.Marshal(document)
				require.NoError(t, err)
				path := filepath.Join(root, "settings.json")
				require.NoError(t, os.WriteFile(path, data, 0600))
				viper.SetConfigFile(path)
				previous := http.DefaultTransport
				t.Cleanup(func() { http.DefaultTransport = previous })
				calls := 0
				http.DefaultTransport = localDiscoveryTransport(func(req *http.Request) (*http.Response, error) {
					calls++
					status, body := 404, `{}`
					switch req.URL.Path {
					case "/v1/models":
						status, body = 200, `{"data":[{"id":"active"}]}`
					case "/api/ps":
						status, body = 200, `{"models":[{"name":"active","context_length":32768}]}`
					}
					return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
				})
				loaded, err := Load(root, false)
				require.NoError(t, err)
				want := int64(32768)
				if override > 0 {
					want = override
				}
				require.Equal(t, want, models.SupportedModels[loaded.Agents[AgentCoder].Model].ContextWindow)
				require.Positive(t, calls)
				found, err := modelCatalog(models.ProviderLocal, model.ProviderProfile, p)(context.Background())
				require.NoError(t, err)
				require.Equal(t, want, found[0].ContextWindow)
				require.NoError(t, UpdateAgentModelDefinition(AgentCoder, found[0]))
				cfg = nil
				viper.Reset()
				viper.SetConfigFile(path)
				calls = 0
				_, err = LoadOffline(root)
				require.NoError(t, err)
				require.Zero(t, calls, "offline commands must use saved metadata without network")
				require.Equal(t, want, models.SupportedModels[model.ID].ContextWindow)
			})
		}
	}
}
