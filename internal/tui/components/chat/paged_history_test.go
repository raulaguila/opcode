package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/stretchr/testify/require"
)

func sqliteTranscriptFixture(t *testing.T, count int) *messagesCmp {
	t.Helper()
	_, service := sqliteHistoryFixture(t, count)
	m := NewMessagesCmp(&app.App{Messages: service}).(*messagesCmp)
	m.SetSize(80, 20)
	runSQLiteHistoryCommand(m, m.SetSession(session.Session{ID: "A"}))
	t.Cleanup(m.resetMessageReconciliation)
	return m
}

func TestSQLiteHistoryInitialReadAndPageUpAreBounded(t *testing.T) {
	m := sqliteTranscriptFixture(t, 1000)
	require.False(t, m.rendering)
	require.Len(t, m.messages, transcriptPageMessages)
	require.Equal(t, "000872", m.messages[0].ID)
	require.Equal(t, "000999", m.currentMsgID)
	require.True(t, m.history.more)
	m.viewport.GotoTop()
	_, command := m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	require.NotNil(t, command)
	runSQLiteHistoryCommand(m, command)
	require.Len(t, m.messages, transcriptPageMessages*2)
	require.Equal(t, "000744", m.messages[0].ID)
	require.Equal(t, "000999", m.currentMsgID)
	full, err := m.app.Messages.List(context.Background(), "A")
	require.NoError(t, err)
	require.Len(t, full, 1000)
}

func TestSQLiteSearchLoadsAllHistoricalPagesAsCommands(t *testing.T) {
	m := sqliteTranscriptFixture(t, 1000)
	m.StartSearch("Historical evidence message 0.")
	require.Len(t, m.messages, transcriptPageMessages, "navigation must not query persistence on the UI goroutine")
	require.Empty(t, m.find.matches)
	runSQLiteHistoryCommand(m, PendingHistoryCommand(m))
	require.Len(t, m.messages, 1000)
	require.False(t, m.history.more)
	require.Empty(t, m.history.first)
	require.NotEmpty(t, m.find.matches)
	require.Equal(t, 0, m.find.matches[0].row)
	assertTranscriptReference(t, m)
}

func TestSQLiteSelectionLoadsHistoryBeforeRecordingCoordinates(t *testing.T) {
	m := sqliteTranscriptFixture(t, 200)
	before := m.viewport.View()
	m.selectionMouse(tea.MouseClickMsg{X: 4, Y: 2, Button: tea.MouseLeft})
	require.False(t, m.SelectionDragging())
	runSQLiteHistoryCommand(m, PendingHistoryCommand(m))
	require.Len(t, m.messages, 200)
	require.True(t, m.SelectionDragging())
	require.Equal(t, before, m.viewport.View())
	m.selectionMouse(tea.MouseReleaseMsg{X: 12, Y: 2, Button: tea.MouseLeft})
	require.True(t, m.HasSelection())
	require.NotEmpty(t, m.SelectedText())
}

func TestSQLiteHistoricalPagingDiscoversNestedTasks(t *testing.T) {
	connection, service := sqliteHistoryFixture(t, 200)
	parts := `[{
"type":"tool_call","data":{"id":"task","name":"agent","input":"{}","finished":true}}]`
	_, err := connection.Exec(`UPDATE messages SET parts=? WHERE id='000000'`, parts)
	require.NoError(t, err)
	_, err = connection.Exec(`INSERT INTO sessions(id,title,created_at,updated_at) VALUES('task','Nested',1,1)`)
	require.NoError(t, err)
	_, err = connection.Exec(`INSERT INTO messages(id,session_id,role,parts,created_at,updated_at) VALUES('child','task','assistant',?,1,1)`, `[{
"type":"tool_call","data":{"id":"shell","name":"bash","input":"{}","finished":true}}]`)
	require.NoError(t, err)
	m := NewMessagesCmp(&app.App{Messages: service}).(*messagesCmp)
	m.SetSize(80, 20)
	runSQLiteHistoryCommand(m, m.SetSession(session.Session{ID: "A"}))
	require.Empty(t, m.nested.snapshots)
	m.StartSearch("old history")
	runSQLiteHistoryCommand(m, PendingHistoryCommand(m))
	require.Contains(t, m.nested.snapshots, "task")
	require.Len(t, m.nested.snapshots["task"], 1)
	require.Equal(t, "shell", m.nested.snapshots["task"][0].ToolCalls()[0].ID)
}

func TestSQLitePromptHistoryKeepsLatest200HumanPromptsInOrder(t *testing.T) {
	_, service := sqliteHistoryFixture(t, 1000)
	history, err := readPromptHistory(context.Background(), service, "A")
	require.NoError(t, err)
	require.Len(t, history, 200)
	require.Equal(t, "Historical evidence message 600.", history[0])
	require.Equal(t, "Historical evidence message 998.", history[199])
}

