package models

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/raulaguila/opcode/internal/httpclient"
)

type catalogMetadataKey struct{}

// WithoutCatalogMetadata lists names quickly; selected metadata is read separately.
// Startup discovery keeps its existing full metadata behavior.
func WithoutCatalogMetadata(ctx context.Context) context.Context {
	return context.WithValue(ctx, catalogMetadataKey{}, false)
}

// InspectLocalModelMetadata performs only /api/show for this model. It never loads
// weights, generates tokens, follows redirects, or publishes to the shared registry.
func InspectLocalModelMetadata(ctx context.Context, endpoint, key, id string) (Model, error) {
	endpoint, err := NormalizeLocalEndpoint(endpoint)
	if err != nil {
		return Model{}, err
	}
	if !validCatalogID(id) {
		return Model{}, fmt.Errorf("invalid model ID")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	found := []localModel{{ID: id}}
	client := &http.Client{Transport: httpclient.Transport(ctx, http.DefaultTransport), CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	enrichOllamaContexts(ctx, client, endpoint, key, found, nil)
	if err := ctx.Err(); err != nil {
		return Model{}, err
	}
	if found[0].MaxContextLength <= 0 && found[0].SupportsTools == nil && found[0].Hosting == "" {
		return Model{}, fmt.Errorf("model metadata unavailable")
	}
	return convertLocalModel(found[0]), nil
}
