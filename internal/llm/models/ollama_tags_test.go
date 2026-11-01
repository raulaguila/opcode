package models

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOllamaTagsEstimateContextAndHideEmbeddingModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/tags", r.URL.Path)
		w.Write([]byte(`{"models":[{"name":"chat:7b","details":{"families":["gemma4"],"context_length":65536}},{"name":"nomic-embed:latest","details":{"families":["nomic-bert-moe"],"context_length":512}}]}`))
	}))
	defer server.Close()
	found := []localModel{{ID: "chat:7b"}, {ID: "nomic-embed"}}
	applyOllamaTags(context.Background(), server.Client(), server.URL+"/v1", "", found)

	chat := convertLocalModel(found[0])
	require.Equal(t, int64(65536), chat.ContextWindow)
	require.True(t, chat.ContextEstimated, "the tags maximum ignores Modelfile num_ctx")
	require.False(t, chat.EmbeddingOnly())
	require.True(t, convertLocalModel(found[1]).EmbeddingOnly())
}

func TestQuickLocalDiscoveryAppliesOllamaTags(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Write([]byte(`{"data":[{"id":"chat:7b"},{"id":"nomic-embed:latest"}]}`))
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"chat:7b","details":{"families":["llama"],"context_length":131072}},{"name":"nomic-embed:latest","details":{"families":["nomic-bert"],"context_length":512}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	found, err := DiscoverLocalModels(WithoutCatalogMetadata(context.Background()), server.URL+"/v1", "")
	require.NoError(t, err)
	require.Len(t, found, 2)
	require.Equal(t, int64(131072), found[0].ContextWindow)
	require.True(t, found[1].EmbeddingOnly())
}
