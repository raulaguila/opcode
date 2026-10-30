package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/stretchr/testify/require"
)

func TestWebSearchDisabledDeniedAndPrivateNetworkPolicies(t *testing.T) {
	productivityWorkspace(t)
	cfg := config.Get()
	previousRuntime, previousSecurity := cfg.Runtime, cfg.Security
	t.Cleanup(func() { cfg.Runtime, cfg.Security = previousRuntime, previousSecurity })
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); io.WriteString(w, "never needed") }))
	defer server.Close()
	p := &denyingPermissions{}
	tool := &webSearchTool{permissions: p, endpoint: server.URL}
	input := toolInput(map[string]any{"query": "public documentation"})
	cfg.Runtime.WebSearch = false
	r, err := tool.Run(toolContext(), input)
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.Empty(t, p.requests)
	cfg.Runtime.WebSearch = true
	_, err = tool.Run(toolContext(), input)
	require.ErrorIs(t, err, permission.ErrorPermissionDenied)
	require.EqualValues(t, 0, requests.Load())
	require.Len(t, p.requests, 1)
	require.Equal(t, WebSearchToolName, p.requests[0].ToolName)
	require.NotContains(t, p.requests[0].Description, "documentation")
	p.allow = func(permission.CreatePermissionRequest) bool { return true }
	cfg.Security.AllowPrivateNetwork = false
	_, err = tool.Run(toolContext(), input)
	require.ErrorContains(t, err, "blocked by policy")
	require.EqualValues(t, 0, requests.Load())
	ctx, cancel := context.WithCancel(toolContext())
	cancel()
	before := len(p.requests)
	_, err = tool.Run(ctx, input)
	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, p.requests, before)
	const secret = "synthetic-web-search-private-key"
	config.RegisterRuntimeCredentials("web-search-test", secret)
	t.Cleanup(func() { config.RegisterRuntimeCredentials("web-search-test") })
	r, err = SecureTool(tool, p).Run(toolContext(), toolInput(map[string]any{"query": secret}))
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.NotContains(t, r.Content, secret)
	require.Len(t, p.requests, before, "credentials must not reach a network request or permission prompt")
}

func TestWebSearchBoundsParametersBodyAndResults(t *testing.T) {
	productivityWorkspace(t)
	cfg := config.Get()
	previousRuntime, previousSecurity := cfg.Runtime, cfg.Security
	t.Cleanup(func() { cfg.Runtime, cfg.Security = previousRuntime, previousSecurity })
	cfg.Runtime.WebSearch, cfg.Security.AllowPrivateNetwork = true, true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/large":
			io.WriteString(w, strings.Repeat("x", (1<<20)+1))
		case "/blocked":
			w.WriteHeader(http.StatusTooManyRequests)
		case "/empty":
			io.WriteString(w, `<html>challenge; no results</html>`)
		default:
			if r.URL.Query().Get("q") != "Go & APIs" {
				t.Error("query was not URL encoded correctly")
			}
			w.Header().Set("Content-Type", "text/html")
			for i := range 15 {
				fmt.Fprintf(w, `<div class="result"><a class="result__a" href="https://example.test/%d">Title %d</a><div class="result__snippet">Untrusted snippet %d</div></div>`, i, i, i)
			}
		}
	}))
	defer server.Close()
	p := &denyingPermissions{allow: func(permission.CreatePermissionRequest) bool { return true }}
	tool := &webSearchTool{permissions: p, endpoint: server.URL}
	r, err := tool.Run(toolContext(), toolInput(map[string]any{"query": "Go & APIs", "limit": 2}))
	require.NoError(t, err)
	require.False(t, r.IsError)
	var body struct {
		Results []webSearchResult
		Notice  string
	}
	require.NoError(t, json.Unmarshal([]byte(r.Content), &body))
	require.Len(t, body.Results, 2)
	require.Equal(t, "https://example.test/0", body.Results[0].URL)
	require.Contains(t, body.Notice, "not current repository")
	for _, path := range []string{"/large", "/blocked", "/empty"} {
		tool.endpoint = server.URL + path
		r, err = tool.Run(toolContext(), toolInput(map[string]any{"query": "Go & APIs"}))
		require.NoError(t, err)
		require.True(t, r.IsError, path)
	}
	count := len(p.requests)
	for _, params := range []map[string]any{{"query": ""}, {"query": strings.Repeat("x", 513)}, {"query": "q", "limit": 11}, {"query": "q", "limit": -1}} {
		r, err = tool.Run(toolContext(), toolInput(params))
		require.NoError(t, err)
		require.True(t, r.IsError)
	}
	require.Len(t, p.requests, count, "invalid input must never request network access")
}

func TestWebSearchParserUnwrapsDeduplicatesAndBoundsUntrustedText(t *testing.T) {
	raw := `<div class="result"><a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fdoc%2F">Go docs</a><div class="result__snippet">` + strings.Repeat("ação ", 500) + `</div></div>` +
		`<div class="result"><a class="result__a" href="https://go.dev/doc/">duplicate</a></div>` +
		`<div class="result"><a class="result__a" href="javascript:alert(1)">unsafe</a></div>` +
		`<div class="result"><a class="result__a" href="https://user:password@example.test">credentials</a></div>`
	results, err := parseWebSearch(raw, 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "https://go.dev/doc/", results[0].URL)
	require.Len(t, []rune(results[0].Snippet), 480)
}

func TestWebSearchRedirectRequiresSeparateApprovalAndCancellationStopsHTTP(t *testing.T) {
	productivityWorkspace(t)
	cfg := config.Get()
	previousRuntime, previousSecurity := cfg.Runtime, cfg.Security
	t.Cleanup(func() { cfg.Runtime, cfg.Security = previousRuntime, previousSecurity })
	cfg.Runtime.WebSearch, cfg.Security.AllowPrivateNetwork = true, true
	var destinationHits atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { destinationHits.Add(1) }))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer origin.Close()
	p := &denyingPermissions{allow: func(request permission.CreatePermissionRequest) bool { return request.Path == "network:"+origin.URL }}
	tool := &webSearchTool{permissions: p, endpoint: origin.URL}
	_, err := tool.Run(toolContext(), toolInput(map[string]any{"query": "public query"}))
	require.ErrorIs(t, err, permission.ErrorPermissionDenied)
	require.Len(t, p.requests, 2)
	require.Equal(t, "network:"+destination.URL, p.requests[1].Path)
	require.EqualValues(t, 0, destinationHits.Load())
	started, stopped := make(chan struct{}), make(chan struct{})
	blocking := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done(); close(stopped) }))
	defer blocking.Close()
	tool.endpoint = blocking.URL
	p.allow = func(permission.CreatePermissionRequest) bool { return true }
	ctx, cancel := context.WithCancel(toolContext())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, e := tool.Run(ctx, toolInput(map[string]any{"query": "public query"})); done <- e }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP fixture did not start")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("search did not cancel")
	}
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP connection did not close on cancellation")
	}
}
