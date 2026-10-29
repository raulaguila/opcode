package config

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/stretchr/testify/require"
)

func TestProfileTLSOptInReachesCatalogWithoutChangingOtherProfiles(t *testing.T) {
	isolateModelConfig(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":[{"id":"fixture","context_window":32768}]}`)
	}))
	defer server.Close()
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	profile := ProviderProfile{Name: "TLS", Backend: models.ProviderLocal, Provider: Provider{BaseURL: server.URL + "/v1", API: "chat_completions"}}
	_, err := TestProviderProfile("tls", profile)(models.WithoutCatalogMetadata(context.Background()))
	require.Error(t, err)
	profile.TLSInsecure = true
	found, err := TestProviderProfile("tls", profile)(models.WithoutCatalogMetadata(context.Background()))
	require.NoError(t, err)
	require.Len(t, found, 1)
	profile.TLSInsecure = false
	_, err = TestProviderProfile("tls", profile)(models.WithoutCatalogMetadata(context.Background()))
	require.Error(t, err)
	profile.TLSInsecure = true
	profile.API = "ollama"
	_, err = PreviewProviderProfile("tls", profile)
	require.Error(t, err)
}
