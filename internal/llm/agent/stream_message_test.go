package agent

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/db"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

type streamRecordingStore struct {
	*memoryMessages
	mu           sync.Mutex
	publications []message.Message
	writes       int
	failure      error
}

func (s *streamRecordingStore) PublishUpdate(msg message.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	msg.Parts = append([]message.ContentPart(nil), msg.Parts...)
	s.publications = append(s.publications, msg)
}

func (s *streamRecordingStore) Update(ctx context.Context, msg message.Message) error {
	s.mu.Lock()
	s.writes++
	err := s.failure
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return s.memoryMessages.Update(ctx, msg)
}

func newRecordingStream(t *testing.T) (*streamRecordingStore, message.Message) {
	t.Helper()
	store := &streamRecordingStore{memoryMessages: &memoryMessages{}}
	msg, err := store.Create(context.Background(), "session", message.CreateMessageParams{Role: message.Assistant})
	require.NoError(t, err)
	return store, msg
}

func TestStreamWriterBoundsCheckpointsAndPreservesLiveSnapshots(t *testing.T) {
	store, msg := newRecordingStream(t)
	now := time.Now()
	w := newStreamMessageWriter(store, &msg, now)
	for range 2048 {
		w.append(provider.ProviderEvent{Type: provider.EventContentDelta, Content: strings.Repeat("x", 32)})
		require.NoError(t, w.flush(context.Background(), now, false))
	}
	require.Equal(t, 4, store.writes, "64KiB needs four 16KiB SQL checkpoints")
	require.Len(t, msg.Content().Text, 64<<10)
	require.Greater(t, len(store.publications), store.writes)
	require.Equal(t, strings.Repeat("x", 32), store.publications[0].Content().Text)
	saved, err := store.List(context.Background(), "session")
	require.NoError(t, err)
	require.Equal(t, msg.Content().Text, saved[0].Content().Text)
}

func TestStreamWriterCheckpointsWhileProviderIsIdle(t *testing.T) {
	store, msg := newRecordingStream(t)
	now := time.Now()
	w := newStreamMessageWriter(store, &msg, now)
	w.append(provider.ProviderEvent{Type: provider.EventContentDelta, Content: "partial"})
	require.NoError(t, w.flush(context.Background(), now, false))
	require.Zero(t, store.writes)
	require.NoError(t, w.flush(context.Background(), now.Add(streamCheckpointInterval), false))
	require.Equal(t, 1, store.writes)
	saved, err := store.List(context.Background(), "session")
	require.NoError(t, err)
	require.Equal(t, "partial", saved[0].Content().Text, "a restarted reader sees the last checkpoint")
}

func TestStreamCheckpointSurvivesReopenWithoutFinalizingWorker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stream.db")
	conn, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	defer conn.Close()
	_, err = conn.Exec(`CREATE TABLE messages (
		id TEXT PRIMARY KEY, session_id TEXT, role TEXT, parts TEXT,
		model TEXT, created_at INTEGER, updated_at INTEGER, finished_at INTEGER)`)
	require.NoError(t, err)
	store := message.NewService(db.New(conn))
	msg, err := store.Create(context.Background(), "s", message.CreateMessageParams{Role: message.Assistant})
	require.NoError(t, err)
	now := time.Now()
	w := newStreamMessageWriter(store, &msg, now)
	w.append(provider.ProviderEvent{Type: provider.EventContentDelta, Content: "durable checkpoint"})
	require.NoError(t, w.flush(context.Background(), now.Add(streamCheckpointInterval), false))
	w.append(provider.ProviderEvent{Type: provider.EventContentDelta, Content: " volatile tail"})
	require.NoError(t, w.flush(context.Background(), now.Add(streamCheckpointInterval+streamPublishInterval), false))
	live, err := store.List(context.Background(), "s")
	require.NoError(t, err)
	require.Equal(t, "durable checkpoint volatile tail", live[0].Content().Text, "overflow recovery includes the last live snapshot")
	live[0].Parts[0] = message.TextContent{Text: "caller mutation"}
	current, err := store.Get(context.Background(), msg.ID)
	require.NoError(t, err)
	require.Equal(t, "durable checkpoint volatile tail", current.Content().Text)
	// A second connection reads SQL without asking the original worker to flush:
	// loss of the process must not lose already acknowledged checkpoints.
	reopened, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	defer reopened.Close()
	saved, err := message.NewService(db.New(reopened)).Get(context.Background(), msg.ID)
	require.NoError(t, err)
	require.Equal(t, "durable checkpoint", saved.Content().Text)
	require.Nil(t, saved.FinishPart(), "the recovered checkpoint is still interrupted")
	require.Equal(t, "durable checkpoint volatile tail", msg.Content().Text)
	msg.AddFinish(message.FinishReasonCanceled)
	require.NoError(t, store.Update(context.Background(), msg))
	final, err := message.NewService(db.New(reopened)).Get(context.Background(), msg.ID)
	require.NoError(t, err)
	require.Equal(t, "durable checkpoint volatile tail", final.Content().Text)
	require.Equal(t, message.FinishReasonCanceled, final.FinishPart().Reason)
}