func TestSQLiteNestedHistoryPagesEarlierToolsAndPreservesBudget(t *testing.T) {
	connection, service := sqliteHistoryFixture(t, 300)
	tool := func(id, input string) string {
		raw, err := json.Marshal([]map[string]any{{"type": "tool_call", "data": message.ToolCall{ID: id, Name: "bash", Input: input, Finished: true}}})
		require.NoError(t, err)
		return string(raw)
	}
	_, err := connection.Exec(`UPDATE messages SET parts=? WHERE id='000000'`, tool("first", "{}"))
	require.NoError(t, err)
	_, err = connection.Exec(`UPDATE messages SET parts=? WHERE id='000299'`, tool("last", "{}"))
	require.NoError(t, err)
	history, size, err := readNestedHistory(context.Background(), service, "A")
	require.NoError(t, err)
	require.Positive(t, size)
	require.Len(t, history, 2)
	require.Equal(t, "first", history[0].ToolCalls()[0].ID)
	require.Equal(t, "last", history[1].ToolCalls()[0].ID)
	_, err = connection.Exec(`UPDATE messages SET parts=? WHERE id='000000'`, tool("first", strings.Repeat("x", nestedHistoryLimit)))
	require.NoError(t, err)
	loaded := loadNestedHistory(context.Background(), service, []string{"A"})
	require.Empty(t, loaded, "an oversized task remains excluded as a whole, preserving the original budget policy")
}

func TestHistoryJournalOwnsImmutableEventsAndAppliesDeletion(t *testing.T) {
	history := &renderHistory{histories: map[string][]message.Message{"A": benchmarkSessionMessages(2)}}
	m := NewMessagesCmp(&app.App{Messages: history}).(*messagesCmp)
	m.SetSize(80, 20)
	snapshot := m.SetSession(session.Session{ID: "A"})().(messageReloadMsg)
	event := previewEvent("created evidence")
	event.Type = pubsub.CreatedEvent
	event.Payload.Parts = append(event.Payload.Parts, message.BinaryContent{Data: []byte("private")}, message.Finish{Reason: message.FinishReasonEndTurn, ProviderState: &message.ProviderState{Data: json.RawMessage(`{"private":true}`)}})
	m.Update(event)
	event.Payload.Parts[0] = message.TextContent{Text: "MUTATED_AFTER_PUBLICATION"}
	m.Update(pubsub.Event[message.Message]{Type: pubsub.DeletedEvent, Payload: history.histories["A"][0]})
	runSQLiteHistoryCommand(m, m.acceptMessageReload(snapshot))
	require.Len(t, m.messages, 2)
	require.Equal(t, "1", m.messages[0].ID)
	require.Equal(t, "created evidence", m.messages[1].Content().Text)
	require.Empty(t, m.messages[1].BinaryContent()[0].Data)
	require.Nil(t, m.messages[1].FinishPart().ProviderState)
	require.Equal(t, "0", history.histories["A"][0].ID, "snapshot replay must not mutate a store-owned slice")
	m.Update(pubsub.Event[message.Message]{Type: pubsub.DeletedEvent, Payload: m.messages[1]})
	require.Len(t, m.messages, 1)
}

func TestHistoryJournalOverflowAndGapRetryAuthoritativeRead(t *testing.T) {
	for _, kind := range []string{"events", "bytes", "gap"} {
		t.Run(kind, func(t *testing.T) { testHistoryJournalRetry(t, kind) })
	}
}

func testHistoryJournalRetry(t *testing.T, kind string) {
	history := &renderHistory{histories: map[string][]message.Message{"A": benchmarkSessionMessages(2)}}
	m := NewMessagesCmp(&app.App{Messages: history}).(*messagesCmp)
	m.SetSize(80, 20)
	snapshot := m.SetSession(session.Session{ID: "A"})().(messageReloadMsg)
	switch kind {
	case "events":
		for i := range messageJournalEvents + 1 {
			m.Update(previewEvent(fmt.Sprint(i)))
		}
	case "bytes":
		m.Update(previewEvent(strings.Repeat("x", messageJournalBytes+1)))
	case "gap":
		event := previewEvent("gap")
		event.ReconcileRequired = true
		m.Update(event)
	}
	require.True(t, m.reconcile.gap)
	require.Empty(t, m.reconcile.journal)
	retry := m.acceptMessageReload(snapshot)
	require.NotNil(t, retry)
	require.True(t, m.rendering)
	runSQLiteHistoryCommand(m, retry)
	require.False(t, m.rendering)
	require.Equal(t, history.histories["A"], m.messages)
	require.Equal(t, 2, history.calls)
}
