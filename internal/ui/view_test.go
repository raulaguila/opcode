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
	require.Contains(t, view, "› ● view README.md · edit store.go", "a step without a result is still running")
	require.NotContains(t, view, "FIRST_DRAFT", "a revised reply replaces the draft")
	require.Contains(t, view, "REVISED_REPLY")
	m.messages = append(m.messages, message.Message{ID: "r2", Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "c2", Content: "no match", IsError: true}}})
	require.Contains(t, ansi.Strip(m.renderTranscript(100)), "› ✗ view README.md · edit store.go · 1 failure", "failures show and count")
}

func TestEmptyScreenAndComposerFollowTheMode(t *testing.T) {
	m := New(nil)
	m.width, m.height = 80, 24
	m.layout()
	require.Contains(t, ansi.Strip(m.renderTranscript(80)), "?")
	for _, mode := range []string{"ask", "plan", "execute"} {
		m.mode = mode
		top := strings.Split(ansi.Strip(m.composerView()), "\n")[0]
		require.True(t, strings.HasPrefix(top, "╭─ "+modeLabel(mode)), top)
	}
	m.cycleMode()
	require.Equal(t, "ask", m.mode, "Shift+Tab wraps from Execute to Ask")
}
