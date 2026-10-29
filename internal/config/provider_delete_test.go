package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func deletionConfig(t *testing.T) (string, string) {
	t.Helper()
	isolateModelConfig(t)
	root := t.TempDir()
	global, project := filepath.Join(t.TempDir(), "config.json"), filepath.Join(root, ".opcode.json")
	viper.SetConfigFile(global)
	cfg = &Config{WorkingDir: root, ProviderProfiles: map[string]ProviderProfile{"office": {Backend: models.ProviderLocal}}, Models: map[models.ModelID]models.Model{}, Agents: map[AgentName]Agent{}}
	return global, project
}

func TestDeleteProviderRemovesBothLayersCredentialsAndModelsWithoutResurrection(t *testing.T) {
	global, project := deletionConfig(t)
	for _, path := range []string{global, project} {
		require.NoError(t, os.WriteFile(path, []byte(`{"providerProfiles":{"office":{"backend":"local","baseURL":"http://offline.invalid/v1","apiKey":"fake-deleted-key","extension":true},"keep":{"backend":"local","disabled":true}},"models":{"profile.office.custom":{"provider":"local","provider_profile":"office","api_model":"custom"}},"future":{"keep":9007199254740993}}`), 0600))
	}
	m := models.Model{ID: "profile.office.custom", Provider: models.ProviderLocal, ProviderProfile: "office", APIModel: "custom"}
	cfg.Models[m.ID], models.SupportedModels[m.ID] = m, m
	require.NoError(t, DeleteProvider("office", false))
	for _, path := range []string{global, project} {
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		require.NotContains(t, string(raw), "fake-deleted-key")
		require.NotContains(t, string(raw), `"office"`)
		require.Contains(t, string(raw), "9007199254740993")
		var document Config
		require.NoError(t, json.Unmarshal(raw, &document))
		require.Contains(t, document.ProviderProfiles, "keep")
		require.NotContains(t, document.Models, m.ID)
		info, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	}
	require.NotContains(t, cfg.ProviderProfiles, "office")
	require.NotContains(t, cfg.Models, m.ID)
	require.NotContains(t, models.SupportedModels, m.ID)
}

func TestDeleteProviderRejectsAgentsInEitherLayerAndInvalidJSONBeforeWriting(t *testing.T) {
	global, project := deletionConfig(t)
	initial := []byte(`{"providerProfiles":{"office":{"backend":"local"}},"future":true}`)
	require.NoError(t, os.WriteFile(global, initial, 0600))
	require.NoError(t, os.WriteFile(project, []byte(`{"agents":{"title":{"model":"profile.office.saved"}}}`), 0600))
	require.ErrorContains(t, DeleteProvider("office", false), "title")
	raw, err := os.ReadFile(global)
	require.NoError(t, err)
	require.Equal(t, initial, raw)
	require.Contains(t, cfg.ProviderProfiles, "office")
	require.NoError(t, os.WriteFile(project, []byte(`invalid`), 0600))
	require.ErrorContains(t, DeleteProvider("office", false), "invalid configuration")
	raw, err = os.ReadFile(global)
	require.NoError(t, err)
	require.Equal(t, initial, raw)
	require.Contains(t, cfg.ProviderProfiles, "office")
}

func TestDeleteLegacyProviderLeavesNoCredentialAndCannotBeRecreatedFromEnvironment(t *testing.T) {
	global, _ := deletionConfig(t)
	t.Setenv("OPENAI_API_KEY", "fake-environment-key")
	require.NoError(t, os.WriteFile(global, []byte(`{"providers":{"openai":{"apiKey":"fake-literal-key","futureCredential":"fake-extension-secret"}},"future":true}`), 0600))
	cfg.Providers = map[models.ModelProvider]Provider{models.ProviderOpenAI: {APIKey: "fake-literal-key"}}
	cfg.Agents[AgentCoder] = Agent{Model: models.GPT41}
	require.ErrorContains(t, DeleteProvider("openai", true), "coder")
	delete(cfg.Agents, AgentCoder)
	require.NoError(t, DeleteProvider("openai", true))
	raw, err := os.ReadFile(global)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "fake-literal-key")
	require.NotContains(t, string(raw), "fake-environment-key")
	require.NotContains(t, string(raw), "fake-extension-secret")
	require.Contains(t, string(raw), `"future": true`)
	var file Config
	require.NoError(t, json.Unmarshal(raw, &file))
	require.Equal(t, Provider{Disabled: true}, file.Providers[models.ProviderOpenAI])
	cfg.Providers = file.Providers
	require.NoError(t, configureProviders())
	require.Equal(t, Provider{Disabled: true}, cfg.Providers[models.ProviderOpenAI])
}

func TestDeleteProviderRestoresFirstLayerIfSecondLayerCannotBeWritten(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission failure injection requires a non-root user")
	}
	global, project := deletionConfig(t)
	raw := []byte(`{"providerProfiles":{"office":{"backend":"local"}},"unknown":9007199254740993}`)
	for _, path := range []string{global, project} {
		require.NoError(t, os.WriteFile(path, raw, 0600))
	}
	root := filepath.Dir(project)
	require.NoError(t, os.Chmod(root, 0500))
	t.Cleanup(func() { _ = os.Chmod(root, 0700) })
	require.Error(t, DeleteProvider("office", false))
	for _, path := range []string{global, project} {
		current, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, raw, current, "failed deletion must preserve both layers byte-for-byte")
	}
	require.Contains(t, cfg.ProviderProfiles, "office")
}
