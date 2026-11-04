package models

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/raulaguila/opcode/internal/httpclient"
)

// InspectLocalModelMetadata drives the live /api/show enrichment for one model.
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
