package config

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/raulaguila/opcode/internal/llm/models"
)

func configureLocalProvider() error {
	local := cfg.Providers[models.ProviderLocal]
	if local.Disabled {
		return nil
	}
	endpoint := os.Getenv("LOCAL_ENDPOINT")
	if endpoint == "" {
		endpoint = local.BaseURL
	}
	needsDiscovery := cfg.Agents[AgentCoder].Model == ""
	usesLocal := false
	for _, agent := range cfg.Agents {
		model, exists := models.SupportedModels[agent.Model]
		if model.ProviderProfile != "" {
			continue
		}
		if model.Provider == models.ProviderLocal || strings.HasPrefix(string(agent.Model), "local.") {
			usesLocal = true
			needsDiscovery = needsDiscovery || !exists
		}
	}
	if endpoint == "" && !usesLocal {
		return nil
	}
	baseURL, err := models.NormalizeLocalEndpoint(endpoint)
	if err != nil {
		return err
	}
	local.BaseURL = baseURL
	cfg.Providers[models.ProviderLocal] = local
	if !needsDiscovery {
		return nil
	}
	found, err := models.DiscoverLocalModels(context.Background(), baseURL, local.APIKey)
	if err != nil {
		return fmt.Errorf("local provider: %w", err)
	}
	for _, model := range found {
		if _, explicit := cfg.Models[model.ID]; !explicit {
			models.SupportedModels[model.ID] = providerModelDefaults(local, model)
		}
	}
	if cfg.Agents == nil {
		cfg.Agents = make(map[AgentName]Agent)
	}
	if cfg.Agents[AgentCoder].Model == "" {
		cfg.Agents[AgentCoder] = Agent{Model: found[0].ID}
	}
	// Never silently switch to a remote model if an explicitly chosen local
	// model was not returned by the local server.
	for name, agent := range cfg.Agents {
		if strings.HasPrefix(string(agent.Model), "local.") {
			if _, exists := models.SupportedModels[agent.Model]; !exists {
				return fmt.Errorf("local model configured for agent %s is unavailable; choose an installed model returned by /v1/models", name)
			}
		}
	}
	return nil
}
