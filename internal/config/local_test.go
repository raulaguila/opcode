package config

import (
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

type localDiscoveryTransport func(*http.Request) (*http.Response, error)

func (f localDiscoveryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestLocalEndpointDiscoversConfiguredModelWithoutAPIKey(t *testing.T) {
	for _, selected := range []string{"", "local.llama3.1:8b", "local.unavailable"} {
		t.Run(selected, func(t *testing.T) {
			isolateModelConfig(t)
			t.Setenv("LOCAL_ENDPOINT", "")
			t.Setenv("LOCAL_API_KEY", "")
			t.Setenv("OPENAI_API_KEY", "unit-test-key")
			originalTransport := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = originalTransport })
			calls := 0
			http.DefaultTransport = localDiscoveryTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, "local.invalid", req.URL.Host)
				require.Empty(t, req.Header.Get("Authorization"))
				status, body := 404, `{}`
				if req.URL.Path == "/v1/models" {
					status, body = 200, `{"data":[{"id":"llama3.1:8b","object":"model"}]}`
				} else {
					require.Contains(t, []string{"/api/v0/models", "/api/ps"}, req.URL.Path)
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})
			dir := t.TempDir()
			path := filepath.Join(dir, ".opcode.json")
			require.NoError(t, os.WriteFile(path, []byte(`{"providers":{"local":{"baseURL":"http://local.invalid"}},"agents":{"coder":{"model":"`+selected+`"}}}`), 0600))
			viper.SetConfigFile(path)
			loaded, err := Load(dir, false)
			require.Equal(t, 3, calls)
			if selected == "local.unavailable" {
				require.ErrorContains(t, err, "unavailable")
				require.Nil(t, loaded)
				return
			}
			require.NoError(t, err)
			require.NoError(t, loaded.ValidateStartup())
			require.False(t, loaded.Providers[models.ProviderLocal].Disabled)
			require.Empty(t, loaded.Providers[models.ProviderLocal].APIKey)
			require.Equal(t, "http://local.invalid/v1", loaded.Providers[models.ProviderLocal].BaseURL)
			for _, name := range []AgentName{AgentCoder, AgentTitle, AgentSummarizer, AgentTask} {
				require.Equal(t, models.ModelID("local.llama3.1:8b"), loaded.Agents[name].Model)
			}
		})
	}
}

func TestLocalEndpointEnvironmentOverridesFile(t *testing.T) {
	isolateModelConfig(t)
	t.Setenv("LOCAL_ENDPOINT", "http://environment.invalid/custom/v1/")
	cfg = &Config{
		Providers: map[models.ModelProvider]Provider{models.ProviderLocal: {BaseURL: "http://file.invalid/v1"}},
		Agents:    map[AgentName]Agent{AgentCoder: {Model: "custom-local"}},
	}
	models.SupportedModels["custom-local"] = models.Model{ID: "custom-local", Provider: models.ProviderLocal, APIModel: "test"}
	require.NoError(t, configureLocalProvider())
	require.Equal(t, "http://environment.invalid/custom/v1", cfg.Providers[models.ProviderLocal].BaseURL)
}
