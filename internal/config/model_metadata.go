package config

import (
	"context"

	"github.com/raulaguila/opcode/internal/llm/models"
)

// ModelMetadata snapshots configuration on the UI thread before cancellable I/O.
// An absent lookup means this connection has no supported per-model endpoint.
func ModelMetadata(model models.Model) func(context.Context) (models.Model, error) {
	if model.Provider != models.ProviderLocal {
		return nil
	}
	p, err := providerForModelMetadata(model)
	if err != nil || (p.API != "ollama" && model.API != "ollama") {
		return nil
	}
	explicit, overridden := cfg.Models[model.ID]
	return func(ctx context.Context) (models.Model, error) {
		resolved, err := resolveMetadataProvider(ctx, p)
		if err != nil {
			return model, err
		}
		found, err := models.InspectLocalModelMetadata(ctx, resolved.BaseURL, resolved.APIKey, model.APIModel)
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
		model.SupportsTools = found.SupportsTools
		if found.SupportsChat != nil {
			model.SupportsChat = found.SupportsChat
		}
		if found.Hosting != "" {
			model.Hosting = found.Hosting
		}
		return providerModelDefaults(p, model), nil
	}
}

// Resolution belongs to the asynchronous lookup, never to UI snapshot creation.
func resolveMetadataProvider(ctx context.Context, p Provider) (Provider, error) {
	if err := ctx.Err(); err != nil {
		return Provider{}, err
	}
	if p.APIKeyRef != "" && p.APIKey == "" {
		var err error
		p, err = resolveProvider(models.ProviderLocal, p)
		if err != nil {
			return Provider{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return Provider{}, err
	}
	return p, nil
}
