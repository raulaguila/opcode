package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

func TestTranscriptShowsTurnStepsAndOnlyTheLatestReply(t *testing.T) {
	m := New(nil)
	m.width, m.height = 100, 30
	m.messages = []message.Message{
		{ID: "u", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "O que faz?"}}},
		{ID: "a1", Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "c1", Name: "view", Input: `{"file_path":"/w/README.md"}`, Finished: true}}},
		{ID: "r1", Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "c1", Content: "ok"}}},
		{ID: "a2", Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "c2", Name: "edit", Input: `{"file_path":"/w/store.go"}`, Finished: true}}},
		{ID: "a3", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "FIRST_DRAFT"}}},
		{ID: "a4", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "REVISED_REPLY"}}},
	}
	view := ansi.Strip(m.renderTranscript(100))
	require.Contains(t, view, "┃ O que faz?")
	require.Contains(t, view, "› ✓ view README.md\n  › ● edit store.go", "one line per outcome; a step without a result is still running")
	require.NotContains(t, view, "FIRST_DRAFT", "a revised reply replaces the draft")
	require.Contains(t, view, "REVISED_REPLY")
	m.messages = append(m.messages, message.Message{ID: "r2", Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "c2", Content: "no match", IsError: true}}})
	require.Contains(t, ansi.Strip(m.renderTranscript(100)), "› ✗ edit store.go · 1 failure", "failures show and count")
	m.messages = append(m.messages,
		message.Message{ID: "a5", Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "c3", Name: "edit", Input: `{"file_path":"/w/store.go"}`, Finished: true}, message.ToolCall{ID: "c4", Name: "edit", Input: `{"file_path":"/w/store.go"}`, Finished: true}}},
		message.Message{ID: "r3", Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "c3", IsError: true}, message.ToolResult{ToolCallID: "c4", IsError: true}}})
	require.Contains(t, ansi.Strip(m.renderTranscript(100)), "› ✗ edit store.go ×2 · 2 failures", "a repeated target folds into ×N")
}

func TestEmptyScreenAndComposerFollowTheMode(t *testing.T) {
	m := New(nil)
	m.width, m.height = 80, 24
	m.layout()
	require.Contains(t, ansi.Strip(m.renderTranscript(80)), "?")
	for _, mode := range []string{"ask", "plan", "execute"} {
		m.mode = mode
		top := strings.Split(ansi.Strip(m.composerView()), "\n")[0]
		require.True(t, strings.HasPrefix(top, " ▁ "+modeLabel(mode)), top)
	}
	m.cycleMode()
	require.Equal(t, "ask", m.mode, "Shift+Tab wraps from Execute to Ask")
}

func TestDiffPreviewNumbersLinesAndCountsChanges(t *testing.T) {
	diff := "--- a/store.go\n+++ b/store.go\n@@ -44,2 +44,4 @@\n }\n+func Remove() {\n+}\n-old\n"
	rows, added, removed := diffPreview(diff, 40, diffRows)
	require.Equal(t, 2, added)
	require.Equal(t, 1, removed)
	text := ansi.Strip(strings.Join(rows, "\n"))
	require.Contains(t, text, "   44   }")
	require.Contains(t, text, "   45  +func Remove() {")
	require.Contains(t, text, "   45  -old")
}

func TestFooterNamesWhatTheRunIsDoing(t *testing.T) {
	m := New(nil)
	m.messages = []message.Message{
		{ID: "a1", Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "c1", Name: "view", Input: `{"file_path":"/w/README.md"}`, Finished: true}}},
		{ID: "r1", Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "c1", Content: "ok"}}},
		{ID: "a2", Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "c2", Name: "edit", Input: `{"file_path":"/w/main.go"}`, Finished: true}}},
	}
	require.Contains(t, m.doing(), "main.go") // the call without a result is the one running
	require.Equal(t, "+12 −3", sizeLabel(12, 3))
	require.Equal(t, "+12", sizeLabel(12, 0))
	require.Empty(t, sizeLabel(0, 0))
}
