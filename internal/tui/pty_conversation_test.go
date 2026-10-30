//go:build integration

package tui

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const conversationDiagnosis = "DIAGNOSIS: O projeto coleta sensores em SQLite (README.md:1-2)."
const conversationAcknowledgment = "Perfeito — fica o diagnóstico. Não farei alterações agora."

// Only provider output is controlled. Tool execution, evidence validation,
// question submission, shutdown and history loading use the shipped binary.
func TestPTYConversationChronologyQuitAndResume(t *testing.T) {
	if os.Getenv("OPCODE_LIVE_TESTS") != "1" {
		t.Skip("explicit PTY acceptance opt-in required")
	}
	if runtime.GOOS != "linux" {
		t.Skip("Linux PTY acceptance")
	}
	binary := os.Getenv("OPCODE_EVAL_BINARY")
	require.True(t, filepath.IsAbs(binary))
	var generations, inferenceRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/chat" {
			inferenceRequests.Add(1)
		}
		conversationProvider(t, &generations, w, r)
	}))
	defer server.Close()
	home, workspace, data := conversationWorkspace(t, server.URL)
	h := newPTYUIHarnessAtSize(t, binary, home, workspace, 160, 44)
	h.wait("initialization", 20*time.Second, func(frame string) bool { return strings.Contains(frame, "Initialize Project") })
	h.send("n")
	h.wait("ready", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Initialize Project") && ptyUIHasFooter(frame)
	})
	h.paste("Explique o projeto atual e pergunte se quero implementar as melhorias.")
	h.send("\r")
	h.wait("visible diagnosis and pending question", 20*time.Second, func(frame string) bool {
		return strings.Contains(frame, "DIAGNOSIS:") && strings.Contains(frame, "Question for you") && strings.Contains(frame, "Waiting for your answer")
	})
	require.Contains(t, h.frame(), "README.md:1-2")
	require.Contains(t, h.frame(), "Inspecting source")
	h.resize(60, 20)
	h.wait("question remains usable in compact terminal", 5*time.Second, func(frame string) bool {
		return strings.Contains(frame, "Question for you") && strings.Contains(frame, "Waiting for your answer") && strings.Contains(frame, "Agora não")
	})
	h.assertComposer()
	t.Logf("question panel at 60x20:\n%s", h.frame())
	h.resize(160, 44)
	h.send("\x1b[B\x1b[B\r") // No implicit selection: explicitly choose Agora não.
	h.wait("acknowledgment with visible diagnosis", 15*time.Second, conversationCompleted)
	assertConversationOrder(t, h.frame())
	require.EqualValues(t, 3, generations.Load(), "no citation repair or extra model request should be needed")
	h.assertComposer()
	conversationQuit(t, h, true)
	count := conversationMessageCount(t, data)
	requests := inferenceRequests.Load()
	require.Greater(t, count, 0)

	h = newPTYUIHarnessWithArgs(t, binary, home, workspace, 160, 44, []string{"--resume"})
	h.wait("last conversation restored without model generation", 15*time.Second, conversationCompleted)
	assertConversationOrder(t, h.frame())
	require.EqualValues(t, 3, generations.Load())
	require.Equal(t, requests, inferenceRequests.Load(), "resume must not call the model, including auxiliary requests")
	require.Equal(t, count, conversationMessageCount(t, data), "reopening must not execute tools or append messages")
	draft := "UNSENT_RESUME_DRAFT ação 漢字"
	h.paste(draft)
	h.wait("unsent resume draft", 5*time.Second, func(frame string) bool { return strings.Contains(frame, draft) })
	conversationQuit(t, h, false)
	h = newPTYUIHarnessWithArgs(t, binary, home, workspace, 160, 44, []string{"--resume"})
	h.wait("conversation and draft restored", 15*time.Second, func(frame string) bool {
		return conversationCompleted(frame) && strings.Contains(frame, draft)
	})
	require.EqualValues(t, 3, generations.Load())
	require.Equal(t, requests, inferenceRequests.Load())
	require.Equal(t, count, conversationMessageCount(t, data))
	h.assertComposer()
	conversationQuit(t, h, false)
	readme, err := os.ReadFile(filepath.Join(workspace, "README.md"))
	require.NoError(t, err)
	require.Equal(t, "# SensorStore\nO projeto coleta sensores em SQLite.\n", string(readme), "question answers must not authorize source edits")
}

