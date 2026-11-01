package chat

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/stretchr/testify/require"
)

func liveReasoningFixture() *messagesCmp {
	m := renderFixture(0)
	m.app.CoderAgent = revisionTestAgent{busy: true}
	m.messages = []message.Message{
		{ID: "u", SessionID: m.session.ID, Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Explain project"}}},
		{ID: "a", SessionID: m.session.ID, Role: message.Assistant, Parts: []message.ContentPart{message.ReasoningContent{Thinking: "PROVIDER_REASONING"}}},
	}
	m.renderView()
	m.viewport.GotoBottom()
	return m
}

func reasoningHit(t *testing.T, m *messagesCmp) transcriptHit {
	t.Helper()
	for _, hit := range m.hits {
		if strings.Contains(hit.id, "process:reasoning:") {
			return hit
		}
	}
	t.Fatal("missing model reasoning block")
	return transcriptHit{}
}

func updateReasoning(m *messagesCmp, item message.Message) {
	m.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: item})
}

func TestLiveReasoningFoldsAtFirstPublicTextBeforeCitationValidation(t *testing.T) {
	m := liveReasoningFixture()
	require.Contains(t, timelineText(m), "PROVIDER_REASONING")
	key := reasoningHit(t, m).id
	item := m.messages[1]
	item.AppendContent(" \n")
	updateReasoning(m, item)
	require.True(t, reasoningHit(t, m).expanded, "whitespace is not a public answer")
	item.AppendContent("Public answer README.md:1")
	updateReasoning(m, item)
	require.Equal(t, key, reasoningHit(t, m).id, "text deltas must not replace the reasoning block")
	require.NotContains(t, timelineText(m), "PROVIDER_REASONING", "the first public text folds automatic reasoning while generation is active")
	item.Parts = append(item.Parts, message.Finish{Reason: message.FinishReasonEndTurn})
	item.SetCitationValidation(message.CitationPending)
	updateReasoning(m, item)
	require.NotContains(t, timelineText(m), "PROVIDER_REASONING", "citation validation must not reopen reasoning")
	item.SetCitationValidation(message.CitationValid)
	updateReasoning(m, item)
	m.app.CoderAgent = revisionTestAgent{}
	m.Update(tea.FocusMsg{})
	require.NotContains(t, timelineText(m), "PROVIDER_REASONING")
	require.False(t, reasoningHit(t, m).expanded)
	require.Equal(t, " \nPublic answer README.md:1", m.LatestResponse())
	require.Equal(t, "PROVIDER_REASONING", m.messages[1].ReasoningContent().Thinking, "folding must not erase provider data")
	m.toggleProcess(key)
	require.Contains(t, timelineText(m), "PROVIDER_REASONING", "completed reasoning remains available on demand")
}

func TestManualReasoningChoiceSurvivesStreamingAndCompletion(t *testing.T) {
	for _, keepOpen := range []bool{false, true} {
		m := liveReasoningFixture()
		key := reasoningHit(t, m).id
		m.toggleProcess(key) // Explicitly close the automatic expansion.
		if keepOpen {
			m.toggleProcess(key)
		}
		item := m.messages[1]
		item.AppendContent("Public answer")
		updateReasoning(m, item)
		require.Equal(t, keepOpen, reasoningHit(t, m).expanded)
		item.Parts = append(item.Parts, message.Finish{Reason: message.FinishReasonEndTurn})
		updateReasoning(m, item)
		m.app.CoderAgent = revisionTestAgent{}
		m.Update(tea.FocusMsg{})
		require.Equal(t, keepOpen, reasoningHit(t, m).expanded)
		require.Equal(t, "Public answer", m.LatestResponse())
	}
}

func TestReadingReasoningIsNotCollapsedWhenTurnCompletes(t *testing.T) {
	m := liveReasoningFixture()
	m.SetSize(80, 10)
	item := m.messages[1]
	item.AppendReasoningContent(strings.Repeat("\nProvider reasoning line", 60))
	updateReasoning(m, item)
	m.viewport.GotoBottom()
	m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	require.False(t, m.viewport.AtBottom())
	offset := m.viewport.YOffset
	item.AppendContent("Public answer")
	updateReasoning(m, item)
	require.True(t, reasoningHit(t, m).expanded, "the first public text cannot interrupt an active reader")
	require.Equal(t, offset, m.viewport.YOffset)
	item.Parts = append(item.Parts, message.Finish{Reason: message.FinishReasonEndTurn})
	updateReasoning(m, item)
	m.app.CoderAgent = revisionTestAgent{}
	m.Update(tea.FocusMsg{})
	require.True(t, reasoningHit(t, m).expanded)
	require.Equal(t, offset, m.viewport.YOffset, "completion cannot interrupt someone reading earlier output")
}

func TestReasoningAndToolDetailsDoNotDuplicateProviderText(t *testing.T) {
	m := liveReasoningFixture()
	item := m.messages[1]
	item.Parts = append(item.Parts, message.ToolCall{ID: "read", Name: "example_tool", Input: `{}`, Finished: true})
	updateReasoning(m, item)
	for _, hit := range append([]transcriptHit(nil), m.hits...) {
		if hit.kind == "process" && !strings.Contains(hit.id, "process:reasoning:") && !hit.expanded {
			m.toggleProcess(hit.id)
		}
	}
	require.Equal(t, 1, strings.Count(timelineText(m), "PROVIDER_REASONING"))
	require.Contains(t, timelineText(m), "tool call")
}

func TestMissingProviderReasoningDoesNotCreateThinkingText(t *testing.T) {
	m := liveReasoningFixture()
	item := m.messages[1]
	item.Parts = []message.ContentPart{message.TextContent{Text: "Public answer"}}
	updateReasoning(m, item)
	for _, hit := range m.hits {
		require.NotContains(t, hit.id, "process:reasoning:")
	}
	require.Contains(t, timelineText(m), "Public answer")
}

func TestNewModelCallDoesNotReopenEarlierReasoning(t *testing.T) {
	m := liveReasoningFixture()
	first := reasoningHit(t, m).id
	item := message.Message{ID: "next", SessionID: m.session.ID, Role: message.Assistant, Parts: []message.ContentPart{message.ReasoningContent{Thinking: "NEXT_REASONING"}}}
	m.Update(pubsub.Event[message.Message]{Type: pubsub.CreatedEvent, Payload: item})
	require.NotContains(t, timelineText(m), "PROVIDER_REASONING")
	require.Contains(t, timelineText(m), "NEXT_REASONING")
	item.AppendContent("Public answer")
	updateReasoning(m, item)
	require.NotContains(t, timelineText(m), "NEXT_REASONING")
	m.toggleProcess(first)
	require.Contains(t, timelineText(m), "PROVIDER_REASONING", "older provider reasoning remains available")
}
