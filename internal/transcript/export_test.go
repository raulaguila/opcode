package transcript

import (
	"encoding/json"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestMarkdownExportsVisibleContentWithRedactionAndSafeFences(t *testing.T) {
	msgs := []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "question secret"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "answer"}, message.ReasoningContent{Thinking: "hidden-reasoning"}, message.ToolCall{Name: "view", Input: "secret ``` injected"}, message.Finish{ProviderState: &message.ProviderState{Data: json.RawMessage(`{"id":"opaque-state"}`)}}}},
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{Name: "view", Content: "result secret"}}},
	}
	data, err := Markdown(session.Session{ID: "session", Title: "title secret"}, msgs, func(s string) string { return strings.ReplaceAll(s, "secret", "[REDACTED]") })
	require.NoError(t, err)
	output := string(data)
	for _, want := range []string{"question [REDACTED]", "answer", "result [REDACTED]", "````text"} {
		require.Contains(t, output, want)
	}
	for _, unwanted := range []string{"secret", "hidden-reasoning", "opaque-state"} {
		require.NotContains(t, output, unwanted)
	}
}
func TestMarkdownRejectsOversizedExport(t *testing.T) {
	_, err := Markdown(session.Session{}, []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: strings.Repeat("x", 4<<20)}}}}, nil)
	require.Error(t, err)
}
