package config

import (
	"context"
	"fmt"
	"maps"
	"net/url"
	"slices"

	"github.com/raulaguila/opcode/internal/llm/models"
)

func configureProviders() error {
	for id, p := range cfg.Providers {
		resolved, err := resolveProvider(id, p)
		if err != nil {
			return fmt.Errorf("provider %s: %w", id, err)
		}
		cfg.Providers[id] = resolved
	}
	for id, profile := range cfg.ProviderProfiles {
		if err := validateProfileID(id); err != nil {
			return err
		}
		resolved, err := resolveProvider(profile.Backend, profile.Provider)
		if err != nil {
			return fmt.Errorf("provider profile %s: %w", id, err)
		}
		profile.Provider = resolved
		cfg.ProviderProfiles[id] = profile
	}
	return nil
}

func resolveProvider(backend models.ModelProvider, p Provider) (Provider, error) {
	if p.Disabled {
		return p, nil
	}
	if !knownBackend(backend) {
		return Provider{}, fmt.Errorf("unsupported backend %q", backend)
	}
	if p.BaseURL != "" {
		if backend == models.ProviderLocal {
			endpoint, err := models.NormalizeLocalEndpoint(p.BaseURL)
			if err != nil {
				return Provider{}, err
			}
			p.BaseURL = endpoint
		} else {
			u, err := url.Parse(p.BaseURL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
				return Provider{}, fmt.Errorf("baseURL must be an HTTP API URL without credentials or query")
			}
			if models.CatalogEndpoint(backend) == "" {
				return Provider{}, fmt.Errorf("backend uses its platform-specific endpoint settings")
			}
		}
	}
	if p.APIKeyEnv != "" {
		if !validEnvironmentName(p.APIKeyEnv) {
			return Provider{}, fmt.Errorf("invalid apiKeyEnv name")
		}
		p.APIKey = Environment(p.APIKeyEnv)
		if p.APIKey == "" {
			return Provider{}, fmt.Errorf("environment variable %s is not set", p.APIKeyEnv)
		}
	}
	probe := models.Model{Provider: backend, API: p.API, APIModel: "validation", ContextWindow: 4096, DefaultMaxTokens: 2048}
	if p.ContextWindow == 0 {
		probe.ContextWindow = max(probe.ContextWindow, p.DefaultMaxTokens)
	}
	if p.ContextWindow != 0 {
		probe.ContextWindow = p.ContextWindow
		probe.DefaultMaxTokens = min(2048, p.ContextWindow/2)
	}
	if p.DefaultMaxTokens != 0 {
		probe.DefaultMaxTokens = p.DefaultMaxTokens
	}
	if _, err := normalizeModel("provider-default", probe); err != nil {
		return Provider{}, fmt.Errorf("invalid model defaults: %w", err)
	}
	return p, nil
}

// ModelCatalog captures settings and metadata on the caller's thread. Its
// network function never reads/writes the shared configuration or registry.
func ModelCatalog(id models.ModelProvider) func(context.Context) ([]models.Model, error) {
	p := cfg.Providers[id]
	return modelCatalog(id, "", p)
}

func modelCatalog(id models.ModelProvider, profile string, p Provider) func(context.Context) ([]models.Model, error) {
	known := maps.Clone(models.SupportedModels)
	if profile != "" {
		for _, model := range ConfiguredProfileModels(profile) {
			known[model.ID] = model
		}
	}
	explicit := maps.Clone(cfg.Models)
	selected := known[cfg.Agents[AgentCoder].Model]
	// Preserve existing Ollama installations that configured the native protocol
	// per model before provider defaults existed.
	if id == models.ProviderLocal && p.API == "" && selected.Provider == id && selected.ProviderProfile == profile {
		p.API = selected.API
	}
	return func(ctx context.Context) ([]models.Model, error) {
		if p.Disabled {
			return nil, fmt.Errorf("provider disabled")
		}
		found, err := models.DiscoverProviderModels(ctx, id, p.BaseURL, p.APIKey)
		if err != nil {
			return nil, err
		}
		byWire := map[string]models.Model{}
		// Stable preference: the selected definition, then explicit, then built-in.
		keys := slices.Sorted(maps.Keys(known))
		for _, key := range keys {
			model := known[key]
			if model.Provider == id && model.ProviderProfile == profile {
				if _, ok := byWire[model.APIModel]; !ok {
					byWire[model.APIModel] = model
				}
			}
		}
		for _, key := range slices.Sorted(maps.Keys(explicit)) {
			model := known[key]
			if model.Provider == id && model.ProviderProfile == profile {
				byWire[model.APIModel] = model
			}
		}
		if selected.Provider == id && selected.ProviderProfile == profile {
			byWire[selected.APIModel] = selected
		}
		for i, model := range found {
			model = providerModelDefaults(p, model)
			if existing, ok := byWire[model.APIModel]; ok {
				if existing.ContextAuto {
					if !model.ContextEstimated || existing.ContextEstimated {
						existing.ContextWindow = model.ContextWindow
						existing.ContextEstimated = model.ContextEstimated
						existing.ContextSource = model.ContextSource
					} else {
						existing.ContextSource = "cache"
					}
					existing.DefaultMaxTokens = min(existing.DefaultMaxTokens, existing.ContextWindow/2)
				}
				model = existing
				if _, overridden := explicit[existing.ID]; !overridden || existing.ContextAuto {
					model = providerModelDefaults(p, model)
				}
			}
			if profile != "" && model.ProviderProfile == "" {
				model.ID = models.ModelID("profile." + profile + "." + model.APIModel)
				model.ProviderProfile = profile
			}
			found[i] = model
		}
		return found, nil
	}
}

func providerModelDefaults(p Provider, model models.Model) models.Model {
	if p.API != "" {
		model.API = p.API
	}
	if p.ContextWindow > 0 {
		model.ContextWindow = p.ContextWindow
		model.ContextEstimated = false
		model.ContextSource = "configuration"
	}
	if p.DefaultMaxTokens > 0 {
		model.DefaultMaxTokens = p.DefaultMaxTokens
	}
	model.DefaultMaxTokens = min(model.DefaultMaxTokens, model.ContextWindow/2)
	return model
}

func ModelWithProviderDefaults(model models.Model) models.Model {
	if configured, explicit := cfg.Models[model.ID]; explicit && !configured.ContextAuto {
		return model
	}
	p, err := ProviderForModel(model)
	if err != nil {
		return model
	}
	return providerModelDefaults(p, model)
}

// Refresh selected local metadata after restart, including named profiles. The
// saved definition remains a usable offline cache; manual model limits win.
func refreshLocalContexts() {
	connections := map[string]bool{}
	for _, agent := range cfg.Agents {
		model, ok := cfg.Models[agent.Model]
		if !ok || model.Provider != models.ProviderLocal || !model.ContextAuto || connections[model.ProviderProfile] {
			continue
		}
		p, err := ProviderForModel(model)
		if err != nil {
			continue
		}
		connections[model.ProviderProfile] = true
		found, err := modelCatalog(model.Provider, model.ProviderProfile, p)(context.Background())
		if err != nil {
			continue
		}
		for _, fresh := range found {
			if existing, ok := cfg.Models[fresh.ID]; ok && existing.ContextAuto {
				cfg.Models[fresh.ID] = fresh
				models.SupportedModels[fresh.ID] = fresh
			}
		}
	}
}
