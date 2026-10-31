//go:build integration

package tui

import (
	"encoding/json"
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

// Catalog inspection and navigation must work without inference or implicit
// consent. Exercise the actual terminal decoder, renderer and mouse routes.
func TestPTYModelCapabilitiesAndModalNavigation(t *testing.T) {
	if os.Getenv("OPCODE_LIVE_TESTS") != "1" || runtime.GOOS != "linux" {
		t.Skip("explicit Linux PTY acceptance opt-in required")
	}
	binary := os.Getenv("OPCODE_EVAL_BINARY")
	require.True(t, filepath.IsAbs(binary))
	var inference atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		servePickerPTYCatalog(t, &inference, w, r)
	}))
	defer server.Close()
	home, workspace, _ := conversationWorkspace(t, server.URL)
	h := newPTYUIHarnessAtSize(t, binary, home, workspace, 120, 36, "COLORTERM=truecolor", "COLORFGBG=15;0")
	h.wait("initialization", 20*time.Second, func(frame string) bool { return strings.Contains(frame, "Initialize Project") })
	h.send("n")
	h.wait("ready", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Initialize Project") && ptyUIHasFooter(frame)
	})
	const draft = "KEEP_DRAFT ação 漢字"
	h.paste(draft)
	h.wait("unsent draft", 5*time.Second, func(frame string) bool { return strings.Contains(frame, draft) })
	assertPickerPTYCapabilities(t, h)
	for _, size := range [][2]int{{60, 16}, {80, 24}, {120, 36}} {
		assertPickerPTYSettingsClick(t, h, size, draft)
	}
	require.Zero(t, inference.Load(), "catalogs, details and menus must not generate tokens")
	require.NoFileExists(t, filepath.Join(workspace, "Opcode.md"))
	conversationQuit(t, h, false)
}

func servePickerPTYCatalog(t *testing.T, inference *atomic.Int32, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	if r.Header.Get("Authorization") != "" {
		t.Error("isolated fixture must not inherit credentials")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/api/tags", "/v1/models":
		entries := []map[string]any{
			{"name": "conversation-fixture", "id": "conversation-fixture", "remote_host": "", "capabilities": []string{"completion", "tools"}},
			{"name": "EMBED_UI_ONLY", "id": "EMBED_UI_ONLY", "remote_host": "", "capabilities": []string{"embedding"}},
			{"name": "REMOTE_UI_CHAT", "id": "REMOTE_UI_CHAT", "remote_host": "https://ollama.com", "capabilities": []string{"completion"}},
			{"name": "UNKNOWN_UI_CHAT", "id": "UNKNOWN_UI_CHAT"},
		}
		writePickerPTYJSON(t, w, map[string]any{"models": entries, "data": entries})
	case "/api/show":
		var request struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		metadata := map[string]any{"model_info": map[string]any{"llama.context_length": 65536}}
		if request.Model == "EMBED_UI_ONLY" {
			metadata["capabilities"] = []string{"embedding"}
		} else if request.Model != "UNKNOWN_UI_CHAT" {
			metadata["capabilities"] = []string{"completion", "tools"}
		}
		writePickerPTYJSON(t, w, metadata)
	case "/api/ps":
		writePickerPTYJSON(t, w, map[string]any{"models": []any{}})
	case "/api/chat", "/v1/chat/completions":
		inference.Add(1)
		t.Error("menu navigation attempted inference")
		w.WriteHeader(http.StatusBadRequest)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func writePickerPTYJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Error(err)
	}
}

func assertPickerPTYCapabilities(t *testing.T, h *ptyUIHarness) {
	t.Helper()
	h.send("\x0f")
	h.wait("explicit capabilities and hosting", 10*time.Second, func(frame string) bool {
		return strings.Contains(frame, "Choose model") && strings.Contains(frame, "REMOTE_UI_CHAT") && strings.Contains(frame, "Cloud") && strings.Contains(frame, "Local") && strings.Contains(frame, "UNKNOWN_UI_CHAT")
	})
	require.NotContains(t, h.frame(), "EMBED_UI_ONLY", "embedding-only entries must not be offered for chat")
	h.clickLabel("Ctrl+D details")
	h.wait("metadata details", 5*time.Second, func(frame string) bool {
		return strings.Contains(frame, "Capabilities:") && strings.Contains(frame, "Effective:")
	})
	h.send("\x1b")
	h.wait("draft restored", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Choose model") && strings.Contains(frame, "KEEP_DRAFT")
	})
}

func assertPickerPTYSettingsClick(t *testing.T, h *ptyUIHarness, size [2]int, draft string) {
	t.Helper()
	h.resize(size[0], size[1])
	h.wait("draft after resize", 5*time.Second, func(frame string) bool { return strings.Contains(frame, draft) })
	h.send("\x0b")
	h.wait("command palette", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "Search commands") })
	h.paste("settings")
	h.wait("filtered settings action", 5*time.Second, func(frame string) bool {
		return strings.Contains(frame, "Commands") && strings.Contains(frame, "settings") && strings.Contains(frame, "Settings")
	})
	h.clickLabel("Settings")
	h.wait("settings opened by actual mouse", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "Connections and preferences") })
	h.send("\x1b")
	h.wait("same unsent draft after settings", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Connections and preferences") && strings.Contains(frame, draft)
	})
	h.assertComposer()
}
