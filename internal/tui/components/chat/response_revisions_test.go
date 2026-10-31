package chat

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

type revisionTestAgent struct {
	benchmarkAgent
	busy bool
}

func (a revisionTestAgent) IsSessionBusy(string) bool { return a.busy }

func revisionFixture(state string) *messagesCmp {
	m := renderFixture(0)
	m.app.CoderAgent = revisionTestAgent{busy: state == message.CitationPending}
	m.messages = []message.Message{
		{ID: "u", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Explain project"}}},
		{ID: "original", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "ORIGINAL_TEXT", CitationStatus: message.CitationRevising}, message.Finish{Reason: message.FinishReasonEndTurn}}},
		{ID: "app", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Private repair", Origin: "application"}}},
		{ID: "correction", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "CORRECTED_TEXT README.md:1", CitationStatus: state, RevisionOf: "original"}, message.Finish{Reason: message.FinishReasonEndTurn}}},
	}
	m.renderView()
	return m
}

func TestLegacySupersededVersionFoldsWithoutInventingALink(t *testing.T) {
	m := revisionFixture(message.CitationValid)
	m.messages[1].SetCitationValidation(message.CitationSuperseded)
	m.messages[3].SetRevisionOf("")
	m.invalidateMessageCache("original")
	m.invalidateMessageCache("correction")
	m.renderView()
	require.NotContains(t, timelineText(m), "ORIGINAL_TEXT")
	require.Contains(t, timelineText(m), "CORRECTED_TEXT")
	m.toggleRevision("original")
	require.Contains(t, timelineText(m), "ORIGINAL_TEXT")
	require.Empty(t, m.messages[3].RevisionOf(), "legacy presentation must not fabricate a relationship")
}

func TestInterruptedRevisionDoesNotShowPermanentReviewingStatus(t *testing.T) {
	m := revisionFixture(message.CitationPending)
	m.app.CoderAgent = revisionTestAgent{}
	m.renderView()
	require.Contains(t, timelineText(m), "ORIGINAL_TEXT")
	require.Contains(t, timelineText(m), "Citations not validated")
	require.NotContains(t, timelineText(m), "Revising references")
}

func TestRevisionPromotesOnlyValidatedCandidateAndKeepsPreviousAvailable(t *testing.T) {
	m := revisionFixture(message.CitationPending)
	require.Contains(t, timelineText(m), "ORIGINAL_TEXT")
	require.NotContains(t, timelineText(m), "CORRECTED_TEXT")
	require.Contains(t, timelineText(m), "Revising references")
	require.Equal(t, "ORIGINAL_TEXT", m.LatestResponse())
	updated := m.messages[3]
	updated.Parts = append([]message.ContentPart{}, updated.Parts...)
	updated.SetCitationValidation(message.CitationValid)
	m.messages[3] = updated
	m.invalidateMessageCache(updated.ID)
	m.renderView()
	require.NotContains(t, timelineText(m), "ORIGINAL_TEXT")
	require.Contains(t, timelineText(m), "CORRECTED_TEXT")
	require.Contains(t, timelineText(m), "View previous version")
	require.NotContains(t, timelineText(m), "1 repair")
	require.NotContains(t, timelineText(m), "Private repair")
	require.Equal(t, "CORRECTED_TEXT README.md:1", m.LatestResponse())
	m.Update(tea.KeyPressMsg{Mod: tea.ModAlt, Code: []rune("v")[0]})
	require.Contains(t, timelineText(m), "ORIGINAL_TEXT")
	require.Contains(t, timelineText(m), "Reason:")
	require.Contains(t, timelineText(m), "Private repair")
	m.Update(tea.KeyPressMsg{Mod: tea.ModAlt, Code: []rune("v")[0]})
	require.NotContains(t, timelineText(m), "ORIGINAL_TEXT")
	m.resetRenderCache()
	m.renderView()
	require.NotContains(t, timelineText(m), "ORIGINAL_TEXT")
	require.Contains(t, timelineText(m), "CORRECTED_TEXT")
}

func TestFailedRevisionRetainsOriginalAndShowsAttemptOnlyOnDemand(t *testing.T) {
	m := revisionFixture(message.CitationInvalid)
	m.messages[1].SetCitationValidation(message.CitationInvalid)
	m.invalidateMessageCache("original")
	m.renderView()
	require.Contains(t, timelineText(m), "ORIGINAL_TEXT")
	require.Contains(t, timelineText(m), "Citations not validated")
	require.NotContains(t, timelineText(m), "CORRECTED_TEXT")
	m.toggleRevision("original")
	require.Contains(t, timelineText(m), "CORRECTED_TEXT")
	require.Equal(t, "ORIGINAL_TEXT", m.LatestResponse())
}

func TestTruncatedRevisionCannotReplaceOriginalDespiteValidCitations(t *testing.T) {
	m := revisionFixture(message.CitationValid)
	m.messages[3].AddFinish(message.FinishReasonMaxTokens)
	m.invalidateMessageCache("correction")
	m.renderView()
	require.Contains(t, timelineText(m), "ORIGINAL_TEXT")
	require.NotContains(t, timelineText(m), "CORRECTED_TEXT")
	require.Equal(t, "ORIGINAL_TEXT", m.LatestResponse())
	m.toggleRevision("original")
	require.Contains(t, timelineText(m), "CORRECTED_TEXT")
}

func TestRevisionNeverMergesOtherHumanTurnsOrToolBearingMessages(t *testing.T) {
	m := revisionFixture(message.CitationValid)
	m.messages[2] = message.Message{ID: "new-user", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Independent request"}}}
	m.resetRenderCache()
	m.renderView()
	require.Contains(t, timelineText(m), "ORIGINAL_TEXT")
	require.Contains(t, timelineText(m), "CORRECTED_TEXT")
	m = revisionFixture(message.CitationValid)
	m.messages[1].AddToolCall(message.ToolCall{ID: "q", Name: "ask_user_question"})
	m.resetRenderCache()
	m.renderView()
	require.Contains(t, timelineText(m), "ORIGINAL_TEXT")
	require.Contains(t, timelineText(m), "CORRECTED_TEXT")
}
