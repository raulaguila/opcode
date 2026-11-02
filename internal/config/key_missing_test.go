package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestUnsetKeyVariableLoadsTheConnectionOffAndMarked(t *testing.T) {
	isolateModelConfig(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LOCAL_ENDPOINT", "")
	t.Setenv("OPCODE_UNSET_ROUTER_KEY", "")
	document := `{"providers":{"local":{"baseURL":"http://127.0.0.1:11434/v1","api":"ollama"}},
"providerProfiles":{"router":{"name":"Router","backend":"openrouter","apiKeyEnv":"OPCODE_UNSET_ROUTER_KEY"},"off":{"name":"Off","backend":"openrouter","disabled":true,"apiKeyEnv":"OPCODE_UNSET_ROUTER_KEY"}},
"models":{"fixture":{"provider":"local","api_model":"fixture","api":"ollama","context_window":16384,"default_max_tokens":2048}},"agents":{"coder":{"model":"fixture"}}}`
	path := filepath.Join(t.TempDir(), "settings.json")
	require.NoError(t, os.WriteFile(path, []byte(document), 0600))
	viper.SetConfigFile(path)
	loaded, err := LoadOffline(t.TempDir())
	require.NoError(t, err, "one connection without its key no longer stops the others")
	require.True(t, loaded.ProviderProfiles["router"].Disabled)
	require.True(t, KeyMissing(models.ModelProvider("profile:router")))
	require.False(t, KeyMissing(models.ModelProvider("profile:off")), "a connection already off is just off")
	raw, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	require.Contains(t, string(raw), `"name":"Router","backend":"openrouter","apiKeyEnv"`, "the file is left as written")
}
