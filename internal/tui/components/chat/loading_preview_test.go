package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/stretchr/testify/require"
)

func previewEvent(text string) pubsub.Event[message.Message] {
	return pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: message.Message{
		ID: "live", SessionID: "A", Role: message.Assistant,
		Parts: []message.ContentPart{message.TextContent{Text: text}},
	}}
}

func loadingPreviewFixture(t *testing.T) *messagesCmp {
	t.Helper()
	history := &renderHistory{histories: map[string][]message.Message{"A": benchmarkSessionMessages(2)}}
	m := NewMessagesCmp(&app.App{Messages: history}).(*messagesCmp)
	m.SetSize(80, 20)
	m.SetSession(session.Session{ID: "A"})
	t.Cleanup(m.resetMessageReconciliation)
	return m
}

func TestSustainedInitialStreamCompletesHistoryWithOrderedJournal(t *testing.T) {
	history := &renderHistory{histories: map[string][]message.Message{"A": benchmarkSessionMessages(2)}}
	m := NewMessagesCmp(&app.App{Messages: history}).(*messagesCmp)
	m.SetSize(80, 20)
	t.Cleanup(m.resetMessageReconciliation)
	snapshot := m.SetSession(session.Session{ID: "A"})().(messageReloadMsg)
	var text string
	for index := range 12 {
		text = fmt.Sprintf("Visible stream chunk %d.", index)
		event := previewEvent(text)
		if index == 0 {
			event.Type = pubsub.CreatedEvent
		}
		m.Update(event)
		require.True(t, m.rendering)
		require.Contains(t, ansi.Strip(m.View()), text)
		require.Empty(t, m.messages, "preview remains presentation-only until a successful snapshot")
		require.Empty(t, m.LatestResponse())
	}
	runSQLiteHistoryCommand(m, m.acceptMessageReload(snapshot))
	require.False(t, m.rendering, "a complete event journal must not wait for a quiet stream")
	require.False(t, m.reconcile.needed)
	require.Equal(t, 1, history.calls, "sustained ordered events do not trigger another full read")
	require.Len(t, m.messages, 3)
	require.Equal(t, text, m.messages[2].Content().Text)
	require.Equal(t, text, m.LatestResponse())
	require.Empty(t, m.loadingPreview)
}

func TestVersionChangedDuringInitialTimeoutReportsWithoutRetrySpin(t *testing.T) {
	history := &canceledHistory{err: context.DeadlineExceeded}
	m := NewMessagesCmp(&app.App{Messages: history}).(*messagesCmp)
	m.SetSize(80, 20)
	result := m.SetSession(session.Session{ID: "A"})().(messageReloadMsg)
	m.Update(previewEvent("Live text before timeout"))
	generation := m.reconcile.generation
	cmd := m.acceptMessageReload(result)
	require.NotNil(t, cmd)
	info, ok := cmd().(util.InfoMsg)
	require.True(t, ok, "a failed read must report an error rather than start another immediate read")
	require.Equal(t, util.InfoTypeError, info.Type)
	require.Equal(t, context.DeadlineExceeded.Error(), info.Msg)
	require.Equal(t, generation, m.reconcile.generation)
	require.False(t, m.reconcile.pending)
	require.False(t, m.rendering)
	require.True(t, m.reconcile.needed)
	require.Empty(t, m.loadingPreview)
	require.Empty(t, m.messages)
}

func TestLoadingPreviewClearsOnDeletionGapSwitchAndSessionClear(t *testing.T) {
	for _, change := range []struct {
		name  string
		apply func(*messagesCmp)
	}{
		{"deletion", func(m *messagesCmp) { event := previewEvent(""); event.Type = pubsub.DeletedEvent; m.Update(event) }},
		{"gap", func(m *messagesCmp) {
			event := previewEvent("untrusted after gap")
			event.ReconcileRequired = true
			m.Update(event)
		}},
		{"switch", func(m *messagesCmp) { m.SetSession(session.Session{ID: "B"}) }},
		{"clear", func(m *messagesCmp) { m.Update(SessionClearedMsg{}) }},
	} {
		t.Run(change.name, func(t *testing.T) {
			m := loadingPreviewFixture(t)
			m.Update(previewEvent("temporary live preview"))
			require.NotEmpty(t, m.loadingPreview.text)
			change.apply(m)
			require.Empty(t, m.loadingPreview)
			require.NotContains(t, m.View(), "temporary live preview")
		})
	}
}

func TestAuthoritativeLoadReplacesPreviewWithoutGrantingPreviewActions(t *testing.T) {
	m := loadingPreviewFixture(t)
	m.Update(previewEvent("Temporary assistant text"))
	require.Empty(t, m.LatestResponse())
	require.Empty(t, m.SelectedText())
	require.Empty(t, m.uiMessages)
	require.NotContains(t, m.View(), "Ctrl+Y")
	m.acceptMessageReload(messageReloadMsg{
		sessionID: "A", generation: m.reconcile.generation, version: m.reconcile.version,
		messages: benchmarkSessionMessages(2),
	})
	require.False(t, m.rendering)
	require.Empty(t, m.loadingPreview)
	require.Len(t, m.messages, 2)
	require.NotContains(t, m.View(), "Temporary assistant text")
	require.NotEmpty(t, m.LatestResponse())
}

