package chat

import (
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCollapsedProcessReportsResultsAndPreservesPublicReply(t *testing.T) {
	m := renderFixture(0)
	m.messages = []message.Message{
		{ID: "call", Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "read", Name: "view", Finished: true}}},
		{ID: "result", Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "read", Name: "view", Content: "private failure detail", IsError: true, Metadata: `{"truncated":true}`}}},
		{ID: "final", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "Public limited answer"}, message.Finish{Reason: message.FinishReasonEndTurn}}},
	}
	m.renderView()
	view := timelineText(m)
	require.Contains(t, view, config.Text("1 falha(s)", "1 failure(s)"))
	require.Contains(t, view, config.Text("1 saída(s) parcial(is)", "1 partial output(s)"))
	require.Contains(t, view, "Public limited answer")
	require.NotContains(t, view, "private failure detail")
}

func TestProcessOutcomeUsesRecordedFlagsNotProseAndDeduplicatesResults(t *testing.T) {
	result := message.Message{Parts: []message.ContentPart{message.ToolResult{ToolCallID: "a", Content: `{"cancelled":true,"truncated":true}`, Metadata: `{"cancelled":true}`, IsError: true}}}
	outcome := processOutcome([]message.Message{result, result})
	require.Contains(t, outcome, config.Text("interrompido", "interrupted"))
	require.Contains(t, outcome, config.Text("1 falha(s)", "1 failure(s)"))
	require.NotContains(t, outcome, config.Text("parcial", "partial"), "JSON in arbitrary tool prose is not outcome metadata")
	require.Equal(t, config.Text("finalizado", "finished"), processOutcome([]message.Message{{Parts: []message.ContentPart{message.ToolResult{ToolCallID: "b", Content: "Error: cancelled"}}}}))
	require.Equal(t, config.Text("interrompido", "interrupted"), processOutcome([]message.Message{{Parts: []message.ContentPart{message.Finish{Reason: message.FinishReasonCanceled}}}}))
}

func TestCitationStatusFollowsSelectedRevisionWithoutBorrowingAnotherText(t *testing.T) {
	m := revisionFixture(message.CitationValid)
	require.Equal(t, message.CitationValid, m.ResponseCitationStatus("correction"))
	require.Equal(t, message.CitationRevising, m.ResponseCitationStatus("original"))
	m = revisionFixture(message.CitationInvalid)
	require.NotEqual(t, message.CitationValid, m.ResponseCitationStatus(m.LatestResponseID()))
}

func TestCitationStatusUsesMessageIdentityAcrossTurnsAndDuplicateText(t *testing.T) {
	m := renderFixture(0)
	m.messages = []message.Message{
		{ID: "u1", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "First question"}}},
		{ID: "a1", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "Same answer", CitationStatus: message.CitationValid}}},
		{ID: "u2", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Second question"}}},
		{ID: "a2", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "Same answer", CitationStatus: message.CitationInvalid}}},
	}
	require.Equal(t, "a2", m.LatestResponseID())
	require.Equal(t, message.CitationValid, m.ResponseCitationStatus("a1"))
	require.Equal(t, message.CitationInvalid, m.ResponseCitationStatus("a2"))
	require.Empty(t, m.ResponseCitationStatus("Same answer"))
	require.Empty(t, m.ResponseCitationStatus("u1"))
	require.Empty(t, m.ResponseCitationStatus("missing"))
}
