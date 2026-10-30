package app

import (
	"context"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

func TestAppShutdownClosesMessageService(t *testing.T) {
	messages := message.NewService(nil, message.WithSearchIndex(t.TempDir(), "/app-projection", nil))
	events := messages.Subscribe(context.Background())
	application := &App{Messages: messages}
	application.Shutdown()
	application.Shutdown()
	select {
	case _, open := <-events:
		require.False(t, open, "app shutdown owns message subscriptions and the search projection")
	case <-time.After(time.Second):
		t.Fatal("message service was not closed by app shutdown")
	}
}
