package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

type preparedSchemaTool struct {
	calls       atomic.Int64
	description string
}

func (tool *preparedSchemaTool) Info() tools.ToolInfo {
	tool.calls.Add(1)
	return tools.ToolInfo{Name: "fixture", Description: tool.description, Parameters: map[string]any{"text": map[string]string{"type": "string"}}}
}
func (*preparedSchemaTool) Run(context.Context, tools.ToolCall) (tools.ToolResponse, error) {
	panic("preparation must never execute a tool")
}

func TestPreparedRequestBudgetsAndSendsIdenticalImmutableJSON(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			captured := make(chan []byte, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				captured <- raw
				if stream {
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				} else {
					fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
				}
			}))
			defer server.Close()
			p := cloneFixture(t, server.URL, "fixture")
			history := []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "original question"}}}}
			tool := &preparedSchemaTool{description: "original schema"}
			prepared, err := p.(RequestPreparer).PrepareRequest(history, []tools.BaseTool{tool}, stream)
			require.NoError(t, err)
			callsAfterPreparation := tool.calls.Load()
			footprint, err := prepared.RequestFootprint()
			require.NoError(t, err)
			originalBytes := append([]byte(nil), prepared.(*preparedHTTP).body...)
			history[0].Parts[0] = message.TextContent{Text: "mutated question"}
			tool.description = "mutated schema"
			if stream {
				var completed bool
				for event := range prepared.StreamResponse(context.Background()) {
					require.NoError(t, event.Error)
					completed = completed || event.Type == EventComplete
				}
				require.True(t, completed)
			} else {
				response, err := prepared.SendMessages(context.Background())
				require.NoError(t, err)
				require.Equal(t, "ok", response.Content)
			}
			raw := <-captured
			require.Equal(t, originalBytes, raw)
			require.EqualValues(t, len(raw), footprint.ASCIIBytes+footprint.OtherBytes)
			require.EqualValues(t, callsAfterPreparation, tool.calls.Load(), "budget/send must share the same prepared tool schema")
			require.NotContains(t, string(raw), "mutated")
		})
	}
}

func TestPreparedRequestModeMismatchNeverSends(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(400) }))
	defer server.Close()
	p := cloneFixture(t, server.URL, "fixture")
	prepared, err := p.(RequestPreparer).PrepareRequest(nil, nil, true)
	require.NoError(t, err)
	_, err = prepared.SendMessages(context.Background())
	require.ErrorContains(t, err, "requires streaming")
	prepared, err = p.(RequestPreparer).PrepareRequest(nil, nil, false)
	require.NoError(t, err)
	for event := range prepared.StreamResponse(context.Background()) {
		require.ErrorContains(t, event.Error, "does not support streaming")
	}
	require.Zero(t, requests.Load())
}

func BenchmarkPreparedRequestBudget(b *testing.B) {
	p := cloneFixture(b, "http://127.0.0.1:11434/v1", "fixture").(*baseProvider)
	history := []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Explain the current project"}}}}
	b.Run("rebuild_after_budget", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, err := p.RequestFootprint(history, nil)
			require.NoError(b, err)
			_, err = p.client.(*openaiClient).prepare(history, nil, true)
			require.NoError(b, err)
		}
	})
	b.Run("prepare_once", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			request, err := p.PrepareRequest(history, nil, true)
			if err != nil {
				b.Fatal(err)
			}
			if _, err := request.RequestFootprint(); err != nil {
				b.Fatal(err)
			}
		}
	})
}
