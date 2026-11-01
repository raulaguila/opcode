package chat

import (
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

func TestRepeatedFailedCallsFoldIntoOneRowWithReasoning(t *testing.T) {
	m := renderFixture(0)
	for _, id := range []string{"a", "b", "c"} {
		m.messages = append(m.messages,
			message.Message{ID: "edit-" + id, Role: message.Assistant, Parts: []message.ContentPart{message.ReasoningContent{Thinking: "retry " + id}, message.ToolCall{ID: "e" + id, Name: "edit", Input: `{"file_path":"/w/store.go"}`, Finished: true}}},
			message.Message{ID: "res-" + id, Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "e" + id, Content: "old_string is empty", IsError: true}}},
		)
	}
	m.renderView()
	view := timelineText(m)
	require.Contains(t, view, "✗ edit store.go ×3")
	require.Equal(t, 0, strings.Count(view, "Model reasoning"), "reasoning behind tool calls stays inside the process row")
	require.NotContains(t, view, "old_string is empty", "the folded row never quotes tool output")
}
