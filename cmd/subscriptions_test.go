package cmd

import (
	"context"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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
