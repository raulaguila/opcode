package provider

import (
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestProviderJSONEndpointOverridesDefaultForInference(t *testing.T) {
	for _, id := range []models.ModelProvider{models.ProviderOpenAI, models.ProviderGROQ, models.ProviderXAI, models.ProviderOpenRouter, models.ProviderAnthropic, models.ProviderGemini, models.ProviderLocal} {
		t.Run(string(id), func(t *testing.T) {
			t.Setenv("LOCAL_ENDPOINT", "")
			p, err := NewProvider(id, WithAPIKey("test-key"), WithBaseURL("http://provider.invalid/prefix/v1"), WithModel(models.Model{ID: "test", Provider: id, APIModel: "test"}))
			require.NoError(t, err)
			base := p.(*baseProvider)
			var transport *apiTransport
			switch client := base.client.(type) {
			case *openaiClient:
				transport = client.transport
			case *anthropicClient:
				transport = client.transport
			case *geminiClient:
				transport = client.transport
			default:
				t.Fatalf("unexpected client %T", client)
			}
			require.Equal(t, "http://provider.invalid/prefix/v1", transport.baseURL)
		})
	}
}
