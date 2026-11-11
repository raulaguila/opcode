package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/stretchr/testify/require"
)

func TestConfiguredHeadersNeverFollowARedirect(t *testing.T) {
	var leaked atomic.Int32
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "" {
			leaked.Add(1)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer elsewhere.Close()
	named := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/mcp", http.StatusTemporaryRedirect)
	}))
	defer named.Close()

	for _, kind := range []config.MCPType{config.MCPHTTP, config.MCPSse} {
		client, err := newMCPClient(config.MCPServer{Type: kind, URL: named.URL + "/mcp", Headers: map[string]string{"X-Api-Key": "secret"}})
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if client.Start(ctx) == nil {
			_, err = client.Initialize(ctx, mcp.InitializeRequest{})
			require.Error(t, err, kind)
		}
		cancel()
		_ = client.Close()
	}
	require.Zero(t, leaked.Load(), "the API key reached a host the user never configured")
}
