package config

import (
	"fmt"
	"maps"
	"math"

	"github.com/raulaguila/opcode/internal/llm/models"
)

func registerModels(definitions map[models.ModelID]models.Model) error {
	validated := make(map[models.ModelID]models.Model, len(definitions))
	for id, model := range definitions {
		normalized, err := normalizeModel(id, model)
		if err != nil {
			return err
		}
		if cfg != nil && normalized.ContextAuto {
			normalized = ModelWithProviderDefaults(normalized)
		}
		validated[id] = normalized
	}
	// Publish only after every definition validates, so a failed load is retryable.
	maps.Copy(models.SupportedModels, validated)
	return nil
}

func normalizeModel(id models.ModelID, model models.Model) (models.Model, error) {
	if id == "" || model.APIModel == "" || model.ContextWindow < 0 {
		return models.Model{}, fmt.Errorf("model %q requires api_model and a nonnegative context_window (omit it for automatic discovery)", id)
	}
	model = automaticModelContext(id, model)
	if err := validateModelProvider(id, model); err != nil {
		return models.Model{}, err
	}
	if model.ThinkingMode != "" && (model.Provider != models.ProviderAnthropic && model.Provider != models.ProviderBedrock || model.ThinkingMode != "enabled" && model.ThinkingMode != "adaptive") {
		return models.Model{}, fmt.Errorf("model %q has unsupported thinking_mode %q", id, model.ThinkingMode)
	}
	for _, cost := range []float64{model.CostPer1MIn, model.CostPer1MOut, model.CostPer1MInCached, model.CostPer1MOutCached} {
		if cost < 0 || math.IsNaN(cost) || math.IsInf(cost, 0) {
			return models.Model{}, fmt.Errorf("model %q has an invalid token price", id)
		}
	}
	if model.DefaultMaxTokens < 0 || model.DefaultMaxTokens > model.ContextWindow {
		return models.Model{}, fmt.Errorf("model %q has an invalid default_max_tokens", id)
	}
	model.ID = id
	if model.Name == "" {
		model.Name = string(id)
	}
	if model.DefaultMaxTokens == 0 {
		model.DefaultMaxTokens = min(MaxTokensFallbackDefault, model.ContextWindow)
	}
	return model, nil
}

func validateModelProvider(id models.ModelID, model models.Model) error {
	switch model.Provider {
	case models.ProviderLocal:
		if model.API != "" && model.API != "chat_completions" && model.API != "responses" && model.API != "ollama" {
			return fmt.Errorf("model %q has unsupported API %q", id, model.API)
		}
	case models.ProviderOpenAI, models.ProviderAzure, models.ProviderOpenRouter, models.ProviderGROQ, models.ProviderXAI:
		if model.API != "" && model.API != "chat_completions" && model.API != "responses" {
			return fmt.Errorf("model %q has unsupported API %q", id, model.API)
		}
	case models.ProviderAnthropic, models.ProviderBedrock, models.ProviderGemini, models.ProviderVertexAI, models.ProviderCopilot:
		if model.API != "" {
			return fmt.Errorf("model %q: API selection is only supported for OpenAI-compatible providers", id)
		}
	default:
		return fmt.Errorf("model %q has unsupported provider %q", id, model.Provider)
	}
	if model.ProviderProfile != "" {
		if cfg == nil {
			return fmt.Errorf("model %q: configuration is not loaded", id)
		}
		profile, ok := cfg.ProviderProfiles[model.ProviderProfile]
		if !ok || profile.Backend != model.Provider {
			return fmt.Errorf("model %q: missing or incompatible provider_profile", id)
		}
	}
	return nil
}

func automaticModelContext(id models.ModelID, model models.Model) models.Model {
	if model.ContextWindow == 0 {
		model.ID = id
		model.ContextAuto = true
		model.ContextWindow, model.ContextEstimated, model.ContextSource = 4096, true, "fallback"
		if known, ok := models.BuiltinMetadata(model); ok && known.ContextWindow > 0 {
			model.ContextWindow, model.ContextEstimated, model.ContextSource = known.ContextWindow, false, "catalog"
		}
		if model.DefaultMaxTokens > 0 {
			model.DefaultMaxTokens = min(model.DefaultMaxTokens, model.ContextWindow/2)
		}
	}
	return model
}