func TestLoadingPreviewStoresOnlyBoundedVisibleAssistantText(t *testing.T) {
	m := loadingPreviewFixture(t)
	event := previewEvent("Visible assistant text")
	event.Payload.Parts = append(event.Payload.Parts,
		message.ReasoningContent{Thinking: "HIDDEN_REASONING"},
		message.BinaryContent{Data: []byte("PRIVATE_BINARY")},
		message.ToolResult{Content: "PRIVATE_TOOL_RESULT"},
		message.Finish{ProviderState: &message.ProviderState{Data: json.RawMessage(`{"private":"PROVIDER_STATE"}`)}},
	)
	m.Update(event)
	require.Equal(t, loadingPreview{id: "live", text: "Visible assistant text"}, m.loadingPreview)
	require.Empty(t, m.messages)
	for _, hidden := range []string{"HIDDEN_REASONING", "PRIVATE_BINARY", "PRIVATE_TOOL_RESULT", "PROVIDER_STATE"} {
		require.NotContains(t, m.View(), hidden)
	}
	for _, long := range []string{
		strings.Repeat("漢字", loadingPreviewBytes) + "LATEST_VISIBLE_TEXT",
		strings.Repeat("漢字\n", loadingPreviewBytes) + "LATEST_VISIBLE_TEXT",
	} {
		m.Update(previewEvent(long))
		require.LessOrEqual(t, len(m.loadingPreview.text), loadingPreviewBytes)
		require.LessOrEqual(t, strings.Count(m.loadingPreview.text, "\n"), loadingPreviewLines-1)
		require.True(t, utf8.ValidString(m.loadingPreview.text))
		visibleCells := strings.NewReplacer(" ", "", "\n", "", "┃", "").Replace(ansi.Strip(m.View()))
		require.Contains(t, visibleCells, "LATEST_VISIBLE_TEXT")
	}
}

func TestLoadingPreviewIgnoresApplicationProcessAndOtherSessions(t *testing.T) {
	for _, item := range []message.Message{
		{ID: "application", SessionID: "A", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "APPLICATION", Origin: "application"}}},
		{ID: "process", SessionID: "A", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "HIDDEN_PROCESS"}, message.ToolCall{ID: "call", Name: "bash"}}},
		{ID: "other", SessionID: "B", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "OTHER_SESSION"}}},
	} {
		m := loadingPreviewFixture(t)
		m.Update(pubsub.Event[message.Message]{Type: pubsub.CreatedEvent, Payload: item})
		require.Empty(t, m.loadingPreview)
		require.Empty(t, m.messages)
	}
}

func TestLoadingPreviewUsesCredentialRedactionAndStripsTerminalControls(t *testing.T) {
	t.Setenv("OPCODE_PREVIEW_SECRET_TOKEN", "preview-secret-token-value")
	m := loadingPreviewFixture(t)
	m.Update(previewEvent("Visible \x1b[31mtext\x1b[0m\x07 preview-secret-token-value"))
	require.NotContains(t, m.loadingPreview.text, "preview-secret-token-value")
	require.NotContains(t, m.loadingPreview.text, "\x1b")
	require.NotContains(t, m.loadingPreview.text, "\x07")
	require.Contains(t, m.loadingPreview.text, "Visible text")
	require.NotContains(t, m.View(), "preview-secret-token-value")
}

func TestOtherSessionStreamingDoesNotInvalidateSelectedInitialHistory(t *testing.T) {
	history := &renderHistory{histories: map[string][]message.Message{"A": benchmarkSessionMessages(4)}}
	m := NewMessagesCmp(&app.App{Messages: history}).(*messagesCmp)
	m.SetSize(80, 20)
	snapshot := m.SetSession(session.Session{ID: "A"})().(messageReloadMsg)
	for index := range 12 {
		event := previewEvent(fmt.Sprintf("OTHER_SESSION_CHUNK_%d", index))
		event.Payload.SessionID = "B"
		m.Update(event)
		require.Equal(t, snapshot.version, m.reconcile.version)
		require.Empty(t, m.loadingPreview)
	}
	require.Nil(t, m.acceptMessageReload(snapshot))
	require.False(t, m.rendering)
	require.Equal(t, history.histories["A"], m.messages)
	require.Equal(t, "3", m.currentMsgID)
	require.NotContains(t, m.View(), "OTHER_SESSION")
}

func TestOtherSessionGapStillInvalidatesSelectedInitialHistory(t *testing.T) {
	history := &renderHistory{histories: map[string][]message.Message{"A": benchmarkSessionMessages(2)}}
	m := NewMessagesCmp(&app.App{Messages: history}).(*messagesCmp)
	m.SetSize(80, 20)
	snapshot := m.SetSession(session.Session{ID: "A"})().(messageReloadMsg)
	event := previewEvent("OTHER_SESSION_GAP")
	event.Payload.SessionID = "B"
	event.ReconcileRequired = true
	m.Update(event)
	retry := m.acceptMessageReload(snapshot)
	require.NotNil(t, retry)
	require.Greater(t, m.reconcile.version, snapshot.version)
	require.True(t, m.rendering)
	require.Empty(t, m.messages)
	m.acceptMessageReload(retry().(messageReloadMsg))
	require.False(t, m.rendering)
	require.Equal(t, history.histories["A"], m.messages)
}
