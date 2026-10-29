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

func TestLocalDiscoveryReadsOllamaContextMetadata(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	http.DefaultTransport = discoveryTransport(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, "Bearer private-fixture", req.Header.Get("Authorization"))
		status, body := 200, `{}`
		switch req.URL.Path {
		case "/api/v0/models":
			status = 404
		case "/v1/models":
			body = `{"data":[{"id":"loaded:8b"},{"id":"preset:8b"},{"id":"maximum:8b"},{"id":"unknown:8b"},{"id":"loaded-fallback:8b"}]}`
		case "/api/ps":
			body = `{"models":[{"name":"loaded:8b","context_length":16384},{"name":"preset:8b","context_length":8192},{"name":"loaded-fallback:8b","context_length":8192}]}`
		case "/api/show":
			require.Equal(t, http.MethodPost, req.Method)
			var request struct {
				Model string `json:"model"`
			}
			require.NoError(t, json.NewDecoder(req.Body).Decode(&request))
			switch request.Model {
			case "loaded:8b", "maximum:8b":
				body = `{"model_info":{"general.architecture":"llama","llama.context_length":131072}}`
			case "preset:8b":
				body = `{"parameters":"num_ctx 32768\nstop \"x\"","model_info":{"general.architecture":"qwen2","qwen2.context_length":131072}}`
			default:
				status = 404
			}
		default:
			t.Fatalf("unexpected metadata endpoint: %s", req.URL.Path)
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
	})
	found, err := DiscoverLocalModels(context.Background(), "http://local.invalid/v1", "private-fixture")
	require.NoError(t, err)
	byID := map[string]Model{}
	for _, model := range found {
		byID[model.APIModel] = model
	}
	for id, window := range map[string]int64{"loaded:8b": 131072, "preset:8b": 32768, "maximum:8b": 131072, "unknown:8b": 4096, "loaded-fallback:8b": 8192} {
		require.Equal(t, window, byID[id].ContextWindow, id)
		require.Equal(t, id == "unknown:8b", byID[id].ContextEstimated, id)
	}
}

func TestOptionalOllamaMetadataFailureKeepsEstimatedCatalog(t *testing.T) {
	for name, raw := range map[string]string{
		"malformed":   `not json`,
		"oversized":   strings.Repeat("x", (1<<20)+1),
		"nonpositive": `{"model_info":{"general.architecture":"llama","llama.context_length":-1},"parameters":"num_ctx 0"}`,
	} {
		t.Run(name, func(t *testing.T) {
			previous := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = previous })
			http.DefaultTransport = discoveryTransport(func(req *http.Request) (*http.Response, error) {
				status, body := 200, raw
				switch req.URL.Path {
				case "/api/v0/models":
					status, body = 404, `{}`
				case "/v1/models":
					body = `{"data":[{"id":"active"}]}`
				case "/api/ps":
					body = `{"models":[]}`
				case "/api/show":
				default:
					t.Fatalf("unexpected endpoint: %s", req.URL.Path)
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
			})
			found, err := DiscoverLocalModels(context.Background(), "http://local.invalid/v1", "")
			require.NoError(t, err)
			require.Len(t, found, 1)
			require.True(t, found[0].ContextAuto)
			require.True(t, found[0].ContextEstimated)
			require.EqualValues(t, 4096, found[0].ContextWindow)
		})
	}
}

func TestOllamaToolsCapabilityUsesOnlyReportedMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, extra      string
		known, supported bool
	}{
		{"unknown", "", false, false}, {"reported", `,"capabilities":["completion","tools"]`, true, true}, {"absent", `,"capabilities":[]`, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = old })
			http.DefaultTransport = discoveryTransport(func(req *http.Request) (*http.Response, error) {
				require.Equal(t, "/api/show", req.URL.Path, "metadata must never generate or load")
				body := `{"model_info":{"general.architecture":"llama","llama.context_length":131072}` + tc.extra + `}`
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
			})
			model, err := InspectLocalModelMetadata(context.Background(), "http://local.invalid/v1", "", "llama3.1:8b")
			require.NoError(t, err)
			require.Equal(t, tc.known, model.SupportsTools != nil)
			if tc.known {
				require.Equal(t, tc.supported, *model.SupportsTools)
			}
		})
	}
}
