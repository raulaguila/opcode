//go:build integration

package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/clipboard"
	"github.com/stretchr/testify/require"
)

func TestPTYReasoningVisibleBeforeFinalAndRespectsReader(t *testing.T) {
	if os.Getenv("OPCODE_LIVE_TESTS") != "1" || runtime.GOOS != "linux" {
		t.Skip("explicit Linux PTY acceptance opt-in required")
	}
	for _, mode := range []string{"automatic", "manual", "reading"} {
		t.Run(mode, func(t *testing.T) { runPTYLiveReasoning(t, mode) })
	}
}

func runPTYLiveReasoning(t *testing.T, mode string) {
	public := make(chan struct{})
	var publicOnce sync.Once
	releasePublic := func() { publicOnce.Do(func() { close(public) }) }
	finish := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(finish) }) }
	var generations atomic.Int32
	thinking := "LIVE_PROVIDER_REASONING"
	if mode == "reading" {
		for i := range 60 {
			thinking += fmt.Sprintf("\n\nREASONING_LINE_%02d", i)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveLiveReasoning(t, w, r, thinking, public, finish, &generations)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(release)
	t.Cleanup(releasePublic)
	home, workspace, _ := conversationWorkspace(t, server.URL)
	h := newPTYUIHarnessAtSize(t, os.Getenv("OPCODE_EVAL_BINARY"), home, workspace, 120, 36, "SSH_CONNECTION=isolated-reasoning-acceptance")
	h.wait("initialization", 20*time.Second, func(frame string) bool { return strings.Contains(frame, "Initialize Project") })
	h.send("n")
	h.wait("ready", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Initialize Project") && ptyUIHasFooter(frame)
	})
	h.paste("Explique o projeto em README.md com um exemplo de shell.")
	h.send("\r")
	visible := "LIVE_PROVIDER_REASONING"
	if mode == "reading" {
		visible = "REASONING_LINE_59"
	}
	h.wait("provider reasoning before final", 15*time.Second, func(frame string) bool {
		return strings.Contains(frame, visible) && !strings.Contains(frame, "Fim da explicação.") && !ptyUIIdleFooter(frame)
	})
	anchor := ""
	switch mode {
	case "automatic":
		h.resize(60, 24)
		h.wait("reasoning remains visible after resize", 5*time.Second, func(frame string) bool { return strings.Contains(frame, visible) })
		h.resize(120, 36)
	case "manual":
		h.clickLabel("Model reasoning")
		h.wait("manual fold", 5*time.Second, func(frame string) bool { return !strings.Contains(frame, visible) })
		h.clickLabel("Model reasoning")
		h.wait("manual expansion", 5*time.Second, func(frame string) bool { return strings.Contains(frame, visible) })
	case "reading":
		h.send("\x1b[5~")
		h.wait("reading earlier reasoning", 5*time.Second, func(frame string) bool { return !strings.Contains(frame, visible) && firstReasoningLine(frame) != "" })
		anchor = firstReasoningLine(h.frame())
	}
	require.EqualValues(t, 1, generations.Load())
	releasePublic()
	assertPTYReasoningAtPublicText(t, h, mode, visible, anchor)
	release()
	h.wait("turn and source validation finished", 20*time.Second, func(frame string) bool {
		return ptyUIIdleFooter(frame)
	})
	if mode == "reading" {
		require.Equal(t, anchor, firstReasoningLine(h.frame()), "finalization cannot move the reader or fold their content")
		h.send("\x1b[1;3F") // Alt+End explicitly resumes following the tail.
	}
	h.wait("public final response", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "Fim da explicação.") })
	if mode == "automatic" {
		require.NotContains(t, h.frame(), visible)
		h.clickLabel("Model reasoning")
		h.wait("completed reasoning remains available", 5*time.Second, func(frame string) bool { return strings.Contains(frame, visible) })
		h.send("\x19")
		h.wait("response copy action", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "Copy response") })
		h.send("\r")
		sequence, err := clipboard.OSC52(codeDetailsAnswer)
		require.NoError(t, err)
		h.wait("only the original response is copied", 5*time.Second, func(string) bool { return strings.Contains(h.rawOutput(), sequence) })
		h.send("\x1b")
		h.wait("copy panel dismissed", 5*time.Second, func(frame string) bool {
			return !strings.Contains(frame, "Copy response") && strings.Contains(frame, "Fim da explicação.")
		})
	} else {
		require.Contains(t, h.frame(), visible, "manual expansion and active reading survive completion")
	}
	require.NotContains(t, h.frame(), "Citations not validated")
	require.EqualValues(t, 1, generations.Load(), "presentation must not add inference or citation repairs")
	h.assertComposer()
}

func assertPTYReasoningAtPublicText(t *testing.T, h *ptyUIHarness, mode, visible, anchor string) {
	t.Helper()
	if mode == "reading" {
		h.wait("reader position preserved when public text begins", 5*time.Second, func(frame string) bool {
			return firstReasoningLine(frame) == anchor && !ptyUIIdleFooter(frame)
		})
		return
	}
	h.wait("public text arrives before provider completion", 10*time.Second, func(frame string) bool {
		return strings.Contains(frame, "Fim da explicação.") && !ptyUIIdleFooter(frame)
	})
	require.Equal(t, mode == "manual", strings.Contains(h.frame(), visible), "automatic reasoning folds on public text; manual choices survive")
}

func firstReasoningLine(frame string) string {
	for line := range strings.SplitSeq(frame, "\n") {
		if strings.Contains(line, "REASONING_LINE_") {
			return line
		}
	}
	return ""
}

func serveLiveReasoning(t *testing.T, w http.ResponseWriter, r *http.Request, thinking string, public, finish <-chan struct{}, generations *atomic.Int32) {
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
		_, _ = io.WriteString(w, `{"message":{"content":"Reasoning test"},"done":true,"done_reason":"stop"}`)
		return
	}
	if generations.Add(1) != 1 {
		t.Error("unexpected inference or citation repair")
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"role": "assistant", "thinking": thinking}, "done": false})
	w.(http.Flusher).Flush()
	select {
	case <-public:
	case <-r.Context().Done():
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"role": "assistant", "content": codeDetailsAnswer}, "done": false})
	w.(http.Flusher).Flush()
	select {
	case <-finish:
	case <-r.Context().Done():
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"role": "assistant", "content": ""}, "done": true, "done_reason": "stop", "eval_count": 32, "eval_duration": 1000000000})
}
