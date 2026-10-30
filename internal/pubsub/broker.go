package pubsub

import (
	"context"
	"sync"
)

const bufferSize = 64

type Broker[T any] struct {
	subs              map[chan Event[T]]struct{}
	mu                sync.RWMutex
	done              chan struct{}
	subCount          int
	channelBufferSize int
	overflows         uint64
	waiters           sync.WaitGroup
}

func NewBroker[T any]() *Broker[T] {
	return NewBrokerWithOptions[T](bufferSize, 1000)
}

func NewBrokerWithOptions[T any](channelBufferSize, maxEvents int) *Broker[T] {
	if maxEvents <= 0 {
		maxEvents = 1000
	}
	if channelBufferSize <= 0 {
		channelBufferSize = 1
	}
	channelBufferSize = min(channelBufferSize, maxEvents)
	b := &Broker[T]{
		subs:              make(map[chan Event[T]]struct{}),
		done:              make(chan struct{}),
		subCount:          0,
		channelBufferSize: channelBufferSize,
	}
	return b
}

func (b *Broker[T]) Shutdown() {
	b.mu.Lock()
	select {
	case <-b.done: // Already closed
		b.mu.Unlock()
		b.waiters.Wait()
		return
	default:
		close(b.done)
	}

	for ch := range b.subs {
		delete(b.subs, ch)
		close(ch)
	}

	b.subCount = 0
	b.mu.Unlock()
	b.waiters.Wait()
}

func (b *Broker[T]) Subscribe(ctx context.Context) <-chan Event[T] {
	b.mu.Lock()
	defer b.mu.Unlock()

	select {
	case <-b.done:
		ch := make(chan Event[T])
		close(ch)
		return ch
	default:
	}

	sub := make(chan Event[T], b.channelBufferSize)
	b.subs[sub] = struct{}{}
	b.subCount++

	b.waiters.Add(1)
	go func() {
		defer b.waiters.Done()
		select {
		case <-ctx.Done():
		case <-b.done:
			return
		}

		b.mu.Lock()
		defer b.mu.Unlock()

		select {
		case <-b.done:
			return
		default:
		}

		delete(b.subs, sub)
		close(sub)
		b.subCount--
	}()

	return sub
}

func (b *Broker[T]) GetSubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.subCount
}

func (b *Broker[T]) Publish(t EventType, payload T) {
	b.PublishEvent(Event[T]{Type: t, Payload: payload})
}

// PublishEvent forwards an event without losing reconciliation state across
// brokers, such as a hot-swappable agent proxy.
func (b *Broker[T]) PublishEvent(source Event[T]) {
	b.mu.Lock()
	defer b.mu.Unlock()
	select {
	case <-b.done:
		return
	default:
	}

	// Publishers serialize only bounded, in-memory channel operations. The same
	// lock protects cancellation/shutdown from sending to a closed subscriber.
	for sub := range b.subs {
		event := source
		select {
		case sub <- event:
		default:
			// A slow consumer keeps a bounded tail, including the newest state.
			// Any lost lifecycle transition must trigger authoritative reload;
			// a channel alone cannot guarantee unlimited critical-event delivery.
			select {
			case <-sub:
				b.overflows++
			default:
			}
			event.ReconcileRequired = true
			sub <- event // At least one slot is free; other publishers hold this lock.
		}
	}
}

// OverflowCount counts discarded queued deliveries, never published payloads.
func (b *Broker[T]) OverflowCount() uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.overflows
}
