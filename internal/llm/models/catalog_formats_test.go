package models

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompatibleCatalogFormatsAcrossLocalAndOpenAIBackends(t *testing.T) {
	for _, tc := range []struct {
		name, body     string
		window, output int64
	}{
		{"vllm", `{"data":[{"id":"alias","max_model_len":131072}]}`, 131072, 0},
		{"context alias", `{"data":[{"id":"alias","max_context_length":"131072"}]}`, 131072, 0},
		{"gateway nested", `{"data":[{"id":"alias","openai":{"id":"different-wire-id","max_model_len":131072}}]}`, 131072, 0},
		{"models envelope", `{"models":[{"model":"alias","context_window":"65536","max_output_tokens":"8192"}]}`, 65536, 8192},
		{"bare array", `[{"name":"alias","contextLength":32768,"maxOutputTokens":2048}]`, 32768, 2048},
		{"items envelope", `{"items":[{"model_id":"alias","limits":{"context_size":64000,"output_token_limit":4096}}]}`, 64000, 4096},
		{"data wrapper", `{"data":{"models":[{"id":"alias","metadata":{"contextWindow":"128000"}}]}}`, 128000, 0},
		{"result wrapper", `{"result":{"data":[{"id":"alias","info":{"meta":{"max_context_tokens":"98304"}}}]}}`, 98304, 0},
		{"litellm metadata", `{"data":[{"model_name":"alias","model_info":{"max_input_tokens":60000,"max_output_tokens":1000}}]}`, 60000, 1000},
		{"output is not context", `{"data":[{"id":"alias","max_output_tokens":2048}]}`, 4096, 2048},
		{"invalid top valid nested", `{"data":[{"id":"alias","context_length":-1,"max_model_len":false,"openai":{"max_model_len":"131072"}}]}`, 131072, 0},
		{"invalid values stay unknown", `{"data":[{"id":"alias","context_length":"128k","max_model_len":9223372036854775808,"max_context_length":1.5,"loaded_context_length":-3}]}`, 4096, 0},
		{"string IDs", `{"result":["alias"]}`, 4096, 0},
	} {
		for _, backend := range []ModelProvider{ProviderLocal, ProviderOpenAI} {
			t.Run(tc.name+"/"+string(backend), func(t *testing.T) {
				catalogTransport(t, func(req *http.Request) (*http.Response, error) {
					require.Equal(t, "/gateway/v1/models", req.URL.Path)
					return catalogResponse(req, 200, tc.body), nil
				})
				found, err := DiscoverProviderModels(WithoutCatalogMetadata(context.Background()), backend, "https://gateway.invalid/gateway/v1", "")
				require.NoError(t, err)
				require.Len(t, found, 1)
				require.Equal(t, "alias", found[0].APIModel)
				require.EqualValues(t, tc.window, found[0].ContextWindow)
				if tc.output > 0 {
					require.EqualValues(t, tc.output, found[0].DefaultMaxTokens)
				}
				unknown := strings.Contains(tc.name, "unknown") || tc.name == "string IDs" || tc.name == "output is not context"
				require.Equal(t, unknown, found[0].ContextEstimated)
				require.Equal(t, !unknown, found[0].ContextSource == "provider")
			})
		}
	}
}

func TestGatewayPhotoShapeUsesAdvertisedLimitWithoutAssumingUICapabilities(t *testing.T) {
	body, err := os.ReadFile("testdata/gateway-models.json")
	require.NoError(t, err)
	for _, backend := range []ModelProvider{ProviderLocal, ProviderOpenAI} {
		t.Run(string(backend), func(t *testing.T) {
			catalogTransport(t, func(req *http.Request) (*http.Response, error) { return catalogResponse(req, 200, string(body)), nil })
			found, err := DiscoverProviderModels(WithoutCatalogMetadata(context.Background()), backend, "http://gateway.invalid/prefix/v1", "")
			require.NoError(t, err)
			require.Len(t, found, 1)
			require.EqualValues(t, 131072, found[0].ContextWindow)
			require.False(t, found[0].ContextEstimated)
			require.True(t, found[0].ContextAuto)
			require.Equal(t, "team-model-alias", found[0].APIModel)
			require.Nil(t, found[0].SupportsTools)
			require.False(t, found[0].SupportsAttachments)
		})
	}
}

func TestCatalogEnvelopesRemainBoundedAndRejectErrorObjects(t *testing.T) {
	for _, body := range []string{`{"error":{"message":"private-server-body"}}`, `{"data":{"data":{"data":{"data":[]}}}}`, `{"data":{}}`, `null`, `not json`} {
		_, err := decodeCatalogPage([]byte(body))
		require.Error(t, err)
		require.NotContains(t, err.Error(), "private-server-body")
	}
	page, err := decodeCatalogPage([]byte(`{"data":{"models":[{"id":"alias","n_ctx":16000}]},"has_more":true,"last_id":"alias"}`))
	require.NoError(t, err)
	require.True(t, page.HasMore)
	require.Equal(t, "alias", page.LastID)
	entries := make([]string, 5001)
	for i := range entries {
		entries[i] = fmt.Sprintf(`{"id":"model-%d","context_window":10000}`, i)
	}
	page, err = decodeCatalogPage([]byte("[" + strings.Join(entries, ",") + "]"))
	require.NoError(t, err)
	require.Len(t, page.Data, 5000)
}
