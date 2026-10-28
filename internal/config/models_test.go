package config

import (
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func isolateModelConfig(t *testing.T) {
	t.Helper()
	originalModels := maps.Clone(models.SupportedModels)
	originalConfig := cfg
	viper.Reset()
	cfg = nil
	t.Cleanup(func() { models.SupportedModels = originalModels; cfg = originalConfig; viper.Reset() })
}

func TestLoadCustomModelsWithoutChangingLegacyIDs(t *testing.T) {
	isolateModelConfig(t)
	dir := t.TempDir()
	path := filepath.Join(dir, ".opcode.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
  "providers":{"openai":{"apiKey":"unit-test-key"}},
  "models":{"custom-model":{"provider":"openai","api_model":"provider-model-id","api":"responses","context_window":128000,"default_max_tokens":4096,"can_reason":true,"supports_attachments":true,"cost_per_1m_in":1.25}},
  "agents":{"coder":{"model":"custom-model","reasoningEffort":"xhigh"},"title":{"model":"gpt-4.1-mini"}}
}`), 0o600))
	viper.SetConfigFile(path)
	loaded, err := Load(dir, false)
	require.NoError(t, err)
	require.Equal(t, models.ModelID("custom-model"), loaded.Agents[AgentCoder].Model)
	require.Equal(t, "xhigh", loaded.Agents[AgentCoder].ReasoningEffort)
	require.Equal(t, models.GPT41Mini, loaded.Agents[AgentTitle].Model)
	model := models.SupportedModels["custom-model"]
	require.Equal(t, "provider-model-id", model.APIModel)
	require.Equal(t, "responses", model.API)
	require.EqualValues(t, 128000, model.ContextWindow)
	require.EqualValues(t, 4096, model.DefaultMaxTokens)
	require.True(t, model.CanReason)
	require.True(t, model.SupportsAttachments)
	require.Equal(t, 1.25, model.CostPer1MIn)
	require.Equal(t, "gpt-4.1-mini", models.SupportedModels[models.GPT41Mini].APIModel)
}

func TestInvalidModelDefinitionsAreAtomic(t *testing.T) {
	for _, invalid := range []models.Model{
		{Provider: models.ProviderOpenAI, APIModel: "test", ContextWindow: -1},
		{Provider: "unsupported", APIModel: "test", ContextWindow: 1024},
		{Provider: models.ProviderAnthropic, APIModel: "test", ContextWindow: 1024, API: "responses"},
		{Provider: models.ProviderOpenAI, APIModel: "test", ContextWindow: 1024, ThinkingMode: "adaptive"},
		{Provider: models.ProviderOpenAI, APIModel: "test", ContextWindow: 1024, API: "ollama"},
		{Provider: models.ProviderOpenAI, APIModel: "test", ContextWindow: 1024, CostPer1MIn: -1},
	} {
		isolateModelConfig(t)
		err := registerModels(map[models.ModelID]models.Model{
			"valid-test-model":   {Provider: models.ProviderOpenAI, APIModel: "valid", ContextWindow: 1024},
			"invalid-test-model": invalid,
		})
		require.Error(t, err)
		require.NotContains(t, models.SupportedModels, models.ModelID("valid-test-model"))
	}
}

func TestNativeOllamaModelKeepsDottedIdentifierAndConfiguredContext(t *testing.T) {
	isolateModelConfig(t)
	t.Setenv("LOCAL_ENDPOINT", "")
	t.Setenv("LOCAL_API_KEY", "")
	dir := t.TempDir()
	path := filepath.Join(dir, ".opcode.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
  "providers":{"local":{"baseURL":"http://127.0.0.1:11434/v1"}},
  "models":{"local.llama3.1:8b":{"provider":"local","api":"ollama","api_model":"llama3.1:8b","context_window":16384,"default_max_tokens":2048}},
  "agents":{"coder":{"model":"local.llama3.1:8b"}}
}`), 0600))
	viper.SetConfigFile(path)
	loaded, err := Load(dir, false)
	require.NoError(t, err)
	require.NoError(t, loaded.ValidateStartup())
	require.Len(t, loaded.Models, 1)
	model := loaded.Models["local.llama3.1:8b"]
	require.Equal(t, "llama3.1:8b", model.APIModel)
	require.Equal(t, "ollama", model.API)
	require.EqualValues(t, 16384, model.ContextWindow)
	require.Equal(t, model.API, models.SupportedModels["local.llama3.1:8b"].API)
}

func TestLegacyConfigurationStillLoads(t *testing.T) {
	isolateModelConfig(t)
	dir := t.TempDir()
	path := filepath.Join(dir, ".opcode.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"providers":{"openai":{"apiKey":"unit-test-key"}},"agents":{"coder":{"model":"gpt-4.1-mini","maxTokens":500}}}`), 0o600))
	viper.SetConfigFile(path)
	loaded, err := Load(dir, false)
	require.NoError(t, err)
	require.Equal(t, models.GPT41Mini, loaded.Agents[AgentCoder].Model)
	require.EqualValues(t, 500, loaded.Agents[AgentCoder].MaxTokens)
	require.Empty(t, models.SupportedModels[models.GPT41Mini].API, "legacy models must keep Chat Completions")
}

func TestProjectCannotWeakenGlobalSecurity(t *testing.T) {
	isolateModelConfig(t)
	global := filepath.Join(t.TempDir(), "global.json")
	project := t.TempDir()
	require.NoError(t, os.WriteFile(global, []byte(`{"security":{"sandbox":"bubblewrap","allowNetwork":false,"envAllow":["SAFE_VARIABLE"]}}`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(project, ".opcode.json"), []byte(`{"security":{"sandbox":"none","allowNetwork":true,"envAllow":["OPENAI_API_KEY"]},"checks":{"unit":{"command":"go test ./..."}}}`), 0600))
	viper.SetConfigFile(global)
	loaded, err := Load(project, false)
	require.NoError(t, err)
	require.Equal(t, "bubblewrap", loaded.Security.Sandbox)
	require.False(t, loaded.Security.AllowNetwork)
	require.Equal(t, []string{"SAFE_VARIABLE"}, loaded.Security.EnvAllow)
	require.Equal(t, "go test ./...", loaded.Checks["unit"].Command)
}
