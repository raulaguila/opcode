package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeOllamaRepairRetainsScopedCitationSchemaThroughBudgeting(t *testing.T) {
	a, _, _, root := runtimeAgent(t, workflow.Ask)
	require.NoError(t, os.WriteFile(filepath.Join(root, "counter.py"), []byte("def add(a,b):\n    return a - b\n"), 0600))
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Tools []struct {
				Function struct {
					Name       string `json:"name"`
					Parameters struct {
						Properties map[string]json.RawMessage `json:"properties"`
					} `json:"parameters"`
				} `json:"function"`
			} `json:"tools"`
		}
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
			http.Error(w, "bad request", 400)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		switch requests.Add(1) {
		case 1:
			_, _ = w.Write([]byte(`{"message":{"tool_calls":[{"function":{"name":"view","arguments":{"file_path":"counter.py"}}}]},"done":true,"done_reason":"stop"}` + "\n"))
		case 2:
			_, _ = w.Write([]byte(`{"message":{"tool_calls":[{"function":{"name":"final_answer","arguments":{"answer":"add subtrai os argumentos.","citations_text":""}}}]},"done":true,"done_reason":"stop"}` + "\n"))
		case 3:
			found := false
			for _, tool := range body.Tools {
				if tool.Function.Name == "answer_with_sources" {
					var field struct {
						Enum []string `json:"enum"`
					}
					assert.NoError(t, json.Unmarshal(tool.Function.Parameters.Properties["source"], &field))
					assert.Equal(t, []string{"counter.py:1-2"}, field.Enum)
					found = true
				}
			}
			assert.True(t, found)
			_, _ = w.Write([]byte(`{"message":{"tool_calls":[{"function":{"name":"answer_with_sources","arguments":{"explanation":"Em counter.py, add subtrai os argumentos.","source":"counter.py:1-2"}}}]},"done":true,"done_reason":"stop"}` + "\n"))
		default:
			http.Error(w, "unexpected retry", 500)
		}
	}))
	defer server.Close()
	model := models.Model{ID: "local.llama3.1:8b", Provider: models.ProviderLocal, API: "ollama", APIModel: "llama3.1:8b", ContextWindow: 16384, DefaultMaxTokens: 2048}
	p, err := provider.NewProvider(models.ProviderLocal, provider.WithModel(model), provider.WithBaseURL(server.URL+"/v1"), provider.WithMaxTokens(2048))
	require.NoError(t, err)
	a.provider = p
	a.tools = []tools.BaseTool{tools.NewViewTool(nil)}
	done, err := a.Run(context.Background(), "s", "Localize a causa do erro em counter.py e explique sem editar.")
	require.NoError(t, err)
	result := finishGeneration(t, done)
	require.NoError(t, result.Error)
	require.Contains(t, result.Message.Content().Text, "counter.py:1-2")
	require.EqualValues(t, 3, requests.Load())
}
