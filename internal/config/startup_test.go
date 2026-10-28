package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestStartupWithoutProviderGivesConfigurationInstructions(t *testing.T) {
	isolateModelConfig(t)
	// Explicit values isolate this regression from the developer's credentials.
	for _, name := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY", "GROQ_API_KEY", "OPENROUTER_API_KEY", "XAI_API_KEY", "GITHUB_TOKEN", "AZURE_OPENAI_ENDPOINT", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_PROFILE", "AWS_DEFAULT_PROFILE", "AWS_REGION", "AWS_DEFAULT_REGION", "AWS_CONTAINER_CREDENTIALS_RELATIVE_URI", "AWS_CONTAINER_CREDENTIALS_FULL_URI", "VERTEXAI_PROJECT", "GOOGLE_CLOUD_PROJECT"} {
		t.Setenv(name, "")
	}
	t.Setenv("LOCAL_ENDPOINT", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, ".opcode.json")
	require.NoError(t, os.WriteFile(path, []byte(`{}`), 0600))
	viper.SetConfigFile(path)
	loaded, err := Load(dir, false)
	require.NoError(t, err, "loading settings alone must not require credentials")
	err = loaded.ValidateStartup()
	require.ErrorContains(t, err, "OPENAI_API_KEY")
	require.ErrorContains(t, err, "~/.opcode.json")
	require.ErrorContains(t, err, "agents.coder.model")
	require.NotContains(t, loaded.Agents, AgentTitle, "do not manufacture an agent with an empty model")
}

func TestValidateCompletesMissingAuxiliaryAgents(t *testing.T) {
	isolateModelConfig(t)
	cfg = &Config{
		Providers: map[models.ModelProvider]Provider{models.ProviderOpenAI: {APIKey: "unit-test-key"}},
		Agents: map[AgentName]Agent{
			AgentCoder: {Model: models.GPT41Mini, MaxTokens: 500},
			AgentTask:  {Model: models.GPT41, MaxTokens: 600},
		},
	}
	require.NoError(t, Validate())
	require.NoError(t, cfg.ValidateStartup())
	for _, name := range []AgentName{AgentTitle, AgentSummarizer} {
		require.Equal(t, models.GPT41Mini, cfg.Agents[name].Model)
	}
	require.Equal(t, Agent{Model: models.GPT41, MaxTokens: 600}, cfg.Agents[AgentTask], "preserve explicit auxiliary choices")
}

func TestLoadSingleCoderWithCustomProviderModel(t *testing.T) {
	isolateModelConfig(t)
	t.Setenv("LOCAL_ENDPOINT", "")
	t.Setenv("LOCAL_API_KEY", "")
	dir := t.TempDir()
	path := filepath.Join(dir, ".opcode.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
  "providers":{"local":{"baseURL":"http://127.0.0.1:11434/v1"}},
  "models":{"my-local-model":{"provider":"local","api_model":"local-model","context_window":32768,"default_max_tokens":4096}},
  "agents":{"coder":{"model":"my-local-model","maxTokens":500}}
}`), 0600))
	viper.SetConfigFile(path)
	// Isolate provider selection without consulting any real provider credentials.
	viper.Set("agents", map[string]any{"coder": map[string]any{"model": "my-local-model", "maxTokens": 500}})
	loaded, err := Load(dir, false)
	require.NoError(t, err)
	require.NoError(t, loaded.ValidateStartup())
	require.Empty(t, loaded.Providers[models.ProviderLocal].APIKey)
	require.False(t, loaded.Providers[models.ProviderLocal].Disabled)
	for _, name := range []AgentName{AgentCoder, AgentTitle, AgentSummarizer, AgentTask} {
		require.Equal(t, models.ModelID("my-local-model"), loaded.Agents[name].Model)
	}
	require.EqualValues(t, 80, loaded.Agents[AgentTitle].MaxTokens)
}
