package config

import (
	"context"
	"github.com/raulaguila/opcode/internal/llm/models"
)

// ModelMetadata snapshots configuration on the UI thread before cancellable I/O.
// An absent lookup means this connection has no supported per-model endpoint.
func ModelMetadata(model models.Model) func(context.Context) (models.Model, error) {
	p, err := ProviderForModel(model)
	if err != nil || model.Provider != models.ProviderLocal || (p.API != "ollama" && model.API != "ollama") {
		return nil
	}
	explicit, overridden := cfg.Models[model.ID]
	return func(ctx context.Context) (models.Model, error) {
		found, err := models.InspectLocalModelMetadata(ctx, p.BaseURL, p.APIKey, model.APIModel)
		if err != nil {
			return model, err
		}
		if !overridden || explicit.UsesAutomaticContext() {
			model.ContextWindow = found.ContextWindow
			model.ContextEstimated = found.ContextEstimated
			model.ContextAuto = true
			model.ContextSource = found.ContextSource
			model.DefaultMaxTokens = min(model.DefaultMaxTokens, model.ContextWindow/2)
		}
		return providerModelDefaults(p, model), nil
	}
}
