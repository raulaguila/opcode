package models

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPickerCapabilitiesUseExplicitProviderEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, metadata string
		embeddingOnly  bool
		chatKnown      bool
	}{
		{"missing", `{}`, false, false},
		{"empty", `{"capabilities":[]}`, false, false},
		{"unrecognized", `{"capabilities":["future"]}`, false, false},
		{"embedding", `{"capabilities":["embedding"]}`, true, true},
		{"mixed", `{"capabilities":["embedding","completion"]}`, false, true},
		{"chat without tools", `{"capabilities":["completion"]}`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var details ollamaMetadata
			require.NoError(t, json.Unmarshal([]byte(tc.metadata), &details))
			local := localModel{ID: "name-embedding-cloud"}
			applyOllamaMetadata(&local, details)
			model := convertLocalModel(local)
			require.Equal(t, tc.embeddingOnly, model.EmbeddingOnly())
			require.Equal(t, tc.chatKnown, model.SupportsChat != nil)
			require.Empty(t, model.Hosting, "names never imply hosting")
		})
	}
}

func TestPickerHostingUsesExplicitProviderEvidenceAndSurvivesCache(t *testing.T) {
	for _, tc := range []struct{ name, metadata, hosting string }{
		{"missing", `{}`, ""},
		{"local", `{"remote_host":""}`, "local"},
		{"cloud", `{"remote_host":"https://ollama.com"}`, "cloud"},
		{"unknown upstream", `{"remote_host":"http://office.invalid"}`, ""},
		{"lookalike host", `{"remote_host":"https://ollama.com.invalid"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var details ollamaMetadata
			require.NoError(t, json.Unmarshal([]byte(tc.metadata), &details))
			local := localModel{ID: "fixture"}
			applyOllamaMetadata(&local, details)
			require.Equal(t, tc.hosting, convertLocalModel(local).Hosting)
			unsupported := false
			local.SupportsChat = &unsupported
			identity := ollamaMetadataCacheKey{endpoint: "picker-fixture", model: tc.name}
			cacheOllamaMetadata(identity, local)
			var cached localModel
			require.True(t, cachedOllamaMetadata(identity, &cached))
			require.True(t, convertLocalModel(cached).EmbeddingOnly())
			require.Equal(t, tc.hosting, cached.Hosting)
			raw, err := json.Marshal(convertLocalModel(local))
			require.NoError(t, err)
			require.NotContains(t, string(raw), "SupportsChat", "runtime evidence must not grow saved model schema")
			require.NotContains(t, string(raw), "supports_chat")
			require.NotContains(t, string(raw), "Hosting")
			require.NotContains(t, string(raw), "hosting")
		})
	}
}

func TestEmbeddingMetadataWithoutContextStillRejectsChatSelection(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	http.DefaultTransport = discoveryTransport(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, "/api/show", req.URL.Path)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"capabilities":["embedding"]}`)), Request: req}, nil
	})
	model, err := InspectLocalModelMetadata(context.Background(), "http://fixture.invalid/v1", "", "fixture")
	require.NoError(t, err)
	require.True(t, model.EmbeddingOnly())
	require.True(t, model.ContextEstimated)
}

func TestCompatibleOptionalMetadataShapesPreserveModel(t *testing.T) {
	for _, tc := range []struct{ name, capabilities, host string }{
		{"object", `{"embedding":true}`, `{}`},
		{"string", `"embedding"`, `42`},
		{"null", `null`, `null`},
		{"mixed array", `["embedding",null]`, `false`},
		{"unknown array", `["future"]`, `[]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalogTransport(t, func(req *http.Request) (*http.Response, error) {
				body := `{"data":[{"id":"fixture","context_length":32768,"capabilities":` + tc.capabilities + `,"remote_host":` + tc.host + `}]}`
				return catalogResponse(req, 200, body), nil
			})
			found, err := DiscoverLocalModels(WithoutCatalogMetadata(context.Background()), "http://fixture.invalid/prefix/v1", "")
			require.NoError(t, err)
			require.Len(t, found, 1)
			require.EqualValues(t, 32768, found[0].ContextWindow)
			require.Nil(t, found[0].SupportsChat)
			require.Empty(t, found[0].Hosting)
		})
	}
}

func TestOllamaUnknownOptionalShapesPreserveContext(t *testing.T) {
	for _, capabilities := range []string{`{"embedding":true}`, `"embedding"`, `null`, `["embedding",null]`} {
		t.Run(capabilities, func(t *testing.T) {
			catalogTransport(t, func(req *http.Request) (*http.Response, error) {
				require.Equal(t, "/api/show", req.URL.Path)
				body := `{"capabilities":` + capabilities + `,"remote_host":{},"model_info":{"general.architecture":"llama","llama.context_length":32768}}`
				return catalogResponse(req, 200, body), nil
			})
			model, err := InspectLocalModelMetadata(context.Background(), "http://fixture.invalid/v1", "", "fixture")
			require.NoError(t, err)
			require.EqualValues(t, 32768, model.ContextWindow)
			require.Nil(t, model.SupportsChat)
			require.Nil(t, model.SupportsTools)
			require.Empty(t, model.Hosting)
		})
	}
}
