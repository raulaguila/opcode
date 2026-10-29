package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Exercise the actual compatible transport/agent boundary. These are synthetic
// SSE responses, not an evaluation of the company's endpoint or Qwen weights.
func TestOpenAICompatibleHistoricalCitationAcrossTurns(t *testing.T) {
	for _, test := range []struct {
		name, question, answer string
		repair                 bool
	}{
		{"valid reference", "Explique a função lida anteriormente.", "Em counter.py:2, add subtrai os argumentos.", false},
		{"unread line repaired", "Explique a função lida anteriormente.", "Em counter.py:99, add subtrai os argumentos.", true},
		{"missing project reference repaired", "Explique o projeto a partir da fonte lida anteriormente.", "O projeto define uma função add que subtrai os argumentos.", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, _, _, root := runtimeAgent(t, workflow.Ask)
			require.NoError(t, os.WriteFile(filepath.Join(root, "counter.py"), []byte("def add(a,b):\n    return a - b\n"), 0600))
			read := &citationReadTool{BaseTool: tools.NewReadPageTool()}
			a.tools = []tools.BaseTool{read}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1/chat/completions", r.URL.Path)
				var body struct {
					Messages []struct {
						Role    string          `json:"role"`
						Content json.RawMessage `json:"content"`
					} `json:"messages"`
					Tools []struct {
						Function struct {
							Name string `json:"name"`
						} `json:"function"`
					} `json:"tools"`
				}
				if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
					http.Error(w, "invalid request", http.StatusBadRequest)
					return
				}
				n := requests.Add(1)
				if n >= 4 {
					foundRead := false
					for _, m := range body.Messages {
						if m.Role == "tool" && strings.Contains(string(m.Content), "return a - b") {
							foundRead = true
						}
					}
					assert.True(t, foundRead, "original source remains available on the wire")
				}
				for _, tool := range body.Tools {
					assert.NotEqual(t, "answer_with_sources", tool.Function.Name, "Ollama's private repair tool must not leak into compatible APIs")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				var delta map[string]any
				finish := "stop"
				switch n {
				case 1:
					delta = map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "load-read", "type": "function", "function": map[string]any{"name": "request_tools", "arguments": `{"names":["read_lines"]}`}}}}
					finish = "tool_calls"
				case 2:
					delta = map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "read-once", "type": "function", "function": map[string]any{"name": "read_lines", "arguments": `{"file_path":"counter.py","limit":2}`}}}}
					finish = "tool_calls"
				case 3:
					delta = map[string]any{"content": "Leitura concluída."}
				case 4:
					delta = map[string]any{"content": test.answer}
				case 5:
					assert.True(t, test.repair, "a valid historical reference needs no extra generation")
					notice := string(body.Messages[len(body.Messages)-1].Content)
					assert.Contains(t, notice, "counter.py:1-2")
					var parts []struct {
						Text string `json:"text"`
					}
					assert.NoError(t, json.Unmarshal(body.Messages[len(body.Messages)-1].Content, &parts))
					if assert.NotEmpty(t, parts) {
						assert.Contains(t, parts[0].Text, test.question)
					}
					assert.Contains(t, notice, "Do not repeat successful reads")
					delta = map[string]any{"content": "Em counter.py:2, add usa a - b em vez de a + b."}
				default:
					http.Error(w, "unexpected retry", http.StatusInternalServerError)
					return
				}
				frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
				_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", frame)
			}))
			defer server.Close()
			model := models.Model{ID: "fixture.compatible", Provider: models.ProviderLocal, ProviderProfile: "test-compatible", API: "chat_completions", APIModel: "synthetic-qwen", ContextWindow: 16384, DefaultMaxTokens: 2048}
			p, err := provider.NewProvider(models.ProviderLocal, provider.WithModel(model), provider.WithBaseURL(server.URL+"/v1"), provider.WithMaxTokens(2048))
			require.NoError(t, err)
			a.provider = p
			first, err := a.Run(context.Background(), "s", "Use read_lines para ler counter.py.")
			require.NoError(t, err)
			require.NoError(t, finishGeneration(t, first).Error)
			second, err := a.Run(context.Background(), "s", test.question)
			require.NoError(t, err)
			result := finishGeneration(t, second)
			require.NoError(t, result.Error)
			require.Contains(t, result.Message.Content().Text, "counter.py:2")
			expected := int32(4)
			if test.repair {
				expected++
			}
			require.Equal(t, expected, requests.Load())
			require.EqualValues(t, 1, read.count.Load(), "neither validation nor repair replays the native read")
			raw, err := os.ReadFile(filepath.Join(root, "counter.py"))
			require.NoError(t, err)
			require.Equal(t, "def add(a,b):\n    return a - b\n", string(raw))
		})
	}
}
