package models

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOllamaMetadataPrioritizesSelectedAndBoundsConcurrency(t *testing.T) {
	started := make(chan string, 16)
	release := make(chan struct{})
	var active, peak atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var input struct {
			Model string `json:"model"`
		}
		if request.URL.Path != "/api/show" || json.NewDecoder(request.Body).Decode(&input) != nil {
			w.WriteHeader(400)
			return
		}
		now := active.Add(1)
		defer active.Add(-1)
		for previous := peak.Load(); now > previous && !peak.CompareAndSwap(previous, now); previous = peak.Load() {
		}
		started <- input.Model
		if input.Model != "selected" {
			select {
			case <-release:
			case <-request.Context().Done():
				return
			}
		}
		fmt.Fprint(w, `{"model_info":{"general.architecture":"llama","llama.context_length":131072}}`)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(WithPreferredOllamaModel(context.Background(), "selected"), 2*time.Second)
	defer cancel()
	found := []localModel{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}, {ID: "e"}, {ID: "f"}, {ID: "selected"}}
	done := make(chan struct{})
	go func() { enrichOllamaContexts(ctx, server.Client(), server.URL, "", found, nil); close(done) }()
	require.Equal(t, "selected", awaitMetadataStart(t, ctx, started))
	for range 4 {
		require.NotEqual(t, "selected", awaitMetadataStart(t, ctx, started))
	}
	require.EqualValues(t, 4, peak.Load())
	close(release)
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("metadata workers did not finish")
	}
	for _, model := range found {
		require.EqualValues(t, 131072, model.MaxContextLength)
	}
}

func awaitMetadataStart(t *testing.T, ctx context.Context, started <-chan string) string {
	t.Helper()
	select {
	case model := <-started:
		return model
	case <-ctx.Done():
		t.Fatal("metadata did not start")
		return ""
	}
}

func TestOllamaStaticCacheUsesDigestAndCredentialIdentityNotAllocation(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"capabilities":["tools"],"model_info":{"general.architecture":"llama","llama.context_length":131072}}`)
	}))
	defer server.Close()
	first := enrichMetadataFixture(server, "key-one", "digest-one", 8192)
	second := enrichMetadataFixture(server, "key-one", "digest-one", 16384)
	require.EqualValues(t, 1, requests.Load())
	require.EqualValues(t, 8192, first.LoadedContextLength)
	require.EqualValues(t, 16384, second.LoadedContextLength, "runtime allocation must never come from static cache")
	require.EqualValues(t, 131072, second.MaxContextLength)
	require.NotNil(t, second.SupportsTools)
	require.True(t, *second.SupportsTools)
	enrichMetadataFixture(server, "key-one", "digest-two", 8192)
	require.EqualValues(t, 2, requests.Load(), "model replacement must invalidate static metadata")
	enrichMetadataFixture(server, "key-two", "digest-two", 8192)
	require.EqualValues(t, 3, requests.Load(), "credential identities cannot share metadata")
	enrichMetadataFixture(server, "key-two", "", 8192)
	enrichMetadataFixture(server, "key-two", "", 8192)
	require.EqualValues(t, 5, requests.Load(), "unknown digest must always be observed again")
}

func enrichMetadataFixture(server *httptest.Server, key, digest string, allocation int64) localModel {
	found := []localModel{{ID: "fixture", Digest: digest}}
	enrichOllamaContexts(context.Background(), server.Client(), server.URL, key, found, []OllamaRuntime{{Name: "fixture", Context: allocation}})
	return found[0]
}

func TestOllamaMetadataCacheNeverBypassesHTTPSPolicy(t *testing.T) {
	_, cacheable := ollamaMetadataIdentity("https://localhost/v1", "key", localModel{ID: "fixture", Digest: "digest"}, nil)
	require.False(t, cacheable)
	one, _ := ollamaMetadataIdentity("http://one/v1", "key", localModel{ID: "fixture", Digest: "digest"}, nil)
	two, _ := ollamaMetadataIdentity("http://two/v1", "key", localModel{ID: "fixture", Digest: "digest"}, nil)
	require.NotEqual(t, one, two)
}
