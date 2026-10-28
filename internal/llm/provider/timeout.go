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
	go func() {
		defer close(out)
		streamCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		input := p.Provider.StreamResponse(streamCtx, messages, ts)
		timer := time.NewTimer(p.first)
		defer timer.Stop()
		first := true
		firstDeadline := time.Now().Add(p.first)
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				cancel()
				phase, duration := "stopped sending data", p.idle
				if first {
					phase, duration = "sent no initial response", p.first
				}
				err := fmt.Errorf("model %s for %s: %w", phase, duration, context.DeadlineExceeded)
				select {
				case out <- ProviderEvent{Type: EventError, Error: err}:
				case <-ctx.Done():
				}
				return
			case event, ok := <-input:
				if !ok {
					return
				}
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				select {
				case out <- event:
				case <-ctx.Done():
					return
				}
				if event.Content != "" || event.Thinking != "" || event.ToolCall != nil {
					first = false
				}
				if event.Type == EventComplete || event.Type == EventError {
					return
				}
				if first {
					timer.Reset(max(time.Nanosecond, time.Until(firstDeadline)))
				} else {
					timer.Reset(p.idle)
				}
			}
		}
	}()
	return out
}
