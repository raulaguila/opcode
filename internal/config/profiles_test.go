package config

import (
	"context"
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

func TestProfileJSONKeepsBackendSeparateAndNeverSavesResolvedKey(t *testing.T) {
	isolateModelConfig(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LOCAL_ENDPOINT", "")
	t.Setenv("PROFILE_TEST_KEY", "environment-secret-value")
	root := t.TempDir()
	path := filepath.Join(root, "config.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"providerProfiles":{"office":{"name":"Office gateway","backend":"openai","apiKeyEnv":"PROFILE_TEST_KEY","baseURL":"https://office.invalid/v1","api":"responses","contextWindow":32000}},"models":{"profile.office.model":{"provider":"openai","provider_profile":"office","api_model":"model","context_window":16000}},"agents":{"coder":{"model":"profile.office.model"}},"future":{"keep":9007199254740993}}`), 0600))
	viper.SetConfigFile(path)
	loaded, err := Load(root, false)
	require.NoError(t, err)
	require.NoError(t, loaded.ValidateStartup())
	require.Equal(t, "Office gateway", loaded.ProviderProfiles["office"].Name)
	model := models.SupportedModels["profile.office.model"]
	p, err := ProviderForModel(model)
	require.NoError(t, err)
	require.Equal(t, "https://office.invalid/v1", p.BaseURL)
	require.Equal(t, "environment-secret-value", p.APIKey)
	require.Equal(t, models.ProviderOpenAI, model.Provider)
	require.NoError(t, SaveProviderProfile("office", ProviderProfile{Name: "Renamed", Backend: models.ProviderOpenAI, Provider: Provider{APIKeyEnv: "PROFILE_TEST_KEY", BaseURL: p.BaseURL, API: "responses"}}))
	require.NoError(t, UpdateAgentModelDefinition(AgentCoder, model))
	saved, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(saved), "environment-secret-value")
	require.Contains(t, string(saved), "PROFILE_TEST_KEY")
	require.Contains(t, string(saved), "9007199254740993")
	require.Empty(t, StoredProviderKey("office", false))
	cfg = nil
	viper.Reset()
	viper.SetConfigFile(path)
	loaded, err = Load(root, false)
	require.NoError(t, err)
	require.Equal(t, "office", models.SupportedModels[loaded.Agents[AgentCoder].Model].ProviderProfile)
	bad := model
	bad.Provider = models.ProviderAnthropic
	_, err = ProviderForModel(bad)
	require.ErrorContains(t, err, "differs")
}
func TestTwoProfilesUseIndependentCatalogEndpointsAndIDs(t *testing.T) {
	isolateModelConfig(t)
	cfg = &Config{ProviderProfiles: map[string]ProviderProfile{
		"home":   {Backend: models.ProviderLocal, Provider: Provider{BaseURL: "http://home.invalid/v1", API: "ollama", ContextWindow: 16384}},
		"office": {Backend: models.ProviderLocal, Provider: Provider{BaseURL: "http://office.invalid/v1", API: "chat_completions", ContextWindow: 8192}},
	}, Agents: map[AgentName]Agent{}}
	home, office := ProfileModelCatalog("home"), ProfileModelCatalog("office")
	changed := cfg.ProviderProfiles["home"]
	changed.BaseURL = "http://wrong.invalid"
	cfg.ProviderProfiles["home"] = changed
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	http.DefaultTransport = localDiscoveryTransport(func(req *http.Request) (*http.Response, error) {
		status, body := 404, `{}`
		if req.URL.Path == "/v1/models" {
			require.Contains(t, []string{"home.invalid", "office.invalid"}, req.URL.Host)
			status, body = 200, `{"data":[{"id":"llama3.1:8b"}]}`
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
	})
	a, err := home(context.Background())
	require.NoError(t, err)
	b, err := office(context.Background())
	require.NoError(t, err)
	require.NotEqual(t, a[0].ID, b[0].ID)
	require.Equal(t, "home", a[0].ProviderProfile)
	require.Equal(t, "office", b[0].ProviderProfile)
	require.Equal(t, "ollama", a[0].API)
	require.Equal(t, "chat_completions", b[0].API)
	require.EqualValues(t, 16384, a[0].ContextWindow)
	require.False(t, a[0].ContextEstimated)
	require.NotContains(t, models.SupportedModels, a[0].ID)
}
func TestFirstModelSelectionCreatesAuxiliaryRolesAndRecentList(t *testing.T) {
	isolateModelConfig(t)
	root := t.TempDir()
	path := filepath.Join(root, "settings.json")
	require.NoError(t, os.WriteFile(path, []byte(`{}`), 0600))
	viper.SetConfigFile(path)
	cfg = &Config{WorkingDir: root, Providers: map[models.ModelProvider]Provider{models.ProviderLocal: {BaseURL: "http://local.invalid/v1", API: "ollama"}}}
	candidate := models.Model{ID: "local.first", Name: "First", Provider: models.ProviderLocal, APIModel: "first", ContextWindow: 16000, DefaultMaxTokens: 1024}
	require.NoError(t, UpdateAgentModelDefinition(AgentCoder, candidate))
	require.NoError(t, cfg.ValidateStartup())
	require.Equal(t, []models.ModelID{"local.first"}, cfg.Preferences.RecentModels)
	require.EqualValues(t, 80, cfg.Agents[AgentTitle].MaxTokens)
	require.NoError(t, ToggleFavoriteModel(candidate.ID))
	require.Equal(t, []models.ModelID{candidate.ID}, cfg.Preferences.FavoriteModels)
	require.NoError(t, ToggleFavoriteModel(candidate.ID))
	require.Empty(t, cfg.Preferences.FavoriteModels)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "summarizer")
}
func TestProfileValidationAndPersistenceFailureAreAtomic(t *testing.T) {
	isolateModelConfig(t)
	cfg = &Config{WorkingDir: t.TempDir(), ProviderProfiles: map[string]ProviderProfile{}}
	profile := ProviderProfile{Backend: models.ProviderLocal, Provider: Provider{BaseURL: "http://localhost:11434/v1", API: "ollama"}}
	for _, id := range []string{"", "a.b", "a/b", "bad\n"} {
		_, err := PreviewProviderProfile(id, profile)
		require.Error(t, err)
	}
	bad := profile
	bad.BaseURL = "http://user:secret@localhost/v1"
	_, err := PreviewProviderProfile("valid", bad)
	require.Error(t, err)
	viper.SetConfigFile(filepath.Join(t.TempDir(), "missing", "config.json"))
	require.Error(t, SaveProviderProfile("valid", profile))
	require.Empty(t, cfg.ProviderProfiles)
}
