package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/stretchr/testify/require"
)

type canceledHistory struct {
	message.Service
	ctx context.Context
	err error
}

func (h *canceledHistory) List(ctx context.Context, _ string) ([]message.Message, error) {
	h.ctx = ctx
	if h.err != nil {
		return nil, h.err
	}
	return nil, ctx.Err()
}

func TestSessionLoadRunsInCommandAndCancelsPreviousGeneration(t *testing.T) {
	history := &canceledHistory{}
	m := NewMessagesCmp(&app.App{Messages: history}).(*messagesCmp)
	m.SetSize(80, 20)
	first := m.SetSession(session.Session{ID: "A"})
	require.NotNil(t, first)
	require.Nil(t, history.ctx, "SetSession must not query persistence on the event loop")
	require.True(t, m.rendering)
	second := m.SetSession(session.Session{ID: "B"})
	stale := first().(messageReloadMsg)
	require.ErrorIs(t, history.ctx.Err(), context.Canceled)
	require.Nil(t, m.acceptMessageReload(stale))
	require.True(t, m.rendering)
	m.acceptMessageReload(second().(messageReloadMsg))
	require.False(t, m.rendering)
	require.Equal(t, "B", m.session.ID)
}

func TestInitialSessionFailureClearsLoadingAndReportsError(t *testing.T) {
	for _, err := range []error{errors.New("history unavailable"), context.DeadlineExceeded} {
		history := &canceledHistory{err: err}
		m := NewMessagesCmp(&app.App{Messages: history}).(*messagesCmp)
		result := m.SetSession(session.Session{ID: "A"})().(messageReloadMsg)
		require.ErrorIs(t, result.err, err)
		require.NotNil(t, m.acceptMessageReload(result))
		require.False(t, m.rendering)
		require.False(t, m.reconcile.pending)
		require.True(t, m.reconcile.needed, "a later event may retry the failed read")
	}
}

func TestSessionLoadRejectsSnapshotOlderThanStreamingArrival(t *testing.T) {
	fixture := renderFixture(2)
	history := &renderHistory{histories: map[string][]message.Message{"A": fixture.messages}}
	m := NewMessagesCmp(&app.App{Messages: history}).(*messagesCmp)
	m.SetSize(80, 20)
	old := m.SetSession(session.Session{ID: "A"})().(messageReloadMsg)
	newest := fixture.messages[1]
	newest.AppendContent(" evidence received during the query")
	history.histories["A"] = []message.Message{fixture.messages[0], newest}
	m.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: newest})
	retry := m.acceptMessageReload(old)
	require.NotNil(t, retry)
	require.True(t, m.rendering)
	require.True(t, m.history.initial)
	m.acceptMessageReload(retry().(messageReloadMsg))
	require.False(t, m.rendering)
	require.Contains(t, strings.Join(m.transcriptLines, "\n"), "evidence received during the query")
	require.Equal(t, newest.Content().String(), m.LatestResponse())
}

func paginatedFixture(t *testing.T) *messagesCmp {
	t.Helper()
	fixture := renderFixture(200)
	for index := range fixture.messages {
		fixture.messages[index].Parts = []message.ContentPart{message.TextContent{Text: fmt.Sprintf("Historical evidence message %d.", index)}}
	}
	history := &renderHistory{histories: map[string][]message.Message{"A": fixture.messages}}
	m := NewMessagesCmp(&app.App{Messages: history}).(*messagesCmp)
	m.SetSize(80, 20)
	m.acceptMessageReload(m.SetSession(session.Session{ID: "A"})().(messageReloadMsg))
	return m
}

