package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/stretchr/testify/require"
)

func TestReasoningChooserOnlyEditsActiveAdvertisedModel(t *testing.T) {
	settingsDialog(t)
	cfg := config.Get()
	oldProviders := cfg.Providers
	cfg.Providers = map[models.ModelProvider]config.Provider{models.ProviderLocal: {BaseURL: "http://127.0.0.1:1/v1", API: "responses"}}
	t.Cleanup(func() { cfg.Providers = oldProviders })
	oldAgents := cfg.Agents
	cfg.Agents = map[config.AgentName]config.Agent{config.AgentCoder: {Model: "reasoning-fixture", ReasoningEffort: "low"}}
	old, exists := models.SupportedModels["reasoning-fixture"]
	t.Cleanup(func() {
		cfg.Agents = oldAgents
		if exists {
			models.SupportedModels["reasoning-fixture"] = old
		} else {
			delete(models.SupportedModels, "reasoning-fixture")
		}
	})
	models.SupportedModels["reasoning-fixture"] = models.Model{ID: "reasoning-fixture", Provider: models.ProviderLocal, CanReason: true, API: "responses", ReasoningLevels: []string{"low", "high"}}
	m := NewModelDialogCmp().(*modelDialogCmp)
	m.target = config.AgentCoder
	m.reasoningUpdate(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	require.True(t, m.reasoningOpen)
	m.reasoningUpdate(tea.KeyPressMsg{Code: tea.KeyDown})
	cmd, handled := m.reasoningUpdate(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.True(t, handled)
	require.Equal(t, "high", cmd().(ReasoningSelectedMsg).Effort)
	require.Equal(t, "low", cfg.Agents[config.AgentCoder].ReasoningEffort, "a highlighted choice must not persist before application handles confirmation")
	changed := models.SupportedModels["reasoning-fixture"]
	changed.ReasoningLevels = []string{"low"}
	models.SupportedModels["reasoning-fixture"] = changed
	cmd, _ = m.reasoningUpdate(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, "low", cmd().(ReasoningSelectedMsg).Effort)
	m.reasoningUpdate(tea.KeyPressMsg{Code: tea.KeyEsc})
	require.False(t, m.reasoningOpen)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	require.Equal(t, 60, m.width)
}
