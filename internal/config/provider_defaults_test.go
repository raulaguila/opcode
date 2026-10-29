package config

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestNamedProviderCredentialDoesNotShadowConnectionFields(t *testing.T) {
	isolateModelConfig(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LOCAL_ENDPOINT", "")
	t.Setenv("LOCAL_API_KEY", "")
	t.Setenv("OPCODE_NAMED_PROVIDER_KEY", "named-private-fixture-key")
	const document = `{"providers":{"local":{"baseURL":"http://127.0.0.1:11434/v1","api":"ollama","apiKeyEnv":"OPCODE_NAMED_PROVIDER_KEY","disabled":false,"contextWindow":16384,"defaultMaxTokens":2048},"openai":{"baseURL":"https://gateway.invalid/v1","api":"responses","disabled":true}},"models":{"fixture":{"provider":"local","api_model":"fixture","api":"ollama","context_window":16384,"default_max_tokens":2048}},"agents":{"coder":{"model":"fixture"}}}`
	path := filepath.Join(t.TempDir(), "settings.json")
	require.NoError(t, os.WriteFile(path, []byte(document), 0600))
	viper.SetConfigFile(path)
	oldTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = oldTransport })
	http.DefaultTransport = localDiscoveryTransport(func(*http.Request) (*http.Response, error) {
		t.Error("explicit offline model cannot discover over HTTP")
		return nil, nil
	})
	loaded, err := LoadOffline(t.TempDir())
	require.NoError(t, err)
	p := loaded.Providers[models.ProviderLocal]
	require.Equal(t, "http://127.0.0.1:11434/v1", p.BaseURL)
	require.Equal(t, "ollama", p.API)
	require.Equal(t, "OPCODE_NAMED_PROVIDER_KEY", p.APIKeyEnv)
	require.Equal(t, "named-private-fixture-key", p.APIKey)
	require.False(t, p.Disabled)
	require.EqualValues(t, 16384, p.ContextWindow)
	require.EqualValues(t, 2048, p.DefaultMaxTokens)
	sibling := loaded.Providers[models.ProviderOpenAI]
	require.True(t, sibling.Disabled)
	require.Equal(t, "https://gateway.invalid/v1", sibling.BaseURL)
	require.Equal(t, "responses", sibling.API)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.JSONEq(t, document, string(raw))
	require.NotContains(t, string(raw), "named-private-fixture-key")
}

func TestProviderDefaultSelectionRespectsNamedEnvironmentPrecedence(t *testing.T) {
	for _, value := range []string{"", "private-explicit-named-value"} {
		t.Run(strings.ReplaceAll(value, "private-explicit-named-value", "present"), func(t *testing.T) {
			isolateModelConfig(t)
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("OPCODE_DEFAULT_NAMED_KEY", value)
			t.Setenv("LOCAL_ENDPOINT", "")
			t.Setenv("LOCAL_API_KEY", "")
			t.Setenv("GITHUB_TOKEN", "")
			t.Setenv("ANTHROPIC_API_KEY", "")
			t.Setenv("OPENAI_API_KEY", "")
			t.Setenv("GEMINI_API_KEY", "")
			t.Setenv("GROQ_API_KEY", "")
			t.Setenv("OPENROUTER_API_KEY", "")
			t.Setenv("XAI_API_KEY", "")
			const document = `{"providers":{"openai":{"apiKeyEnv":"OPCODE_DEFAULT_NAMED_KEY","apiKey":"file-fallback-must-not-win","baseURL":"https://gateway.invalid/v1","api":"responses","disabled":true}}}`
			viper.SetConfigType("json")
			require.NoError(t, viper.ReadConfig(bytes.NewBufferString(document)))
			setProviderDefaults()
			var read Config
			require.NoError(t, viper.Unmarshal(&read))
			p := read.Providers[models.ProviderOpenAI]
			require.Equal(t, "https://gateway.invalid/v1", p.BaseURL)
			require.Equal(t, "responses", p.API)
			require.True(t, p.Disabled)
			require.Equal(t, "file-fallback-must-not-win", p.APIKey, "resolved named value must not be installed into Viper overrides")
			if value != "" {
				require.Equal(t, models.GPT41, read.Agents[AgentCoder].Model)
			} else {
				require.NotEqual(t, models.GPT41, read.Agents[AgentCoder].Model, "empty named credential must not select remote defaults from the file key")
			}
			raw, err := json.Marshal(viper.AllSettings())
			require.NoError(t, err)
			if value != "" {
				require.NotContains(t, string(raw), value)
			}
		})
	}
}

func TestNamedEmptyCredentialStillRejectsLiteralFallbackAfterDecode(t *testing.T) {
	isolateModelConfig(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LOCAL_ENDPOINT", "")
	t.Setenv("OPCODE_EMPTY_PROVIDER_KEY", "")
	document := `{"providers":{"local":{"baseURL":"http://127.0.0.1:11434/v1","api":"ollama","apiKey":"literal-must-not-win","apiKeyEnv":"OPCODE_EMPTY_PROVIDER_KEY"}},"models":{"fixture":{"provider":"local","api_model":"fixture","api":"ollama","context_window":16384,"default_max_tokens":2048}},"agents":{"coder":{"model":"fixture"}}}`
	path := filepath.Join(t.TempDir(), "settings.json")
	require.NoError(t, os.WriteFile(path, []byte(document), 0600))
	viper.SetConfigFile(path)
	_, err := LoadOffline(t.TempDir())
	require.ErrorContains(t, err, "environment variable OPCODE_EMPTY_PROVIDER_KEY is not set")
	require.NotContains(t, err.Error(), "literal-must-not-win")
	raw, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	require.JSONEq(t, document, string(raw))
}