func TestStreamWriterFlushesBeforeToolTransitionAndPropagatesStorageFailure(t *testing.T) {
	store, msg := newRecordingStream(t)
	w := newStreamMessageWriter(store, &msg, time.Now())
	require.NoError(t, w.handle(context.Background(), provider.ProviderEvent{Type: provider.EventContentDelta, Content: "before tool"}))
	require.NoError(t, w.handle(context.Background(), provider.ProviderEvent{Type: provider.EventToolUseStart}))
	require.Equal(t, 1, store.writes)
	store.failure = errors.New("checkpoint failed")
	w.append(provider.ProviderEvent{Type: provider.EventContentDelta, Content: " preserved"})
	require.ErrorIs(t, w.flush(context.Background(), time.Now(), true), store.failure)
	require.Equal(t, "before tool preserved", msg.Content().Text)
	require.Positive(t, w.unpersisted, "failure must not acknowledge an unsaved checkpoint")
}

func TestStreamReaderPublishesAndCheckpointsWithoutAnotherProviderEvent(t *testing.T) {
	a, _, _ := steeringAgent(t)
	store, msg := newRecordingStream(t)
	a.messages = store
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan provider.ProviderEvent, 1)
	events <- provider.ProviderEvent{Type: provider.EventContentDelta, Content: "idle output"}
	usage := a.startStreamUsage("session", "local")
	done := make(chan error, 1)
	go func() { done <- a.readAssistantEvents(ctx, "session", &msg, events, time.Now(), usage) }()
	require.Eventually(t, func() bool {
		store.mu.Lock()
		defer store.mu.Unlock()
		return store.writes > 0 && len(store.publications) > 0
	}, 3*time.Second, 10*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	a.finishStreamUsage("session", usage)
	require.Equal(t, "idle output", msg.Content().Text)
}

func TestStreamCompletionCanReorderToolPartsWithoutCorruptingText(t *testing.T) {
	a, _, _ := steeringAgent(t)
	store, msg := newRecordingStream(t)
	a.messages = store
	usage := a.startStreamUsage("session", "local")
	defer a.finishStreamUsage("session", usage)
	events := make(chan provider.ProviderEvent, 4)
	call := message.ToolCall{ID: "one", Name: "view", Input: `{}`}
	events <- provider.ProviderEvent{Type: provider.EventToolUseStart, ToolCall: &call}
	events <- provider.ProviderEvent{Type: provider.EventContentDelta, Content: "after tool"}
	events <- provider.ProviderEvent{Type: provider.EventComplete, Response: &provider.ProviderResponse{ToolCalls: []message.ToolCall{call}, FinishReason: message.FinishReasonToolUse}}
	close(events)
	require.NoError(t, a.readAssistantEvents(context.Background(), "session", &msg, events, time.Now(), usage))
	require.Equal(t, "after tool", msg.Content().Text)
	require.Equal(t, []message.ToolCall{call}, msg.ToolCalls())
	require.Equal(t, message.FinishReasonToolUse, msg.FinishPart().Reason)
}
