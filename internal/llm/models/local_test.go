package models

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type discoveryTransport func(*http.Request) (*http.Response, error)

func (f discoveryTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestNormalizeLocalEndpoint(t *testing.T) {
	for input, expected := range map[string]string{
		"http://localhost:11434":       "http://localhost:11434/v1",
		"http://localhost:1234/v1/":    "http://localhost:1234/v1",
		"https://local.invalid/llm/v1": "https://local.invalid/llm/v1",
	} {
		actual, err := NormalizeLocalEndpoint(input)
		require.NoError(t, err)
		require.Equal(t, expected, actual)
	}
	for _, input := range []string{"", "localhost:11434", "ftp://local.invalid", "https://user:secret@local.invalid/v1", "https://local.invalid/v1?api_key=secret", "https://local.invalid/v1#secret"} {
		_, err := NormalizeLocalEndpoint(input)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret")
	}
}

func TestLocalDiscoveryPreservesLMStudioContextAndPrefersLoaded(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = discoveryTransport(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, "/api/v0/models", req.URL.Path)
		require.Equal(t, "Bearer unit-test-key", req.Header.Get("Authorization"))
		body := `{"data":[{"id":"embedding","object":"model","type":"embeddings"},{"id":"unloaded","object":"model","type":"llm"},{"id":"active","object":"model","type":"llm","state":"loaded","loaded_context_length":8192,"max_context_length":32768}]}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
	})
	found, err := DiscoverLocalModels(context.Background(), "http://local.invalid/v1", "unit-test-key")
	require.NoError(t, err)
	require.Len(t, found, 2)
	require.Equal(t, ModelID("local.active"), found[0].ID)
	require.EqualValues(t, 8192, found[0].ContextWindow)
	require.EqualValues(t, 4096, found[0].DefaultMaxTokens)
	require.False(t, found[0].CanReason, "discovery cannot assume reasoning parameter support")
	require.False(t, found[0].SupportsAttachments, "discovery cannot assume vision support")
}

func TestLocalDiscoveryRejectsBadResponsesAndCancellation(t *testing.T) {
	for _, body := range []string{`{"data":[]}`, `not json`, strings.Repeat("x", (1<<20)+1)} {
		original := http.DefaultTransport
		http.DefaultTransport = discoveryTransport(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
		})
		_, err := DiscoverLocalModels(context.Background(), "http://local.invalid/prefix/v1", "")
		http.DefaultTransport = original
		require.Error(t, err)
	}
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = discoveryTransport(func(req *http.Request) (*http.Response, error) {
		return nil, req.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := DiscoverLocalModels(ctx, "http://local.invalid/v1", "")
	require.Error(t, err)
}
