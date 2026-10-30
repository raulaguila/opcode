package chat

import (
	"context"
	"testing"

	"github.com/raulaguila/opcode/internal/history"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/stretchr/testify/require"
)

type sidebarHistoryProbe struct {
	history.Service
	broker *pubsub.Broker[history.File]
	loads  int
	latest []history.File
}

func (h *sidebarHistoryProbe) Subscribe(ctx context.Context) <-chan pubsub.Event[history.File] {
	return h.broker.Subscribe(ctx)
}
func (h *sidebarHistoryProbe) ListLatestSessionFiles(context.Context, string) ([]history.File, error) {
	h.loads++
	return h.latest, nil
}
func (*sidebarHistoryProbe) ListBySession(context.Context, string) ([]history.File, error) {
	return nil, nil
}

func TestSidebarConsumesCentralHistoryWithoutCreatingSubscriptions(t *testing.T) {
	broker := pubsub.NewBroker[history.File]()
	defer broker.Shutdown()
	source := &sidebarHistoryProbe{broker: broker}
	m := NewSidebarCmp(session.Session{ID: "root"}, source).(*sidebarCmp)
	require.Nil(t, m.Init())
	require.Equal(t, 1, source.loads)
	for range 80 {
		_, cmd := m.Update(pubsub.Event[history.File]{Type: pubsub.CreatedEvent, Payload: history.File{SessionID: "root", Version: history.InitialVersion}})
		require.Nil(t, cmd)
	}
	m.Update(pubsub.Event[history.File]{Payload: history.File{SessionID: "child"}})
	m.Update(pubsub.Event[history.File]{Payload: history.File{SessionID: "root", Version: history.InitialVersion}})
	require.Zero(t, broker.GetSubscriberCount())
	require.Equal(t, "root", m.session.ID)
}
func TestSidebarReconcilesOverflowEvenWhenNewestEventBelongsToAnotherSession(t *testing.T) {
	source := &sidebarHistoryProbe{}
	m := NewSidebarCmp(session.Session{ID: "root"}, source).(*sidebarCmp)
	m.Init()
	m.modFiles["stale.go"] = struct{ additions, removals int }{3, 1}
	_, cmd := m.Update(pubsub.Event[history.File]{Type: pubsub.UpdatedEvent, ReconcileRequired: true, Payload: history.File{SessionID: "child"}})
	require.Nil(t, cmd)
	require.Equal(t, 2, source.loads)
	require.Empty(t, m.modFiles)
	m.modFiles["deleted.go"] = struct{ additions, removals int }{1, 0}
	m.Update(pubsub.Event[history.File]{Type: pubsub.DeletedEvent, Payload: history.File{SessionID: "root"}})
	require.Equal(t, 3, source.loads)
	require.Empty(t, m.modFiles)
}
