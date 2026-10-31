package cmd

import (
	"context"
	"fmt"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/history"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/raulaguila/opcode/internal/session"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionCleanupTimeoutDoesNotCloseOutputUnderALatePublisher(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	output := make(chan tea.Msg, 1)
	release := make(chan struct{})
	published := make(chan any, 1)
	var writers sync.WaitGroup
	writers.Add(1)
	go func() {
		defer writers.Done()
		defer func() { published <- recover() }()
		<-release
		output <- "late message"
	}()
	stopSubscriptions(cancel, &writers, output, time.Millisecond)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	close(release)
	select {
	case recovered := <-published:
		require.Nil(t, recovered, "cleanup must preserve the output until every publisher exits")
	case <-time.After(time.Second):
		t.Fatal("late publisher did not finish")
	}
	var received []tea.Msg
	done := make(chan struct{})
	go func() {
		for event := range output {
			received = append(received, event)
		}
		close(done)
	}()
	select {
	case <-done:
		require.Equal(t, []tea.Msg{"late message"}, received)
	case <-time.After(time.Second):
		t.Fatal("output did not close after the publisher finished")
	}
}

type subscriptionAgentProbe struct {
	agent.Service
	broker *pubsub.Broker[agent.AgentEvent]
}

func (p *subscriptionAgentProbe) Subscribe(ctx context.Context) <-chan pubsub.Event[agent.AgentEvent] {
	return p.broker.Subscribe(ctx)
}

func TestCentralHistorySubscriptionRelaysBothSessionsAndCancels(t *testing.T) {
	files := history.NewService(nil, nil)
	source := files.(interface {
		GetSubscriberCount() int
		PublishEvent(pubsub.Event[history.File])
		Shutdown()
	})
	defer source.Shutdown()
	coder := &subscriptionAgentProbe{broker: pubsub.NewBroker[agent.AgentEvent]()}
	defer coder.broker.Shutdown()
	application := &app.App{Sessions: session.NewService(nil), Messages: message.NewService(nil), Permissions: permission.NewPermissionService(), CoderAgent: coder, History: files}
	events, cleanup := setupSubscriptions(application, context.Background())
	defer cleanup()
	require.Eventually(t, func() bool { return source.GetSubscriberCount() == 1 }, time.Second, time.Millisecond)
	for index := range 80 {
		owner := "root"
		if index%2 == 1 {
			owner = "child"
		}
		file := history.File{ID: fmt.Sprint(index), SessionID: owner}
		source.PublishEvent(pubsub.Event[history.File]{Type: pubsub.UpdatedEvent, Payload: file, ReconcileRequired: index == 79})
		got := receiveHistoryEvent(t, events)
		require.Equal(t, file, got.Payload)
		require.Equal(t, index == 79, got.ReconcileRequired)
	}
	require.Equal(t, 1, source.GetSubscriberCount())
	cleanup()
	require.Eventually(t, func() bool { return source.GetSubscriberCount() == 0 }, time.Second, time.Millisecond)
	for range events {
	} // The central pipeline owns closing only after all workers exit.
}
func receiveHistoryEvent(t *testing.T, events <-chan tea.Msg) pubsub.Event[history.File] {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case msg, ok := <-events:
			require.True(t, ok, "history pipeline closed before delivering the event")
			if event, ok := msg.(pubsub.Event[history.File]); ok {
				return event
			}
		case <-timer.C:
			t.Fatal("central history event was not delivered")
			return pubsub.Event[history.File]{}
		}
	}
}
