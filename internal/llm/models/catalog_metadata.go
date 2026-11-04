package models

import (
	"context"
)

type catalogMetadataKey struct{}

// WithoutCatalogMetadata lists names quickly; selected metadata is read separately.
// Startup discovery keeps its existing full metadata behavior.
func WithoutCatalogMetadata(ctx context.Context) context.Context {
	return context.WithValue(ctx, catalogMetadataKey{}, false)
}
