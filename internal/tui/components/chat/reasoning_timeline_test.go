package chat

import (
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

func TestReasoningDetailsPrecedeMatchingPublicResponseWithoutTrailingProcess(t *testing.T) {
	m := renderFixture(0)
	m.messages = []message.Message{{ID: "final", SessionID: m.session.ID, Role: message.Assistant, Parts: []message.ContentPart{message.ReasoningContent{Thinking: "Internal analysis"}, message.TextContent{Text: "Public answer README.md:1-2"}, message.Finish{Reason: message.FinishReasonEndTurn}}}}
	m.renderView()
	view := timelineText(m)
	label := config.Text("Raciocínio do modelo", "Model reasoning")
	require.Contains(t, view, label)
	require.Less(t, strings.Index(view, label), strings.Index(view, "Public answer"))
	require.NotContains(t, view[strings.Index(view, "Public answer"):], config.Text("Processo", "Process"))
	require.NotContains(t, view, "Internal analysis")
	require.Len(t, m.hits, 3, "one details toggle, response and explicit response actions")
	m.toggleProcess(m.hits[0].id)
	view = timelineText(m)
	require.Contains(t, view, "Internal analysis")
	require.Less(t, strings.Index(view, "Internal analysis"), strings.Index(view, "Public answer"))
	require.Equal(t, "Public answer README.md:1-2", m.LatestResponse())
}

func TestReasoningDetailsDoNotReorderPublicCommentaryAndToolCalls(t *testing.T) {
	m := renderFixture(0)
	m.messages = []message.Message{
		{ID: "narration", Role: message.Assistant, Parts: []message.ContentPart{message.ReasoningContent{Thinking: "Before command"}, message.TextContent{Text: "I will inspect the file"}, message.ToolCall{ID: "read", Name: "example_tool", Input: `{}`, Finished: true}}},
		{ID: "result", Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "read", Content: "read output"}}},
		{ID: "final", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "Final answer"}, message.Finish{Reason: message.FinishReasonEndTurn}}},
	}
	m.renderView()
	view := timelineText(m)
	require.Less(t, strings.Index(view, "I will inspect"), strings.Index(view, config.Text("Processo", "Process")))
	require.Less(t, strings.Index(view, config.Text("Processo", "Process")), strings.Index(view, "Final answer"))
	for _, hit := range m.hits {
		if hit.kind == "process" {
			m.toggleProcess(hit.id)
		}
	}
	require.Equal(t, 1, strings.Count(timelineText(m), "Before command"))
	require.Equal(t, 1, strings.Count(timelineText(m), "I will inspect"))
}

func TestReasoningDetailsFollowSelectedCitationRevision(t *testing.T) {
	m := revisionFixture(message.CitationValid)
	m.messages[1].Parts = append(m.messages[1].Parts, message.ReasoningContent{Thinking: "Original private trace"})
	m.messages[3].Parts = append(m.messages[3].Parts, message.ReasoningContent{Thinking: "Corrected private trace"})
	m.invalidateMessageCache("original")
	m.invalidateMessageCache("correction")
	m.renderView()
	for _, hit := range m.hits {
		if strings.Contains(hit.id, "process:reasoning:correction") {
			m.toggleProcess(hit.id)
			break
		}
	}
	view := timelineText(m)
	require.Contains(t, view, "Corrected private trace")
	require.NotContains(t, view, "Original private trace")
	require.Less(t, strings.Index(view, "Corrected private trace"), strings.Index(view, "CORRECTED_TEXT"))
	require.Equal(t, "CORRECTED_TEXT README.md:1", m.LatestResponse())
}
