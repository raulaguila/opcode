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
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/clipboard"
	"github.com/stretchr/testify/require"
)

const codeDetailsAnswer = "O projeto coleta sensores em SQLite (README.md:1-2).\n\n```bash\nprintf 'ação 漢字\\n'\n```\n\nFim da explicação."
const codeDetailsThinking = "CODE_DETAILS_INTERNAL_TRACE"

func TestPTYCodeBlocksAndReasoningDetails(t *testing.T) {
	if os.Getenv("OPCODE_LIVE_TESTS") != "1" || runtime.GOOS != "linux" {
		t.Skip("explicit Linux PTY acceptance opt-in required")
	}
	binary := os.Getenv("OPCODE_EVAL_BINARY")
	require.True(t, filepath.IsAbs(binary))
	var generations atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveCodeDetailsAnswer(t, &generations, w, r)
	}))
	defer server.Close()
	home, workspace, _ := conversationWorkspace(t, server.URL)
	h := newPTYUIHarnessAtSize(t, binary, home, workspace, 120, 36, "COLORTERM=truecolor", "COLORFGBG=15;0", "SSH_CONNECTION=isolated-acceptance")
	h.wait("initialization", 20*time.Second, func(frame string) bool { return strings.Contains(frame, "Initialize Project") })
	h.send("n")
	h.wait("ready", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Initialize Project") && ptyUIHasFooter(frame)
	})
	h.paste("Explique o projeto em README.md com um exemplo de shell.")
	h.send("\r")
	h.wait("code answer completed", 20*time.Second, func(frame string) bool {
		return strings.Contains(frame, "Fim da explicação.") && strings.Contains(ptyUIFooter(frame), "Completed") && ptyUIIdleFooter(frame)
	})
	assertPTYCodeDetails(t, h)
	require.EqualValues(t, 1, generations.Load(), "presentation must not trigger citation repair")
	saveProposalPTYFrame(t, h)
	frameBytes, err := os.ReadFile(filepath.Join(os.TempDir(), "opcode-proposal-pty-frame.json"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(os.TempDir(), "opcode-code-details-pty-frame.json"), frameBytes, 0600))
	h.clickLabel("Details")
	h.wait("internal details expanded before their response", 5*time.Second, func(frame string) bool {
		trace, answer := strings.Index(frame, codeDetailsThinking), strings.Index(frame, "O projeto coleta")
		return trace >= 0 && answer > trace && strings.Contains(frame, "Fim da explicação.")
	})
	h.clickLabel("Details")
	h.wait("details collapsed", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, codeDetailsThinking) && strings.Contains(frame, "Fim da explicação.")
	})
	h.send("\x19")
	h.wait("explicit copy action", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "Copy response") })
	h.send("\r")
	expected, err := clipboard.OSC52(codeDetailsAnswer)
	require.NoError(t, err)
	h.wait("original Markdown copied exactly", 5*time.Second, func(string) bool { return strings.Contains(h.rawOutput(), expected) })
	h.send("\x1b")
	conversationQuit(t, h, true)
}

func serveCodeDetailsAnswer(t *testing.T, generations *atomic.Int32, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	if r.URL.Path != "/api/chat" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	var request struct{ Stream bool }
	if json.NewDecoder(r.Body).Decode(&request) != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if !request.Stream {
		_, _ = io.WriteString(w, `{"message":{"content":"Code details"},"done":true,"done_reason":"stop"}`)
		return
	}
	if generations.Add(1) != 1 {
		t.Error("unexpected inference or citation repair")
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"role": "assistant", "thinking": codeDetailsThinking, "content": codeDetailsAnswer}, "done": true, "done_reason": "stop", "eval_count": 32, "eval_duration": 1000000000})
}

func assertPTYCodeDetails(t *testing.T, h *ptyUIHarness) {
	t.Helper()
	frame := h.frame()
	require.NotContains(t, frame, "reasoning · finished")
	require.NotContains(t, frame, codeDetailsThinking)
	require.Contains(t, frame, "bash")
	require.Contains(t, frame, "printf 'ação 漢字\\n'")
	require.Contains(t, frame, "README.md:1-2")
	require.NotContains(t, frame, "Validating citations")
	require.NotContains(t, frame, "View correction attempt")
	require.NotContains(t, frame, "Citations not validated")
	h.mu.Lock()
	defer h.mu.Unlock()
	var codeBG, proseBG uint32
	for y, line := range strings.Split(frame, "\n") {
		if x := strings.Index(line, "printf"); x >= 0 {
			codeBG = uint32(h.screen.Cell(ansi.StringWidth(line[:x]), y).BG)
		}
		if x := strings.Index(line, "Fim da explicação."); x >= 0 {
			proseBG = uint32(h.screen.Cell(ansi.StringWidth(line[:x]), y).BG)
		}
	}
	require.NotEqual(t, proseBG, codeBG, "code needs a distinct surface from surrounding prose")
}
