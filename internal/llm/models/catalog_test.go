package models

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func catalogTransport(t *testing.T, f discoveryTransport) {
	t.Helper()
	previous := http.DefaultTransport
	http.DefaultTransport = f
	t.Cleanup(func() { http.DefaultTransport = previous })
}
func catalogResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}
}
func TestCatalogAuthenticationFilteringAndPagination(t *testing.T) {
	for _, provider := range []ModelProvider{ProviderOpenAI, ProviderAnthropic, ProviderGemini} {
		t.Run(string(provider), func(t *testing.T) {
			calls := 0
			catalogTransport(t, func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, "/prefix/models", req.URL.Path)
				require.NotContains(t, req.URL.String(), "private-key")
				body := `{"data":[{"id":"gpt-new"},{"id":"text-embedding-3-small"},{"id":"gpt-image-1"},{"id":"gpt-new"},{"id":"bad\u001b[31m"}]}`
				switch provider {
				case ProviderOpenAI:
					require.Equal(t, "Bearer private-key", req.Header.Get("Authorization"))
				case ProviderAnthropic:
					require.Equal(t, "private-key", req.Header.Get("x-api-key"))
					require.Equal(t, "2023-06-01", req.Header.Get("anthropic-version"))
					if calls == 1 {
						body = `{"data":[{"id":"claude-new","display_name":"Claude New","max_input_tokens":200000}],"has_more":true,"last_id":"claude-new"}`
					} else {
						require.Equal(t, "claude-new", req.URL.Query().Get("after_id"))
						body = `{"data":[{"id":"claude-next"}]}`
					}
				case ProviderGemini:
					require.Equal(t, "private-key", req.Header.Get("x-goog-api-key"))
					if calls == 1 {
						body = `{"models":[{"name":"models/gemini-new","displayName":"Gemini New","inputTokenLimit":32000,"outputTokenLimit":4096,"supportedGenerationMethods":["generateContent"]},{"name":"models/embedding","supportedGenerationMethods":["embedContent"]}],"nextPageToken":"next"}`
					} else {
						require.Equal(t, "next", req.URL.Query().Get("pageToken"))
						body = `{"models":[{"name":"models/gemini-next","supportedGenerationMethods":["generateContent"]}]}`
					}
				}
				return catalogResponse(req, 200, body), nil
			})
			found, err := DiscoverProviderModels(context.Background(), provider, "https://provider.invalid/prefix", "private-key")
			require.NoError(t, err)
			if provider == ProviderOpenAI {
				require.Len(t, found, 1)
				require.Equal(t, "gpt-new", found[0].APIModel)
				require.Equal(t, 1, calls)
			} else {
				require.Len(t, found, 2)
				require.Equal(t, 2, calls)
			}
			for _, m := range found {
				require.Equal(t, provider, m.Provider)
				require.Positive(t, m.ContextWindow)
				require.False(t, m.CanReason)
			}
		})
	}
}
func TestCatalogFailuresDoNotExposeSecretsOrFollowRedirects(t *testing.T) {
	for name, body := range map[string]string{"error": "secret-response-body", "invalid": "not JSON", "large": strings.Repeat("x", (4<<20)+1), "redirect": "redirect-body"} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			catalogTransport(t, func(req *http.Request) (*http.Response, error) {
				calls++
				status := 200
				if name == "error" {
					status = 401
				}
				if name == "redirect" {
					status = 302
				}
				response := catalogResponse(req, status, body)
				response.Header.Set("Location", "https://exfil.invalid?secret=query")
				return response, nil
			})
			_, err := DiscoverProviderModels(context.Background(), ProviderOpenAI, "https://provider.invalid/v1", "private-key")
			require.Error(t, err)
			require.NotContains(t, err.Error(), "private-key")
			require.NotContains(t, err.Error(), "secret-response-body")
			require.Equal(t, 1, calls)
		})
	}
	catalogTransport(t, func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("transport error containing private-key")
	})
	_, err := DiscoverProviderModels(context.Background(), ProviderOpenAI, "https://provider.invalid/v1", "private-key")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private-key")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = DiscoverProviderModels(ctx, ProviderOpenAI, "https://provider.invalid/v1", "private-key")
	require.ErrorIs(t, err, context.Canceled)
	_, err = DiscoverProviderModels(context.Background(), ProviderAzure, "", "")
	require.ErrorIs(t, err, ErrCatalogUnsupported)
}
