package config

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/stretchr/testify/require"
)

func TestModelMetadataDoesNotResolveUnneededOrCancelledVaultCredentials(t *testing.T) {
	fake, _ := vaultConfig(t)
	const account = "provider-abcdefghijklmnopqrst"
	fake.entries[account] = "fake-metadata-vault-key"
	profile := cfg.ProviderProfiles["office"]
	profile.APIKey, profile.APIKeyRef = "", "keyring:"+account
	cfg.ProviderProfiles["office"] = profile
	model := models.Model{ID: "profile.office.model", Provider: models.ProviderOpenAI, ProviderProfile: "office", APIModel: "model"}
	require.Nil(t, ModelMetadata(model))
	require.Zero(t, fake.gets, "browsing a remote model has no Ollama metadata I/O")
	profile.Backend, profile.API = models.ProviderLocal, "chat_completions"
	cfg.ProviderProfiles["office"] = profile
	model.Provider = models.ProviderLocal
	require.Nil(t, ModelMetadata(model))
	require.Zero(t, fake.gets)
	profile.API = "ollama"
	cfg.ProviderProfiles["office"] = profile
	lookup := ModelMetadata(model)
	require.NotNil(t, lookup)
	require.Zero(t, fake.gets, "constructing a command must not read the OS vault")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := lookup(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, fake.gets)
}

func TestLocalMetadataResolvesCapturedVaultCredentialOnlyInsideCommand(t *testing.T) {
	fake, _ := vaultConfig(t)
	const account = "provider-abcdefghijklmnopqrst"
	fake.entries[account] = "fake-metadata-vault-key"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/show", r.URL.Path)
		require.Equal(t, "Bearer fake-metadata-vault-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"model_info":{"general.architecture":"llama","llama.context_length":32768},"capabilities":["tools"]}`))
	}))
	defer server.Close()
	cfg.ProviderProfiles["office"] = ProviderProfile{Backend: models.ProviderLocal, Provider: Provider{BaseURL: server.URL + "/v1", API: "ollama", APIKeyRef: "keyring:" + account}}
	model := models.Model{ID: "profile.office.model", Provider: models.ProviderLocal, ProviderProfile: "office", APIModel: "model", DefaultMaxTokens: 2048}
	lookup := ModelMetadata(model)
	require.NotNil(t, lookup)
	require.Zero(t, fake.gets)
	// A later settings change does not make this already-created command switch
	// credentials/endpoints or read configuration from a background goroutine.
	delete(cfg.ProviderProfiles, "office")
	actual, err := lookup(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, fake.gets)
	require.EqualValues(t, 32768, actual.ContextWindow)
}
