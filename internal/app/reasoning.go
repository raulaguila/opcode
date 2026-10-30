package app

import (
	"fmt"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"slices"
)

func (p *configurableCoder) SetReasoning(role config.AgentName, id models.ModelID, effort string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.delegate == nil || p.delegate.IsBusy() {
		return fmt.Errorf("wait until the agent is idle")
	}
	cfg := config.Get()
	old, ok := cfg.Agents[role]
	model := config.ModelWithProviderDefaults(models.SupportedModels[id])
	if !ok || old.Model != id || !model.CanReason || !slices.Contains(model.ReasoningChoices(), effort) {
		return fmt.Errorf("reasoning choice is not supported by the active model")
	}
	switch model.Provider {
	case models.ProviderOpenAI, models.ProviderAzure, models.ProviderLocal, models.ProviderGROQ, models.ProviderOpenRouter, models.ProviderXAI:
	default:
		return fmt.Errorf("provider does not expose a supported effort parameter")
	}
	connection, connectionErr := config.ProviderForModel(model)
	if connectionErr != nil {
		return connectionErr
	}
	if model.API == "ollama" || connection.API == "ollama" {
		return fmt.Errorf("ollama effort controls are not advertised by this adapter")
	}
	candidate := old
	candidate.ReasoningEffort = effort
	cfg.Agents[role] = candidate
	if _, err := p.delegate.SelectModel(role, model); err != nil {
		cfg.Agents[role] = old
		return err
	}
	return nil
}
