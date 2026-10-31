//go:build integration

package tui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPTYCompactListsPreserveDensityOnResize(t *testing.T) {
	if os.Getenv("OPCODE_LIVE_TESTS") != "1" || runtime.GOOS != "linux" {
		t.Skip("explicit Linux PTY acceptance opt-in required")
	}
	answer := "Lista de sensores (README.md:1-2).\n\n1. ITEM_ONE " + strings.Repeat("coleta de sensores ", 10) + "\n2. ITEM_TWO SQLite.\n\nFim da lista."
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct{ Stream bool }
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		if !request.Stream {
			_, _ = io.WriteString(w, `{"message":{"content":"Compact list"},"done":true}`)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"role": "assistant", "content": answer}, "done": true, "done_reason": "stop"}))
	}))
	defer server.Close()
	home, workspace, _ := conversationWorkspace(t, server.URL)
	h := newPTYUIHarnessAtSize(t, os.Getenv("OPCODE_EVAL_BINARY"), home, workspace, 120, 36, "SSH_CONNECTION=isolated-acceptance")
	h.wait("initialization", 20*time.Second, func(frame string) bool { return strings.Contains(frame, "Initialize Project") })
	h.send("n")
	h.wait("ready", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Initialize Project") && ptyUIHasFooter(frame)
	})
	h.paste("Explique o projeto em README.md com uma lista.")
	h.send("\r")
	h.wait("list completed", 20*time.Second, func(frame string) bool {
		return strings.Contains(frame, "Fim da lista.") && strings.Contains(ptyUIFooter(frame), "Completed") && ptyUIIdleFooter(frame)
	})
	for _, width := range []int{120, 60, 160} {
		h.resize(width, 36)
		h.wait("list reflowed", 5*time.Second, func(frame string) bool {
			return strings.Contains(frame, "ITEM_ONE") && strings.Contains(frame, "ITEM_TWO") && strings.Contains(frame, "Fim da lista.")
		})
		assertPTYCompactList(t, h.frame())
	}
	assertPTYSourceAndReadingActions(t, h)
	raw, err := os.ReadFile(filepath.Join(home, ".opcode", "config.json"))
	require.NoError(t, err)
	var saved struct {
		TUI struct {
			ProseWidth int `json:"proseWidth"`
		} `json:"tui"`
	}
	require.NoError(t, json.Unmarshal(raw, &saved))
	require.Equal(t, 96, saved.TUI.ProseWidth)
	conversationQuit(t, h, false)
}

func assertPTYCompactList(t *testing.T, frame string) {
	t.Helper()
	require.NotContains(t, frame, "Citations not validated")
	lines := strings.Split(frame, "\n")
	for i, line := range lines {
		if strings.Contains(line, "ITEM_TWO") {
			require.Greater(t, i, 0)
			previous := strings.TrimSpace(strings.TrimLeft(lines[i-1], " │┃"))
			require.NotEmpty(t, previous, "wrapped item must not add a blank separator: %s", frame)
			return
		}
	}
	t.Fatal("second list item is not visible")
}

func assertPTYSourceAndReadingActions(t *testing.T, h *ptyUIHarness) {
	t.Helper()
	h.paste("KEEP_READING_DRAFT")
	h.send("\x19")
	h.wait("response actions", 5*time.Second, func(frame string) bool {
		return strings.Contains(frame, "Copy response") && strings.Contains(frame, "Cited sources")
	})
	h.clickLabel("Cited sources")
	h.wait("recorded source validation", 5*time.Second, func(frame string) bool {
		return strings.Contains(frame, "answer was generated") && strings.Contains(frame, "not been revalidated now")
	})
	h.send("\x1b")
	h.wait("back to response actions", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "Copy response") })
	h.send("m")
	h.wait("reading preset discoverable", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "Comfortable reading") })
	h.clickLabel("Comfortable reading")
	h.wait("reading width applied", 5*time.Second, func(frame string) bool {
		return strings.Contains(frame, "Reading width saved") && strings.Contains(frame, "96 columns ✓")
	})
	h.send("\x1b")
	h.wait("draft survives reflow", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Copy response") && strings.Contains(frame, "KEEP_READING_DRAFT")
	})
}
