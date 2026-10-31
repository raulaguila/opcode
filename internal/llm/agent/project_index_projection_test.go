package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestAutomaticIndexProjectionOnlyNamesCurrentTurnNativeReads(t *testing.T) {
	_, _, _, root := runtimeAgent(t, workflow.Ask)
	require.NoError(t, os.WriteFile(filepath.Join(root, "read.py"), []byte("def process():\n    return 'ACTUAL_DELIVERED_BODY'\n"), 0600))
	native, err := tools.NewViewTool(nil).Run(context.Background(), tools.ToolCall{ID: "view", Input: `{"file_path":"read.py"}`})
	require.NoError(t, err)
	require.False(t, native.IsError)
	require.NoError(t, os.WriteFile(filepath.Join(root, "old.py"), []byte("def old():\n    return 1\n"), 0600))
	old, err := tools.NewViewTool(nil).Run(context.Background(), tools.ToolCall{ID: "old-read", Input: `{"file_path":"old.py"}`})
	require.NoError(t, err)
	require.False(t, old.IsError)
	receipt, ok := tools.VisibleSourceReadReceipt("view", native.Content, native.Metadata)
	require.True(t, ok)
	index := `{"navigation_only":true,"results":[{"path":"read.py","symbol":"process","kind":"function","file_hash":"` + receipt.Hash + `","start_line":1,"end_line":2,"preview":"READ_PREVIEW_NOT_EVIDENCE","signature":"SIGNATURE_NOT_EVIDENCE"},{"path":"unread_decoy.py","symbol":"CONTRACT_DECOY","preview":"UNREAD_BODY"},{"path":"old.py","symbol":"OLD_TURN_ONLY"}]}`
	manual := `{"results":[{"path":"manual_candidate.py","preview":"MANUAL_NAVIGATION"}],"navigation_only":true}`
	view := message.ToolResult{ToolCallID: "view", Name: "view", Content: native.Content, Metadata: native.Metadata}
	history := []message.Message{
		{ID: "previous", SessionID: "s", Role: message.User},
		{SessionID: "s", Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "old-read", Name: "view", Input: `{"file_path":"old.py"}`}}},
		{SessionID: "s", Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "old-read", Name: "view", Content: old.Content, Metadata: old.Metadata}}},
		{ID: "current", SessionID: "s", Role: message.User},
		{SessionID: "s", Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "automatic", Name: "search_code_index", Origin: "context"}, message.ToolCall{ID: "manual", Name: "search_code_index"}}},
		{SessionID: "s", Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "automatic", Name: "search_code_index", Content: index}, message.ToolResult{ToolCallID: "manual", Name: "search_code_index", Content: manual}}},
		{SessionID: "s", Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "view", Name: "view", Origin: "context", Input: `{"file_path":"read.py"}`}}},
		{SessionID: "s", Role: message.Tool, Parts: []message.ContentPart{view}},
	}
	before, err := json.Marshal(history)
	require.NoError(t, err)
	projected := projectPreparedIndexes(history, &generationRun{turnMessageID: "current"})
	automatic := projected[5].ToolResults()[0]
	require.Contains(t, automatic.Content, `"path":"read.py"`)
	require.Contains(t, automatic.Content, `"symbol":"process"`)
	require.Contains(t, automatic.Content, `"inspected_reference":"read.py:1-2"`)
	require.Contains(t, automatic.Content, `"navigation_only":true`)
	require.Contains(t, automatic.Content, `"automatic_preparation_summary":true`)
	for _, hidden := range []string{"unread_decoy.py", "UNREAD_BODY", "CONTRACT_DECOY", "old.py", "OLD_TURN_ONLY", "READ_PREVIEW_NOT_EVIDENCE", "SIGNATURE_NOT_EVIDENCE", "start_line", "end_line"} {
		require.NotContains(t, automatic.Content, hidden)
	}
	require.Equal(t, manual, projected[5].ToolResults()[1].Content, "manual searches must remain available to navigate")
	require.Equal(t, view, projected[7].ToolResults()[0], "native content and receipt bytes must remain intact")
	after, err := json.Marshal(history)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after), "provider projection never mutates stored history")
}

func TestAutomaticIndexProjectionOmitsMalformedPagesAndPreservesErrors(t *testing.T) {
	for _, content := range []string{`{"results":[{"path":"UNREAD_TRUNCATED"}`, strings.Repeat("UNREAD_OVERSIZED", 600)} {
		summary := preparedIndexSummary(content, preparedSourceReads{})
		require.Contains(t, summary, `"navigation_details_omitted":true`)
		require.Contains(t, summary, `"results":[]`)
		require.NotContains(t, summary, "UNREAD")
	}
	failure := message.ToolResult{ToolCallID: "automatic", Name: "search_code_index", Content: "index unavailable", IsError: true}
	msg := message.Message{Role: message.Tool, Parts: []message.ContentPart{failure}}
	require.Equal(t, msg, projectPreparedIndexMessage(msg, map[string]bool{"automatic": true}, nil))
}

func TestInspectedIndexReferenceRequiresCompleteSameHashNativeRead(t *testing.T) {
	runtimeAgent(t, workflow.Ask)
	hit := preparedIndexCandidate{Path: "source.py", Symbol: "process", Kind: "function", First: 3, Last: 7, Hash: strings.Repeat("a", 64)}
	receipt := tools.SourceReadReceipt{Path: hit.Path, Hash: hit.Hash, StartLine: 1, EndLine: 8}
	require.Equal(t, "source.py:3-7", inspectedIndexReference(hit, []tools.SourceReadReceipt{receipt}))
	for _, tc := range []struct {
		name    string
		hit     preparedIndexCandidate
		receipt tools.SourceReadReceipt
	}{
		{"incomplete", hit, tools.SourceReadReceipt{Path: hit.Path, Hash: hit.Hash, StartLine: 3, EndLine: 6}},
		{"different path", hit, tools.SourceReadReceipt{Path: "other.py", Hash: hit.Hash, StartLine: 1, EndLine: 8}},
		{"hash mismatch", hit, tools.SourceReadReceipt{Path: hit.Path, Hash: strings.Repeat("b", 64), StartLine: 1, EndLine: 8}},
		{"text window", preparedIndexCandidate{Path: hit.Path, Symbol: "<file>", Kind: "text", First: 3, Last: 7, Hash: hit.Hash}, receipt},
		{"unknown kind", preparedIndexCandidate{Path: hit.Path, Symbol: hit.Symbol, First: 3, Last: 7, Hash: hit.Hash}, receipt},
		{"invalid range", preparedIndexCandidate{Path: hit.Path, Symbol: hit.Symbol, Kind: "function", First: 7, Last: 3, Hash: hit.Hash}, receipt},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Empty(t, inspectedIndexReference(tc.hit, []tools.SourceReadReceipt{tc.receipt}))
		})
	}
}
