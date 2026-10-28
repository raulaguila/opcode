package provider

import (
	"context"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

type timeoutFixture struct {
	Provider
	events    chan ProviderEvent
	cancelled chan struct{}
}

func (p timeoutFixture) Model() models.Model { return models.Model{} }
func (p timeoutFixture) StreamResponse(ctx context.Context, _ []message.Message, _ []tools.BaseTool) <-chan ProviderEvent {
	go func() { <-ctx.Done(); close(p.cancelled) }()
	return p.events
}
func TestStreamTimeoutDistinguishesInitialWaitAndIdle(t *testing.T) {
	for _, started := range []bool{false, true} {
		t.Run(map[bool]string{false: "first", true: "idle"}[started], func(t *testing.T) {
			fixture := timeoutFixture{events: make(chan ProviderEvent, 1), cancelled: make(chan struct{})}
			if started {
				fixture.events <- ProviderEvent{Type: EventContentDelta, Content: "partial"}
			}
			stream := WithTimeouts(fixture, 40*time.Millisecond, 20*time.Millisecond).StreamResponse(context.Background(), nil, nil)
			if started {
				require.Equal(t, EventContentDelta, (<-stream).Type)
			}
			event := <-stream
			require.Equal(t, EventError, event.Type)
			require.ErrorIs(t, event.Error, context.DeadlineExceeded)
			if started {
				require.ErrorContains(t, event.Error, "stopped sending")
			} else {
				require.ErrorContains(t, event.Error, "initial response")
			}
			select {
			case <-fixture.cancelled:
			case <-time.After(time.Second):
				t.Fatal("provider was not cancelled")
			}
		})
	}
}
func TestActiveStreamAndSlowConsumerDoNotTimeOut(t *testing.T) {
	fixture := timeoutFixture{events: make(chan ProviderEvent), cancelled: make(chan struct{})}
	stream := WithTimeouts(fixture, time.Second, 100*time.Millisecond).StreamResponse(context.Background(), nil, nil)
	go func() {
		fixture.events <- ProviderEvent{Type: EventContentDelta, Content: "first"}
		fixture.events <- ProviderEvent{Type: EventComplete, Response: &ProviderResponse{FinishReason: message.FinishReasonEndTurn}}
	}()
	// The wrapper is blocked delivering to this consumer; it must stop its timer.
	time.Sleep(150 * time.Millisecond)
	require.Equal(t, EventContentDelta, (<-stream).Type)
	require.Equal(t, EventComplete, (<-stream).Type)
	_, ok := <-stream
	require.False(t, ok)
}
