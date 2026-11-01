//go:build integration

package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPTYOutputLimitKeepsContextAndRejectsIncompleteCalls(t *testing.T) {
	if os.Getenv("OPCODE_LIVE_TESTS") != "1" || runtime.GOOS != "linux" {
		t.Skip("explicit Linux PTY acceptance opt-in required")
	}
	for _, api := range []string{"ollama", "chat_completions", "responses"} {
		t.Run(api, func(t *testing.T) { testPTYOutputLimit(t, api) })
	}
}

func testPTYOutputLimit(t *testing.T, api string) {
	binary := os.Getenv("OPCODE_EVAL_BINARY")
	require.True(t, filepath.IsAbs(binary))
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := map[string]string{"ollama": "/api/chat", "chat_completions": "/v1/chat/completions", "responses": "/v1/responses"}[api]
		if r.URL.Path != path {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		calls.Add(1)
		if api != "ollama" {
			w.Header().Set("Content-Type", "text/event-stream")
			payload := `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"unfinished-call","type":"function","function":{"name":"bash","arguments":"{\"command\":\"touch SHOULD_NOT_EXIST\"}"}}]},"finish_reason":"length"}]}`
			if api == "responses" {
				payload = `{"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"function_call","call_id":"unfinished-call","name":"bash","arguments":"{\"command\":\"touch SHOULD_NOT_EXIST\"}"}]}}`
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", payload)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"role": "assistant", "tool_calls": []any{
			map[string]any{"function": map[string]any{"name": "final_answer", "arguments": map[string]any{"answer": "UNVERIFIED_OUTPUT"}}},
			map[string]any{"function": map[string]any{"name": "bash", "arguments": map[string]any{"command": "touch SHOULD_NOT_EXIST"}}},
		}}, "done": true, "done_reason": "length", "eval_count": 2048})
	}))
	defer server.Close()
	home, workspace, _ := conversationWorkspace(t, server.URL)
	if api != "ollama" {
		path := filepath.Join(home, ".opcode", "config.json")
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		var cfg map[string]any
		require.NoError(t, json.Unmarshal(raw, &cfg))
		cfg["providers"].(map[string]any)["local"].(map[string]any)["api"] = api
		cfg["models"].(map[string]any)["local.conversation-fixture"].(map[string]any)["api"] = api
		raw, err = json.Marshal(cfg)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, raw, 0600))
	}
	h := newPTYUIHarnessAtSize(t, binary, home, workspace, 120, 40)
	h.wait("initialization", 20*time.Second, func(frame string) bool { return strings.Contains(frame, "Initialize Project") })
	h.send("n")
	h.wait("ready", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Initialize Project") && ptyUIHasFooter(frame)
	})
	h.paste("Responda ok sem alterar arquivos.")
	h.send("\r")
	h.wait("specific output budget failure", 15*time.Second, func(frame string) bool {
		return strings.Contains(frame, "Output limit reached") && strings.Contains(frame, "/settings Review output tokens")
	})
	require.NotContains(t, h.frame(), "Model limit reached")
	require.NotContains(t, h.frame(), "UNVERIFIED_OUTPUT")
	require.Contains(t, ptyUIFooter(h.frame()), "16k ctx")
	require.EqualValues(t, 1, calls.Load(), "no automatic retry or repair")
	require.NoFileExists(t, filepath.Join(workspace, "SHOULD_NOT_EXIST"))
	h.paste("KEEP_OUTPUT_DRAFT")
	h.resize(60, 20)
	h.wait("failure and draft after resize", 5*time.Second, func(frame string) bool {
		return strings.Contains(frame, "KEEP_OUTPUT_DRAFT") && strings.Contains(frame, "Output limit reached")
	})
	h.assertComposer()
	conversationQuit(t, h, false)
	readme, err := os.ReadFile(filepath.Join(workspace, "README.md"))
	require.NoError(t, err)
	require.Equal(t, "# SensorStore\nO projeto coleta sensores em SQLite.\n", string(readme))
}
