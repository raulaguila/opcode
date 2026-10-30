package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
)

type timeoutProvider struct {
	Provider
	first, idle time.Duration
}

func WithTimeouts(p Provider, first, idle time.Duration) Provider {
	return &timeoutProvider{p, first, idle}
}
func (p *timeoutProvider) SendMessages(ctx context.Context, messages []message.Message, ts []tools.BaseTool) (*ProviderResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, p.first)
	defer cancel()
	return p.Provider.SendMessages(ctx, messages, ts)
}
func (p *timeoutProvider) StreamResponse(ctx context.Context, messages []message.Message, ts []tools.BaseTool) <-chan ProviderEvent {
	out := make(chan ProviderEvent)
	go p.relayStream(ctx, messages, ts, out)
	return out
}

func (p *timeoutProvider) relayStream(ctx context.Context, messages []message.Message, ts []tools.BaseTool, out chan ProviderEvent) {
	defer close(out)
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	input := p.Provider.StreamResponse(streamCtx, messages, ts)
	timer := time.NewTimer(p.first)
	defer timer.Stop()
	relay := timeoutRelay{provider: p, ctx: ctx, out: out, timer: timer, first: true, firstDeadline: time.Now().Add(p.first)}
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			cancel()
			relay.timeout()
			return
		case event, ok := <-input:
			if !ok || !relay.event(event) {
				return
			}
		}
	}
}

type timeoutRelay struct {
	provider      *timeoutProvider
	ctx           context.Context
	out           chan<- ProviderEvent
	timer         *time.Timer
	first         bool
	firstDeadline time.Time
}

func (r *timeoutRelay) timeout() {
	phase, duration := "stopped sending data", r.provider.idle
	if r.first {
		phase, duration = "sent no initial response", r.provider.first
	}
	err := fmt.Errorf("model %s for %s: %w", phase, duration, context.DeadlineExceeded)
	publishEvent(r.ctx, r.out, ProviderEvent{Type: EventError, Error: err})
}
func (r *timeoutRelay) event(event ProviderEvent) bool {
	if !r.timer.Stop() {
		select {
		case <-r.timer.C:
		default:
		}
	}
	if !publishEvent(r.ctx, r.out, event) {
		return false
	}
	if event.Content != "" || event.Thinking != "" || event.ToolCall != nil {
		r.first = false
	}
	if event.Type == EventComplete || event.Type == EventError {
		return false
	}
	if r.first {
		r.timer.Reset(max(time.Nanosecond, time.Until(r.firstDeadline)))
	} else {
		r.timer.Reset(r.provider.idle)
	}
	return true
}