func TestSessionPagesVisualTurnsAndPageUpRevealsCompleteDurableHistory(t *testing.T) {
	m := paginatedFixture(t)
	require.Len(t, m.messages, 200)
	require.Len(t, m.renderCache.blocks, transcriptPageTurns)
	require.Equal(t, "199", m.currentMsgID)
	require.Equal(t, "72", m.history.first)
	m.viewport.GotoTop()
	oldHeight := len(m.transcriptLines)
	m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	require.Empty(t, m.history.first)
	require.Equal(t, max(0, len(m.transcriptLines)-oldHeight-m.viewport.Height), m.viewport.YOffset)
	assertTranscriptReference(t, m)
}

func TestSearchMaterializesHistoricalRowsBeforeFindingOldEvidence(t *testing.T) {
	m := paginatedFixture(t)
	m.StartSearch("Historical evidence message 0.")
	require.Empty(t, m.history.first)
	require.NotEmpty(t, m.find.matches)
	require.Equal(t, 0, m.find.matches[0].row)
	assertTranscriptReference(t, m)
}

func TestSelectionMaterializesHistoryWithoutMovingVisibleText(t *testing.T) {
	m := paginatedFixture(t)
	before := m.viewport.View()
	m.selectionMouse(tea.MouseMsg{X: 4, Y: 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	require.Empty(t, m.history.first)
	require.True(t, m.SelectionDragging())
	require.Equal(t, before, m.viewport.View())
	m.selectionMouse(tea.MouseMsg{X: 12, Y: 2, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	require.True(t, m.HasSelection())
	require.NotEmpty(t, m.SelectedText())
}

func TestNewTurnPreservesPagedHistoryAnchorAndFollowsUserRequest(t *testing.T) {
	m := paginatedFixture(t)
	m.viewport.GotoTop()
	m.Update(pubsub.Event[message.Message]{Type: pubsub.CreatedEvent, Payload: message.Message{
		ID: "200", SessionID: "A", Role: message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "new request"}},
	}})
	require.Equal(t, "72", m.history.first)
	require.True(t, m.viewport.AtBottom())
	require.Len(t, m.renderCache.blocks, transcriptPageTurns+1)
	require.Len(t, m.messages, 201)
}

// The store observes cancellation while the first command is actually running.
type waitingHistory struct {
	message.Service
	started chan struct{}
}

func (h *waitingHistory) List(ctx context.Context, _ string) ([]message.Message, error) {
	h.started <- struct{}{}
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestSessionChangeCancelsRunningHistoryRead(t *testing.T) {
	history := &waitingHistory{started: make(chan struct{}, 1)}
	m := NewMessagesCmp(&app.App{Messages: history}).(*messagesCmp)
	t.Cleanup(m.resetMessageReconciliation)
	cmd := m.SetSession(session.Session{ID: "A"})
	result := make(chan messageReloadMsg, 1)
	go func() { result <- cmd().(messageReloadMsg) }()
	select {
	case <-history.started:
	case <-time.After(time.Second):
		t.Fatal("the history read did not start")
	}
	m.SetSession(session.Session{ID: "B"})
	select {
	case stale := <-result:
		require.ErrorIs(t, stale.err, context.Canceled)
		require.Nil(t, m.acceptMessageReload(stale))
	case <-time.After(time.Second):
		t.Fatal("session change did not cancel the running history read")
	}
	require.True(t, m.rendering)
	require.Empty(t, m.messages)
}

func TestSearchStartedDuringInitialLoadIncludesOldestEvidence(t *testing.T) {
	messages := benchmarkSessionMessages(200)
	messages[0].Parts = []message.ContentPart{message.TextContent{Text: "old unique evidence"}}
	history := &renderHistory{histories: map[string][]message.Message{"A": messages}}
	m := NewMessagesCmp(&app.App{Messages: history}).(*messagesCmp)
	m.SetSize(80, 20)
	cmd := m.SetSession(session.Session{ID: "A"})
	m.StartSearch("old unique evidence")
	m.acceptMessageReload(cmd().(messageReloadMsg))
	require.False(t, m.rendering)
	require.Empty(t, m.history.first)
	require.NotEmpty(t, m.find.matches)
}
