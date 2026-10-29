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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/clipboard"
	"github.com/stretchr/testify/require"
)

// Keep the provider connection open at each checkpoint so a rate appearing only
// after completion cannot pass. The shipped binary runs in a real PTY; only the
// native Ollama HTTP stream is a controlled loopback fixture.
func TestPTYStreamingSpeedBeforeCompletion(t *testing.T) {
	for _, variant := range []struct {
		name string
		env  []string
	}{
		{"dark", []string{"COLORFGBG=15;0"}},
		{"light", []string{"COLORFGBG=0;15"}},
		{"no-color", []string{"NO_COLOR=1"}},
	} {
		t.Run(variant.name, func(t *testing.T) { runPTYStreamingSpeedBeforeCompletion(t, variant.env...) })
	}
}

func runPTYStreamingSpeedBeforeCompletion(t *testing.T, env ...string) {
	if os.Getenv("OPCODE_LIVE_TESTS") != "1" {
		t.Skip("explicit PTY acceptance opt-in required")
	}
	if runtime.GOOS != "linux" {
		t.Skip("Linux PTY acceptance")
	}
	binary := os.Getenv("OPCODE_EVAL_BINARY")
	require.True(t, filepath.IsAbs(binary))
	first, second, complete := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var firstOnce, secondOnce, completeOnce sync.Once
	var finalSent atomic.Bool
	releaseFirst := func() { firstOnce.Do(func() { close(first) }) }
	releaseSecond := func() { secondOnce.Do(func() { close(second) }) }
	releaseComplete := func() { completeOnce.Do(func() { close(complete) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body struct{ Stream bool }
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if !body.Stream {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"message":{"content":"Speed test"},"done":true,"done_reason":"stop"}`)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		flush := w.(http.Flusher)
		flush.Flush()
		for _, step := range []struct {
			ready <-chan struct{}
			frame string
		}{
			{first, `{"message":{"content":"SPEED_BEGIN "},"done":false}`},
			{second, `{"message":{"content":"Streaming output while the provider is still working. "},"done":false}`},
			{complete, `{"message":{"content":"SPEED_DONE"},"done":true,"done_reason":"stop","prompt_eval_count":20,"eval_count":40,"eval_duration":2000000000}`},
		} {
			select {
			case <-step.ready:
			case <-r.Context().Done():
				return
			}
			if step.ready == complete {
				finalSent.Store(true)
			}
			io.WriteString(w, step.frame+"\n")
			flush.Flush()
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { releaseFirst(); releaseSecond(); releaseComplete() })
	root := t.TempDir()
	home, workspace := filepath.Join(root, "home"), filepath.Join(root, "project")
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".opcode"), 0700))
	require.NoError(t, os.MkdirAll(workspace, 0700))
	configuration := map[string]any{
		"data": map[string]any{"directory": filepath.Join(root, "data")},
		"tui":  map[string]any{"language": "en", "sidebar": "hidden"}, "runtime": map[string]any{"mode": "ask"},
		"providers": map[string]any{"local": map[string]any{"baseURL": server.URL + "/v1", "api": "ollama"}},
		"models":    map[string]any{"local.speed-fixture": map[string]any{"provider": "local", "api_model": "speed-fixture", "api": "ollama", "context_window": 16384, "default_max_tokens": 2048}},
		"agents":    map[string]any{"coder": map[string]any{"model": "local.speed-fixture"}},
	}
	raw, err := json.Marshal(configuration)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(home, ".opcode", "config.json"), raw, 0600))
	env = append(env, "SSH_CONNECTION=isolated-selection-acceptance")
	h := newPTYUIHarnessAtSize(t, binary, home, workspace, 180, 36, env...)
	h.wait("initialization", 20*time.Second, func(frame string) bool { return strings.Contains(frame, "Initialize Project") })
	h.send("n")
	h.wait("initialization dismissed", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Initialize Project") && ptyUIHasFooter(frame)
	})
	h.paste("Responda somente SPEED_DONE sem ferramentas.")
	h.send("\r")
	h.wait("awaiting first generated data", 15*time.Second, func(frame string) bool {
		return strings.Contains(ptyUIFooter(frame), "Awaiting model…")
	})
	releaseFirst()
	liveRate := regexp.MustCompile(`~[0-9]+(?:\.[0-9]+)? tok/s`)
	h.wait("first generated data received", 5*time.Second, func(frame string) bool {
		return strings.Contains(ptyUIFooter(frame), "Generating") && !liveRate.MatchString(ptyUIFooter(frame))
	})
	firstSeen := time.Now()
	h.wait("one buffered burst has no invented rate", 5*time.Second, func(frame string) bool {
		return time.Since(firstSeen) >= 350*time.Millisecond && strings.Contains(ptyUIFooter(frame), "Generating") && !liveRate.MatchString(ptyUIFooter(frame))
	})
	releaseSecond()
	h.wait("estimated speed before provider completion", 10*time.Second, func(frame string) bool {
		return liveRate.MatchString(ptyUIFooter(frame)) && !ptyUIIdleFooter(frame)
	})
	require.False(t, finalSent.Load(), "speed must be visible before the server sends completion")
	h.wait("more output with a live estimate", 5*time.Second, func(frame string) bool {
		return strings.Contains(frame, "Streaming output while") && liveRate.MatchString(ptyUIFooter(frame))
	})
	h.assertComposer()
	releaseComplete()
	h.wait("exact final provider speed", 10*time.Second, func(frame string) bool {
		footer := ptyUIFooter(frame)
		return ptyUIIdleFooter(frame) && strings.Contains(footer, "20.0 tok/s") && !strings.Contains(footer, "~20.0 tok/s")
	})
	h.assertComposer()

	// A drag highlights text without opening actions or copying on release.
	selected := "Streaming output"
	x, y := -1, -1
	for row, line := range strings.Split(h.frame(), "\n") {
		if at := strings.Index(line, selected); at >= 0 {
			x, y = ansi.StringWidth(line[:at]), row
			break
		}
	}
	require.NotEqual(t, -1, x)
	h.mu.Lock()
	before := h.screen.Cell(x, y)
	h.mu.Unlock()
	h.send(fmt.Sprintf("\x1b[<0;%d;%dM\x1b[<32;%d;%dM\x1b[<0;%d;%dm", x+1, y+1, x+len(selected), y+1, x+len(selected), y+1))
	h.wait("drag highlight with no menu", 5*time.Second, func(frame string) bool {
		h.mu.Lock()
		highlighted := h.screen.Cell(x, y).Mode != before.Mode
		h.mu.Unlock()
		return highlighted && !strings.Contains(frame, "Copy response")
	})
	wantCopy, err := clipboard.OSC52(selected)
	require.NoError(t, err)
	require.NotContains(t, h.rawOutput(), wantCopy, "highlighting alone must not copy")
	h.send("\x03") // With a selection, copy; never open Quit or interrupt the model.
	h.wait("only selected text copied", 5*time.Second, func(frame string) bool {
		return strings.Contains(h.rawOutput(), wantCopy) && !strings.Contains(frame, "Quit") && !strings.Contains(frame, "Copy response")
	})
	h.send("\x1b")
	h.wait("Escape clears highlight", 5*time.Second, func(frame string) bool {
		h.mu.Lock()
		cleared := h.screen.Cell(x, y).Mode == before.Mode
		h.mu.Unlock()
		return cleared && strings.Contains(frame, "SPEED_DONE")
	})
	h.assertComposer()
}
