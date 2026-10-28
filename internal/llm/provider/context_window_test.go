package provider

import (
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAdaptiveWindowCloneChangesOllamaRequestWithoutMutatingOriginal(t *testing.T) {
	model := models.Model{ID: "llama", Provider: models.ProviderLocal, API: "ollama", APIModel: "llama3.1:8b", ContextWindow: 32768, DefaultMaxTokens: 4096}
	original, err := NewProvider(models.ProviderLocal, WithModel(model), WithBaseURL("http://localhost:11434"), WithMaxTokens(4096))
	require.NoError(t, err)
	clone, err := original.(ContextWindowController).WithContextWindow(8192)
	require.NoError(t, err)
	require.EqualValues(t, 32768, original.Model().ContextWindow)
	require.EqualValues(t, 8192, clone.Model().ContextWindow)
	native := clone.(*baseProvider).client.(*ollamaClient)
	_, raw, err := native.request(nil, nil, true)
	require.NoError(t, err)
	options := raw.(object)["options"].(object)
	require.EqualValues(t, 8192, options["num_ctx"])
	require.EqualValues(t, 2048, options["num_predict"])
	_, err = original.(ContextWindowController).WithContextWindow(2048)
	require.Error(t, err)
}
func TestOllamaFailureClassifiesWithoutDisclosingRawBody(t *testing.T) {
	for _, raw := range []string{`"out of memory: secret-data"`, `"maximum context length exceeded: secret-data"`} {
		err := ollamaFailure([]byte(raw))
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret-data")
	}
}
