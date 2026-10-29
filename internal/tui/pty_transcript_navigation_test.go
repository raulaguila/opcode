//go:build integration

package tui

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Exercise real keyboard decoding and focus ownership, with a native Ollama
// loopback fixture so copying/searching cannot depend on model/tool behavior.
func TestPTYTranscriptSearchAndKeyboardSelection(t *testing.T) {
	if os.Getenv("OPCODE_LIVE_TESTS") != "1" {
		t.Skip("explicit PTY acceptance opt-in required")
	}
	binary := os.Getenv("OPCODE_EVAL_BINARY")
	require.True(t, filepath.IsAbs(binary))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		io.WriteString(w, `{"message":{"content":"NAV_ALPHA first.\n\nNAV_ALPHA last ação 漢字."},"done":true,"done_reason":"stop","prompt_eval_count":20,"eval_count":18,"eval_duration":1000000000}`+"\n")
	}))
	defer server.Close()
	root := t.TempDir()
	home, workspace := filepath.Join(root, "home"), filepath.Join(root, "project")
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".opcode"), 0700))
	require.NoError(t, os.MkdirAll(workspace, 0700))
	raw, err := json.Marshal(map[string]any{
		"data": map[string]any{"directory": filepath.Join(root, "data")},
		"tui":  map[string]any{"language": "en", "sidebar": "hidden"}, "runtime": map[string]any{"mode": "ask"},
		"providers": map[string]any{"local": map[string]any{"baseURL": server.URL + "/v1", "api": "ollama"}},
		"models":    map[string]any{"local.navigation-fixture": map[string]any{"provider": "local", "api_model": "navigation-fixture", "api": "ollama", "context_window": 16384, "default_max_tokens": 2048}},
		"agents":    map[string]any{"coder": map[string]any{"model": "local.navigation-fixture"}},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(home, ".opcode", "config.json"), raw, 0600))
	h := newPTYUIHarnessAtSize(t, binary, home, workspace, 120, 32, "SSH_CONNECTION=isolated-keyboard-navigation")
	h.wait("initialization", 20*time.Second, func(frame string) bool { return strings.Contains(frame, "Initialize Project") })
	h.send("n")
	h.wait("ready", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Initialize Project") && ptyUIHasFooter(frame)
	})
	h.paste("Reply with the requested markers only, without tools.")
	h.send("\r")
	h.wait("completed fixture answer", 15*time.Second, func(frame string) bool { return strings.Contains(frame, "NAV_ALPHA last") && ptyUIIdleFooter(frame) })
	draft := "UNSENT_DRAFT ação"
	h.paste(draft)
	h.send("\x1bf") // Alt+F; no composer interception.
	h.wait("transcript find focus", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "Find:") && strings.Contains(frame, draft) })
	h.send("NAV_ALPHA")
	h.wait("two transcript matches", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "1/2") && strings.Contains(frame, draft) })
	h.send("\r")
	h.wait("jump to second result", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "2/2") })
	h.send("\x1bs") // Alt+S begins keyboard selection at current result.
	h.wait("keyboard selection", 5*time.Second, func(frame string) bool {
		return strings.Contains(frame, "Shift+arrows") && !strings.Contains(frame, "Find:")
	})
	for i := 0; i < 8; i++ {
		h.send("\x1b[57351;2u")
	}
	h.send("\x03")
	expected := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("NAV_ALPHA")) + "\x07"
	h.wait("exact selected text copied over OSC52", 5*time.Second, func(frame string) bool {
		return strings.Contains(h.rawOutput(), expected) && strings.Contains(frame, draft)
	})
	require.NotContains(t, h.frame(), "Copy response")
	h.send("\x1b")
	h.wait("selection cleared with draft preserved", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Shift+arrows") && strings.Contains(frame, draft)
	})
	h.clearDraft()
	h.paste("/search NAV_ALPHA")
	h.send("\r")
	h.wait("slash transcript search", 5*time.Second, func(frame string) bool {
		return strings.Contains(frame, "Find: NAV_ALPHA") && strings.Contains(frame, "1/2")
	})
	h.send("\x1b")
	h.wait("search focus released", 5*time.Second, func(frame string) bool { return !strings.Contains(frame, "Find:") })
	h.paste(draft)
	h.wait("composer accepts input after find", 5*time.Second, func(frame string) bool { return strings.Contains(frame, draft) })
	// Connection editing owns focus, then returns to the same catalog and query.
	// The fixture intentionally lacks /models: failure must keep that provider
	// selected and editable without losing the composer draft.
	h.send("\x0f")
	h.wait("catalog after navigation", 10*time.Second, func(frame string) bool { return strings.Contains(frame, "Choose model") })
	h.send("fixture")
	h.send("\x10")
	h.wait("current connection editor", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "Configure provider") })
	h.send("\x1b")
	h.wait("connection list after editor", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "Connections and preferences") })
	h.send("\x1b")
	h.wait("same catalog and search after connection back", 10*time.Second, func(frame string) bool {
		return strings.Contains(frame, "Choose model") && strings.Contains(frame, "fixture") && strings.Contains(frame, draft)
	})
	h.send("\x1b")
	h.wait("composer after connection flow", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Choose model") && strings.Contains(frame, draft)
	})
	h.assertComposer()
}