func conversationProvider(t *testing.T, generations *atomic.Int32, w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/chat" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if r.Header.Get("Authorization") != "" {
		t.Error("isolated provider must not receive credentials")
	}
	var body struct{ Stream bool }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Error(err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if !body.Stream {
		_, _ = io.WriteString(w, `{"message":{"content":"Conversation fixture"},"done":true,"done_reason":"stop"}`)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	content, name, arguments := "", "", map[string]any{}
	switch generations.Add(1) {
	case 1:
		content, name = "Inspecting source.", "view"
		arguments = map[string]any{"file_path": "README.md", "offset": 0, "limit": 2}
	case 2:
		content, name = conversationDiagnosis, "ask_user_question"
		arguments = map[string]any{"question": "Quer implementar as melhorias?", "choices": []string{"Sim", "Agora não"}}
	case 3:
		content = conversationAcknowledgment
	default:
		t.Error("unexpected inference or citation repair")
		content = "UNEXPECTED_GENERATION"
	}
	item := map[string]any{"role": "assistant", "content": content}
	if name != "" {
		item["tool_calls"] = []any{map[string]any{"function": map[string]any{"name": name, "arguments": arguments}}}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"message": item, "done": true, "done_reason": "stop", "prompt_eval_count": 40, "eval_count": 18, "eval_duration": 1000000000})
}

func conversationWorkspace(t *testing.T, endpoint string) (home, workspace, data string) {
	t.Helper()
	root := t.TempDir()
	home, workspace, data = filepath.Join(root, "home"), filepath.Join(root, "project"), filepath.Join(root, "data")
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".opcode"), 0700))
	require.NoError(t, os.MkdirAll(workspace, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "README.md"), []byte("# SensorStore\nO projeto coleta sensores em SQLite.\n"), 0600))
	raw, err := json.Marshal(map[string]any{
		"data": map[string]any{"directory": data},
		"tui":  map[string]any{"language": "en", "sidebar": "hidden"}, "runtime": map[string]any{"mode": "ask"},
		"preferences": map[string]any{"showProcess": false},
		"providers":   map[string]any{"local": map[string]any{"baseURL": endpoint + "/v1", "api": "ollama"}},
		"models":      map[string]any{"local.conversation-fixture": map[string]any{"provider": "local", "api_model": "conversation-fixture", "api": "ollama", "context_window": 16384, "default_max_tokens": 2048}},
		"agents":      map[string]any{"coder": map[string]any{"model": "local.conversation-fixture"}},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(home, ".opcode", "config.json"), raw, 0600))
	return home, workspace, data
}

func conversationCompleted(frame string) bool {
	return strings.Contains(frame, "DIAGNOSIS:") && strings.Contains(frame, "Perfeito") && strings.Contains(frame, "You answered: Agora não") && !strings.Contains(frame, "Question for you") && ptyUIIdleFooter(frame)
}

func assertConversationOrder(t *testing.T, frame string) {
	t.Helper()
	previous := -1
	for _, text := range []string{"Inspecting source", "DIAGNOSIS:", "You answered: Agora não", "Perfeito"} {
		index := strings.Index(frame, text)
		require.Greater(t, index, previous, "%s must retain its chronological position:\n%s", text, frame)
		previous = index
	}
	require.GreaterOrEqual(t, strings.Count(frame, "› Process"), 2, "consecutive tools must form separate collapsed groups")
	require.NotContains(t, frame, "Citations not validated")
	require.NotContains(t, frame, "Model limit reached")
}

func conversationQuit(t *testing.T, h *ptyUIHarness, slash bool) {
	t.Helper()
	if slash {
		h.paste("/quit")
		h.send("\r")
	} else {
		h.send("\x03")
	}
	h.wait("quit confirmation", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "Are you sure you want to quit?") })
	h.send("y")
	select {
	case <-h.exited:
		require.NoError(t, h.command.Wait())
	case <-time.After(10 * time.Second):
		t.Fatalf("quit did not close the shipped process: %s", h.frame())
	}
}

func conversationMessageCount(t *testing.T, data string) int {
	t.Helper()
	address := (&url.URL{Scheme: "file", Path: filepath.Join(data, "opcode.db")}).String() + "?mode=ro"
	conn, err := sql.Open("sqlite3", address)
	require.NoError(t, err)
	defer conn.Close()
	var count int
	require.NoError(t, conn.QueryRow("SELECT COUNT(*) FROM messages").Scan(&count), fmt.Sprintf("database %s", data))
	return count
}
