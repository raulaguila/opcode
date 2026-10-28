package models

import (
	"context"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOllamaStatusIsBoundedReadOnlyAndReportsObservedPlacement(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "GET", r.Method)
		require.Equal(t, "/api/ps", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		w.Write([]byte(`{"models":[{"name":"llama3.1:8b","size":100,"size_vram":40,"context_length":8192}]}`))
	}))
	defer server.Close()
	found, err := InspectOllama(context.Background(), server.URL+"/v1", "test-key")
	require.NoError(t, err)
	require.Len(t, found, 1)
	require.Equal(t, int64(8192), found[0].Context)
	require.Equal(t, "CPU/GPU", found[0].Placement())
	require.Equal(t, "CPU", (OllamaRuntime{}).Placement())
	require.Equal(t, "GPU", (OllamaRuntime{Size: 100, VRAM: 100}).Placement())
}
func TestOllamaStatusRejectsRedirectLargeAndInvalidResponsesWithoutPayloads(t *testing.T) {
	for _, body := range []string{`{"models":[{"context_length":-1}]}`, `not-json-private-body`, strings.Repeat("x", 1<<20+1)} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
		_, err := InspectOllama(context.Background(), server.URL, "")
		require.Error(t, err)
		require.NotContains(t, err.Error(), "private-body")
		server.Close()
	}
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("redirect forwarded credentials") }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer server.Close()
	_, err := InspectOllama(context.Background(), server.URL, "private-redirect-key")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private-redirect-key")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = InspectOllama(ctx, server.URL, "")
	require.Error(t, err)
}
