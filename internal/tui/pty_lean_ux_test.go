//go:build integration

package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPTYLeanApprovalDiscoveryAndModelContext(t *testing.T) {
	if os.Getenv("OPCODE_LIVE_TESTS") != "1" || runtime.GOOS != "linux" {
		t.Skip("explicit Linux PTY acceptance opt-in required")
	}
	var inference atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		servePickerPTYCatalog(t, &inference, w, r)
	}))
	defer server.Close()
	home, workspace, _ := conversationWorkspace(t, server.URL)
	h := newPTYUIHarnessAtSize(t, os.Getenv("OPCODE_EVAL_BINARY"), home, workspace, 180, 36)
	waitLeanPTYReady(h)
	h.paste("KEEP_LEAN_DRAFT")
	h.send("\x0b")
	h.wait("palette exposes count and continuation", 5*time.Second, func(frame string) bool {
		// The corner counts every command.
		return strings.Contains(frame, "Search commands") && regexp.MustCompile(`Commands ─+ \d+ ─`).MatchString(frame)
	})
	h.paste("approval")
	h.wait("approval is searchable", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "Change approval") })
	h.send("\r")
	h.wait("approval chooser explains read-only mode", 5*time.Second, func(frame string) bool {
		return strings.Contains(frame, "Work mode") && strings.Contains(frame, "Read only in this mode") && strings.Contains(frame, "✓ Ask")
	})
	h.send("\x1b[B\r")
	h.wait("selected policy does not grant edits in Ask", 5*time.Second, func(frame string) bool {
		footer := ptyUIFooter(frame)
		return !strings.Contains(frame, "Work mode") && ptyUIMode(frame) == "Ask" && strings.Contains(footer, "Approval: project edits") && strings.Contains(footer, "Read only in this mode")
	})
	h.resize(60, 20)
	h.wait("idle compact footer retains model identity", 6*time.Second, func(frame string) bool {
		return strings.Contains(ptyUIFooter(frame), "conv") && strings.Contains(frame, "KEEP_LEAN_DRAFT")
	})
	h.resize(80, 24)
	h.send("\x0f")
	h.wait("catalog model row carries its context", 10*time.Second, leanCatalogContextVisible)
	h.send("\x1b")
	h.wait("catalog inspection preserves draft", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Choose model") && strings.Contains(frame, "KEEP_LEAN_DRAFT")
	})
	require.Zero(t, inference.Load(), "navigation, policy selection and metadata must never invoke the model")
	h.assertComposer()
}

func leanCatalogContextVisible(frame string) bool {
	for line := range strings.SplitSeq(frame, "\n") {
		if strings.Contains(strings.ToLower(line), "conversation") && strings.Contains(line, "16k") && strings.Contains(line, "✓") {
			return true
		}
	}
	return false
}

func waitLeanPTYReady(h *ptyUIHarness) {
	h.t.Helper()
	h.wait("initialization", 20*time.Second, func(frame string) bool { return strings.Contains(frame, "Initialize Project") })
	h.send("n")
	h.wait("ready", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Initialize Project") && ptyUIHasFooter(frame)
	})
}

func TestPTYRecoveryTaskActionAndBlankClick(t *testing.T) {
	if os.Getenv("OPCODE_LIVE_TESTS") != "1" || runtime.GOOS != "linux" {
		t.Skip("explicit Linux PTY acceptance opt-in required")
	}
	var generations atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveLeanBudget(t, w, r, &generations)
	}))
	defer server.Close()
	home, workspace, _ := conversationWorkspace(t, server.URL)
	path := filepath.Join(home, ".opcode", "config.json")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var cfg map[string]any
	require.NoError(t, json.Unmarshal(raw, &cfg))
	cfg["runtime"].(map[string]any)["maxSteps"] = 1
	raw, err = json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0600))
	h := newPTYUIHarnessAtSize(t, os.Getenv("OPCODE_EVAL_BINARY"), home, workspace, 120, 36)
	waitLeanPTYReady(h)
	h.paste("Leia README.md usando view.")
	h.send("\r")
	h.wait("step budget recovery offers task", 15*time.Second, func(frame string) bool {
		return strings.Contains(frame, "Run limit reached") && strings.Contains(frame, "/task Review task")
	})
	h.paste("KEEP_RECOVERY_DRAFT")
	h.resize(60, 20)
	for row, line := range strings.Split(h.frame(), "\n") {
		if strings.Contains(line, "/task") && strings.Contains(line, "/error") {
			h.send(fmt.Sprintf("\x1b[<0;60;%dM\x1b[<0;60;%dm", row+1, row+1))
			break
		}
	}
	h.clickLabel("/task")
	h.wait("recovery task click opens task rather than error", 5*time.Second, testLiveOllamaPTYUITaskOverview)
	require.NotContains(t, h.frame(), "Execution details")
	h.send("\x1b")
	h.wait("recovery navigation preserves the unsent draft", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Expected outcome") && strings.Contains(frame, "KEEP_RECOVERY_DRAFT") && strings.Contains(frame, "Run limit reached")
	})
	require.EqualValues(t, 1, generations.Load(), "blank clicks and task inspection cannot retry inference")
	h.assertComposer()
}

func serveLeanBudget(t *testing.T, w http.ResponseWriter, r *http.Request, generations *atomic.Int32) {
	t.Helper()
	if r.URL.Path != "/api/chat" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	var request struct{ Stream bool }
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		t.Error(err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if !request.Stream {
		_, _ = io.WriteString(w, `{"message":{"content":"Budget fixture"},"done":true,"done_reason":"stop"}`)
		return
	}
	if generations.Add(1) != 1 {
		t.Error("unexpected extra generation")
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"function": map[string]any{"name": "view", "arguments": map[string]any{"file_path": "README.md", "offset": 0, "limit": 2}}}}}, "done": true, "done_reason": "stop"})
}
