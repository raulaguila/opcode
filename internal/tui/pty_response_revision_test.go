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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const revisionOriginal = "REVISION_ORIGINAL: Este projeto coleta sensores em SQLite."
const revisionCandidate = "REVISION_CORRECTED: Coleta sensores em SQLite"

type ptyRevisionProvider struct {
	t          *testing.T
	fail       bool
	started    chan struct{}
	complete   chan struct{}
	once       sync.Once
	requests   atomic.Int32
	generation atomic.Int32
}

func (p *ptyRevisionProvider) release() { p.once.Do(func() { close(p.complete) }) }

// A correction is held after its first streamed bytes. Passing requires the
// original to remain primary until the application's real citation gate passes.
func TestPTYResponseRevisionPromotesOnlyVerifiedRepairAndPreservesFailures(t *testing.T) {
	if os.Getenv("OPCODE_LIVE_TESTS") != "1" {
		t.Skip("explicit PTY acceptance opt-in required")
	}
	if runtime.GOOS != "linux" {
		t.Skip("Linux PTY acceptance")
	}
	binary := os.Getenv("OPCODE_EVAL_BINARY")
	require.True(t, filepath.IsAbs(binary))
	for _, fail := range []bool{false, true} {
		name := "verified correction replaces primary and keeps history"
		if fail {
			name = "failed correction preserves original"
		}
		t.Run(name, func(t *testing.T) { runPTYResponseRevision(t, binary, fail) })
	}
}

func runPTYResponseRevision(t *testing.T, binary string, fail bool) {
	p := &ptyRevisionProvider{t: t, fail: fail, started: make(chan struct{}), complete: make(chan struct{})}
	server := httptest.NewServer(http.HandlerFunc(p.serveHTTP))
	t.Cleanup(server.Close)
	t.Cleanup(p.release)
	home, workspace, data := conversationWorkspace(t, server.URL)
	h := newPTYUIHarnessAtSize(t, binary, home, workspace, 160, 44)
	h.wait("initialization", 20*time.Second, func(frame string) bool { return strings.Contains(frame, "Initialize Project") })
	h.send("n")
	h.wait("ready", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Initialize Project") && ptyUIHasFooter(frame)
	})
	h.paste("Explique o projeto atual com referências aos arquivos lidos.")
	h.send("\r")
	select {
	case <-p.started:
	case <-time.After(20 * time.Second):
		t.Fatalf("citation gate did not request a correction: %s", h.frame())
	}
	h.wait("original remains primary while correction streams", 10*time.Second, func(frame string) bool {
		return strings.Contains(frame, revisionOriginal) && strings.Contains(frame, "Revising references") && strings.Contains(ptyUIFooter(frame), "Generating") && !strings.Contains(frame, "REVISION_CORRECTED")
	})
	require.EqualValues(t, 2, p.generation.Load())
	p.release()
	if fail {
		assertPTYFailedRevision(t, h, p)
	} else {
		assertPTYVerifiedRevision(t, h, p)
	}
	conversationQuit(t, h, true)
	count, before := conversationMessageCount(t, data), p.requests.Load()
	h = newPTYUIHarnessWithArgs(t, binary, home, workspace, 160, 44, []string{"--resume"})
	if fail {
		assertPTYFailedRevision(t, h, p)
	} else {
		assertPTYVerifiedRevision(t, h, p)
	}
	require.Equal(t, before, p.requests.Load(), "restoring revisions must not make inference requests")
	require.Equal(t, count, conversationMessageCount(t, data), "history restoration must not create messages")
	h.assertComposer()
	conversationQuit(t, h, true)
}

func assertPTYVerifiedRevision(t *testing.T, h *ptyUIHarness, p *ptyRevisionProvider) {
	t.Helper()
	h.wait("verified correction is the sole primary answer", 15*time.Second, func(frame string) bool {
		return strings.Contains(frame, revisionCandidate) && strings.Contains(frame, "README.md:1-2") && strings.Contains(frame, "View previous version") && !strings.Contains(frame, "REVISION_ORIGINAL") && !strings.Contains(frame, "Revising references") && ptyUIIdleFooter(frame)
	})
	require.Equal(t, 1, strings.Count(h.frame(), "REVISION_CORRECTED"))
	require.EqualValues(t, 2, p.generation.Load())
	h.clickLabel("View previous version")
	h.wait("previous answer is available on explicit request", 5*time.Second, func(frame string) bool {
		return strings.Contains(frame, revisionOriginal) && strings.Contains(frame, revisionCandidate)
	})
	require.EqualValues(t, 2, p.generation.Load())
}

func assertPTYFailedRevision(t *testing.T, h *ptyUIHarness, p *ptyRevisionProvider) {
	t.Helper()
	h.wait("failed repair retains original and validation warning", 15*time.Second, func(frame string) bool {
		return strings.Contains(frame, revisionOriginal) && strings.Contains(frame, "Citations not validated") && strings.Contains(frame, "View correction attempt") && !strings.Contains(frame, "REVISION_CORRECTED") && !strings.Contains(frame, "Revising references") && !strings.Contains(ptyUIFooter(frame), "Esc cancels")
	})
	require.EqualValues(t, 2, p.generation.Load(), "the citation gate must stop after its existing repair budget")
	require.NotContains(t, h.frame(), "Model limit reached")
	h.clickLabel("View correction attempt")
	h.wait("failed candidate is inspectable without replacing the original", 5*time.Second, func(frame string) bool {
		return strings.Contains(frame, revisionOriginal) && strings.Contains(frame, revisionCandidate)
	})
}

func (p *ptyRevisionProvider) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/chat" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	p.requests.Add(1)
	if r.Header.Get("Authorization") != "" {
		p.t.Error("isolated revision fixture must not receive credentials")
	}
	var body struct{ Stream bool }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		p.t.Error(err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if !body.Stream {
		_, _ = io.WriteString(w, `{"message":{"content":"Revision fixture"},"done":true,"done_reason":"stop"}`)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	switch p.generation.Add(1) {
	case 1:
		writePTYRevisionChunk(w, revisionOriginal, true)
	case 2:
		p.streamCorrection(w, r)
	default:
		p.t.Error("unexpected additional generation or relaxed repair budget")
		writePTYRevisionChunk(w, "UNEXPECTED_REVISION_GENERATION", true)
	}
}

func (p *ptyRevisionProvider) streamCorrection(w http.ResponseWriter, r *http.Request) {
	writePTYRevisionChunk(w, revisionCandidate, false)
	w.(http.Flusher).Flush()
	close(p.started)
	select {
	case <-r.Context().Done():
		return
	case <-p.complete:
	}
	suffix := " (README.md:1-2)."
	if p.fail {
		suffix = "."
	}
	writePTYRevisionChunk(w, suffix, true)
}

func writePTYRevisionChunk(w http.ResponseWriter, content string, done bool) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"message": map[string]any{"role": "assistant", "content": content}, "done": done,
		"done_reason": "stop", "prompt_eval_count": 40, "eval_count": 18, "eval_duration": 1000000000,
	})
}
