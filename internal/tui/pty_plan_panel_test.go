//go:build integration

package tui

import (
	"encoding/json"
	"fmt"
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

func TestPTYPlanPanelSharedStyleAndResizePreserveDraft(t *testing.T) {
	if os.Getenv("OPCODE_LIVE_TESTS") != "1" || runtime.GOOS != "linux" {
		t.Skip("explicit Linux PTY acceptance opt-in required")
	}
	binary := os.Getenv("OPCODE_EVAL_BINARY")
	require.True(t, filepath.IsAbs(binary))
	for _, variant := range []struct {
		name, bg string
		noColor  bool
	}{{"dark", "15;0", false}, {"light", "0;15", false}, {"no-color", "0;15", true}} {
		t.Run(variant.name, func(t *testing.T) {
			var generations atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { servePlanPanelPTY(t, &generations, w, r) }))
			defer server.Close()
			home, workspace, _ := conversationWorkspace(t, server.URL)
			env := []string{"COLORTERM=truecolor", "COLORFGBG=" + variant.bg}
			if variant.noColor {
				env = append(env, "NO_COLOR=1")
			}
			h := newPTYUIHarnessAtSize(t, binary, home, workspace, 120, 40, env...)
			h.wait("initialization", 20*time.Second, func(frame string) bool { return strings.Contains(frame, "Initialize Project") })
			h.send("n")
			h.wait("ready", 5*time.Second, func(frame string) bool {
				return !strings.Contains(frame, "Initialize Project") && ptyUIHasFooter(frame)
			})
			h.paste("/mode plan")
			h.send("\r")
			h.wait("plan mode", 5*time.Second, func(frame string) bool {
				return strings.HasPrefix(strings.TrimSpace(ptyUIFooter(frame)), "Plan") && ptyUIComposerEmpty(frame)
			})
			h.paste("/plan")
			h.send("\r")
			h.wait("empty styled plan", 5*time.Second, func(frame string) bool {
				return strings.Contains(frame, "Describe the task to organize") && ptyUIComposerEmpty(frame)
			})
			h.paste("Use update_plan to save the fixture plan and finish_task as planned without modifying files.")
			h.send("\r")
			h.wait("saved styled plan", 20*time.Second, func(frame string) bool {
				return strings.Contains(frame, "PLAN_PANEL_READY") && strings.Contains(frame, "Plan 1/14") && strings.Contains(frame, "→ 11. Step 11") && ptyUIIdleFooter(frame)
			})
			require.EqualValues(t, 2, generations.Load(), "presentation never triggers repair or inference")
			const draft = "KEEP_PLAN_DRAFT"
			h.paste(draft)
			for _, size := range [][2]int{{60, 20}, {120, 40}} {
				h.resize(size[0], size[1])
				h.wait("plan and draft after resize", 5*time.Second, func(frame string) bool {
					return strings.Contains(frame, draft) && strings.Contains(frame, "→ 11. Step 11") && strings.Contains(frame, "Plan 1/14")
				})
				frame := h.frame()
				require.Contains(t, frame, "┌")
				require.Contains(t, frame, "Alt+↑/↓ scroll")
				require.NotContains(t, frame, "Session:")
				h.assertComposer()
			}
			h.send("\x1b[1;3A")
			h.wait("plan scrolls to earlier step", 5*time.Second, func(frame string) bool { return strings.Contains(frame, "○ 8. Step 8") })
			require.Contains(t, h.frame(), draft)
			require.True(t, strings.HasPrefix(strings.TrimSpace(ptyUIFooter(h.frame())), "Plan"), "viewing the plan never changes mode")
			conversationQuit(t, h, false)
			readme, err := os.ReadFile(filepath.Join(workspace, "README.md"))
			require.NoError(t, err)
			require.Equal(t, "# SensorStore\nO projeto coleta sensores em SQLite.\n", string(readme))
		})
	}
}

func servePlanPanelPTY(t *testing.T, generations *atomic.Int32, w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/chat" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	var request struct{ Stream bool }
	if json.NewDecoder(r.Body).Decode(&request) != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	item := map[string]any{"role": "assistant", "content": "PLAN_PANEL_READY"}
	if request.Stream {
		switch generations.Add(1) {
		case 1:
			steps := make([]string, 14)
			for i := range steps {
				steps[i] = fmt.Sprintf("pending|Step %d", i+1)
			}
			steps[0], steps[10] = "completed|Step 1", "in_progress|Step 11"
			item["content"] = ""
			item["tool_calls"] = []any{
				map[string]any{"function": map[string]any{"name": "update_plan", "arguments": map[string]any{"objective": "Plan panel fixture", "steps_text": strings.Join(steps, "\n"), "criteria_text": "Review the plan", "checks_text": ""}}},
				map[string]any{"function": map[string]any{"name": "finish_task", "arguments": map[string]any{"status": "planned", "summary": "Plan ready for review"}}},
			}
		case 2:
		default:
			t.Error("unexpected model generation or repair")
		}
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	_ = json.NewEncoder(w).Encode(map[string]any{"message": item, "done": true, "done_reason": "stop", "eval_count": 12, "eval_duration": 1000000000})
}
