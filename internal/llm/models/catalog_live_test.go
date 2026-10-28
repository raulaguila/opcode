//go:build integration

package models

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLiveOllamaModelCatalog(t *testing.T) {
	if os.Getenv("OPCODE_LIVE_TESTS") != "1" || os.Getenv("OPCODE_LIVE_OLLAMA") != "1" {
		t.Skip("set OPCODE_LIVE_TESTS=1 and OPCODE_LIVE_OLLAMA=1")
	}
	endpoint := os.Getenv("LOCAL_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://127.0.0.1:11434/v1"
	}
	found, err := DiscoverProviderModels(context.Background(), ProviderLocal, endpoint, os.Getenv("LOCAL_API_KEY"))
	require.NoError(t, err)
	require.NotEmpty(t, found)
	selected := os.Getenv("OPCODE_LIVE_OLLAMA_MODEL")
	if selected == "" {
		selected = "llama3.1:8b"
	}
	installed := false
	for _, model := range found {
		if model.APIModel == selected {
			installed = true
		}
	}
	require.True(t, installed, "the configured acceptance model must appear in the live catalog")
	t.Logf("Live catalog returned %d models; selected model is available", len(found))
}
