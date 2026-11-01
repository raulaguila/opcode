//go:build integration

package tui

import (
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

func TestPTYApprovalPolicyIsExplicitRevocableAndNotPersisted(t *testing.T) {
	if os.Getenv("OPCODE_LIVE_TESTS") != "1" || runtime.GOOS != "linux" {
		t.Skip("explicit Linux PTY acceptance opt-in required")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/chat" {
			t.Error("menu policy selection must not generate a model response")
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	home, workspace, _ := conversationWorkspace(t, server.URL)
	path := filepath.Join(home, ".opcode", "config.json")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte(strings.Replace(string(raw), `"mode":"ask"`, `"mode":"execute"`, 1)), 0600))
	h := newPTYUIHarnessAtSize(t, os.Getenv("OPCODE_EVAL_BINARY"), home, workspace, 180, 36)
	h.wait("initialization", 20*time.Second, func(frame string) bool { return strings.Contains(frame, "Initialize Project") })
	h.send("n")
	h.wait("ready", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Initialize Project") && ptyUIHasFooter(frame)
	})
	h.paste("KEEP_POLICY_DRAFT")
	h.clickApproval()
	h.wait("separate approval section", 5*time.Second, func(frame string) bool {
		return strings.Contains(frame, "Work mode") && strings.Contains(frame, "Approval") && strings.Contains(frame, "✓ Ask for approval")
	})
	h.send("\r")
	h.wait("selected default policy is focused", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Work mode") && strings.Contains(ptyUIFooter(frame), "Approval: ask") && strings.Contains(frame, "KEEP_POLICY_DRAFT")
	})
	h.clickApproval()
	h.send("\x1b[B\r")
	h.wait("explicit edit policy and preserved draft", 5*time.Second, func(frame string) bool {
		return strings.Contains(ptyUIFooter(frame), "Approval: project edits") && strings.Contains(frame, "KEEP_POLICY_DRAFT")
	})
	h.resize(60, 20)
	require.Contains(t, ptyUIFooter(h.frame()), "A:edit")
	h.clickApproval()
	h.wait("compact policy chooser", 5*time.Second, func(frame string) bool {
		return strings.Contains(frame, "✓ Allow project edits") && strings.Contains(frame, "✓ Execute")
	})
	h.send("\x1b[A\r")
	h.wait("explicitly restored prompt policy", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Work mode") && strings.Contains(frame, "KEEP_POLICY_DRAFT")
	})
	require.Contains(t, ptyUIFooter(h.frame()), "A:ask")
	h.resize(80, 24)
	require.Contains(t, ptyUIFooter(h.frame()), "A:ask")
	h.resize(120, 36)
	require.Contains(t, ptyUIFooter(h.frame()), "Appr.: ask")
	h.resize(180, 36)
	h.wait("prompt label restored", 5*time.Second, func(frame string) bool { return !strings.Contains(ptyUIFooter(frame), "Approval: project edits") })
	h.clickApproval()
	h.send("\x1b[B\r")
	h.wait("edits selected again", 5*time.Second, func(frame string) bool { return strings.Contains(ptyUIFooter(frame), "Approval: project edits") })
	h.send("\x0e")
	h.wait("new conversation resets policy", 5*time.Second, func(frame string) bool { return !strings.Contains(ptyUIFooter(frame), "Approval: project edits") })
	conversationQuit(t, h, false)
	h = newPTYUIHarnessAtSize(t, os.Getenv("OPCODE_EVAL_BINARY"), home, workspace, 180, 36)
	h.wait("fresh process defaults to asking", 15*time.Second, func(frame string) bool {
		return ptyUIHasFooter(frame) && !strings.Contains(frame, "Initialize Project")
	})
	require.NotContains(t, ptyUIFooter(h.frame()), "Approval: project edits")
	conversationQuit(t, h, false)
}
