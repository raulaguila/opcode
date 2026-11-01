//go:build integration

package tui

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func assertLiveProjectEditPolicy(t *testing.T, h *ptyUIHarness, conn *sql.DB, workspace string) {
	t.Helper()
	h.resize(180, 40)
	h.clickApproval()
	h.wait("separate approval section", 5*time.Second, func(frame string) bool {
		return strings.Contains(frame, "Approval") && strings.Contains(frame, "Allow project edits")
	})
	h.clickLabel("Allow project edits")
	h.wait("project edit policy explicitly selected", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Work mode") && strings.Contains(frame, "Approval: project edits")
	})
	before := len(ptyUIMessages(conn))
	h.paste(`First call request_tools with {"names":["write"],"replace":true} to load the native file tool. Then call write exactly once with {"file_path":"policy-created.txt","content":"PROJECT_EDIT_OK"}. Do not use bash, commands, patches or change any other file. After write succeeds, reply exactly POLICY_FILE_CREATED.`)
	h.send("\r")
	h.wait("real model creates file with scoped edit consent", 120*time.Second, func(frame string) bool {
		if strings.Contains(frame, "Allow once") {
			h.send("d")
			t.Fatalf("ordinary native project creation unexpectedly requested additional approval:\n%s", frame)
		}
		history := ptyUIMessages(conn)
		return len(history) > before && history[len(history)-1].Role == "assistant" && history[len(history)-1].finished() && ptyUIIdleFooter(frame)
	})
	contents, err := os.ReadFile(filepath.Join(workspace, "policy-created.txt"))
	require.NoError(t, err, h.frame())
	require.Equal(t, "PROJECT_EDIT_OK", string(contents))
	require.Contains(t, h.frame(), "POLICY_FILE_CREATED")
	calls := countLiveSuccessfulWrites(t, ptyUIMessages(conn)[before:])
	require.Equal(t, 1, calls, "policy consent must not replay a write")
	require.NotContains(t, h.frame(), "Citations not validated")
	t.Log("real Nemotron native write allowed only after explicit project-edit policy; next bash command must still ask")
}

func countLiveSuccessfulWrites(t *testing.T, messages []ptyUIMessage) int {
	t.Helper()
	count := 0
	writes := map[string]bool{}
	for _, item := range messages {
		for _, part := range item.Parts {
			if part.Type == "tool_call" {
				var call struct{ ID, Name string }
				require.NoError(t, json.Unmarshal(part.Data, &call))
				writes[call.ID] = call.Name == "write"
			}
			if part.Type != "tool_result" {
				continue
			}
			var result struct {
				ToolCallID string `json:"tool_call_id"`
				IsError    bool   `json:"is_error"`
			}
			require.NoError(t, json.Unmarshal(part.Data, &result))
			require.False(t, result.IsError, "real policy acceptance must not contain failed tools")
			if writes[result.ToolCallID] {
				count++
			}
		}
	}
	return count
}
