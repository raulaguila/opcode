package app

import (
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

type reasoningProbe struct {
	agent.Service
	busy bool
}

func (p *reasoningProbe) IsBusy() bool { return p.busy }
func (p *reasoningProbe) SelectModel(role config.AgentName, m models.Model) (models.Model, error) {
	return m, config.UpdateAgentModelDefinition(role, m)
}
func TestReasoningPersistsOnlyExplicitIdleChoice(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "settings.json")
	require.NoError(t, os.WriteFile(path, []byte(`{}`), 0600))
	oldPath := viper.ConfigFileUsed()
	viper.SetConfigFile(path)
	t.Cleanup(func() { viper.SetConfigFile(oldPath) })
	cfg := config.Get()
	if cfg == nil {
		var err error
		cfg, err = config.Load(t.TempDir(), false)
		require.NoError(t, err)
	}
	old := *cfg
	t.Cleanup(func() { *cfg = old })
	cfg.WorkingDir = t.TempDir()
	cfg.Agents = map[config.AgentName]config.Agent{config.AgentCoder: {Model: "reasoning-test", MaxTokens: 1000, ReasoningEffort: "low"}}
	cfg.Models = map[models.ModelID]models.Model{}
	cfg.Providers = map[models.ModelProvider]config.Provider{models.ProviderLocal: {BaseURL: "http://127.0.0.1:1/v1", API: "responses"}}
	model := models.Model{ID: "reasoning-test", Name: "Fixture", APIModel: "fixture", Provider: models.ProviderLocal, API: "responses", CanReason: true, ReasoningLevels: []string{"low", "high"}, ContextWindow: 8192, DefaultMaxTokens: 1000}
	previous, found := models.SupportedModels[model.ID]
	models.SupportedModels[model.ID] = model
	t.Cleanup(func() {
		if found {
			models.SupportedModels[model.ID] = previous
		} else {
			delete(models.SupportedModels, model.ID)
		}
	})
	delegate := &reasoningProbe{}
	proxy := &configurableCoder{delegate: delegate}
	require.Error(t, proxy.SetReasoning(config.AgentCoder, model.ID, "xhigh"))
	require.Equal(t, "low", cfg.Agents[config.AgentCoder].ReasoningEffort)
	delegate.busy = true
	require.Error(t, proxy.SetReasoning(config.AgentCoder, model.ID, "high"))
	delegate.busy = false
	require.NoError(t, proxy.SetReasoning(config.AgentCoder, model.ID, "high"))
	require.Equal(t, "high", cfg.Agents[config.AgentCoder].ReasoningEffort)
	connection := cfg.Providers[models.ProviderLocal]
	connection.API = "ollama"
	cfg.Providers[models.ProviderLocal] = connection
	require.Error(t, proxy.SetReasoning(config.AgentCoder, model.ID, "low"))
	require.Equal(t, "high", cfg.Agents[config.AgentCoder].ReasoningEffort)
	_, err := os.Stat(path)
	require.NoError(t, err)
}
