package pubsub

import (
	"context"
	"sync"
	"testing"
)

func TestPublishWhileSubscribersCancelAndBrokerShutsDown(t *testing.T) {
	b := NewBroker[int]()
	var wg sync.WaitGroup
	for range 100 {
		ctx, cancel := context.WithCancel(context.Background())
		_ = b.Subscribe(ctx)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer cancel()
			for j := range 100 {
				b.Publish(UpdatedEvent, j)
			}
		}()
	}
	wg.Add(2)
	go func() { defer wg.Done(); b.Shutdown() }()
	go func() { defer wg.Done(); b.Shutdown() }()
	wg.Wait()
}

func TestBrokerHonorsCapacityAndMarksLostTransitions(t *testing.T) {
	b := NewBrokerWithOptions[int](1, 3)
	defer b.Shutdown()
	events := b.Subscribe(context.Background())
	if cap(events) != 1 {
		t.Fatalf("capacity = %d, want 1", cap(events))
	}
	b.Publish(CreatedEvent, 1)
	b.Publish(UpdatedEvent, 2)
	b.Publish(DeletedEvent, 3)
	event := <-events
	if event.Type != DeletedEvent || event.Payload != 3 || !event.ReconcileRequired {
		t.Fatalf("latest lifecycle state or reconciliation marker lost: %+v", event)
	}
	if b.OverflowCount() != 2 {
		t.Fatalf("overflows = %d, want 2", b.OverflowCount())
	}
}

func TestBrokerShutdownJoinsUncancelledSubscriptionWaiters(t *testing.T) {
	b := NewBrokerWithOptions[int](8, 2)
	var subscriptions []<-chan Event[int]
	for range 32 {
		subscriptions = append(subscriptions, b.Subscribe(context.Background()))
	}
	if cap(subscriptions[0]) != 2 {
		t.Fatalf("maxEvents did not bound buffer")
	}
	b.Shutdown() // Must join waiters without requiring any subscriber cancellation.
	if b.GetSubscriberCount() != 0 {
		t.Fatal("subscribers retained after shutdown")
	}
	for _, events := range subscriptions {
		if _, open := <-events; open {
			t.Fatal("subscription remains open")
		}
	}
	b.Shutdown()
	if _, open := <-b.Subscribe(context.Background()); open {
		t.Fatal("late subscription is open")
	}
}

func TestBrokerPreservesUnsaturatedLifecycleOrder(t *testing.T) {
	b := NewBrokerWithOptions[int](3, 3)
	defer b.Shutdown()
	events := b.Subscribe(context.Background())
	for i, kind := range []EventType{CreatedEvent, UpdatedEvent, DeletedEvent} {
		b.Publish(kind, i)
	}
	for i, kind := range []EventType{CreatedEvent, UpdatedEvent, DeletedEvent} {
		event := <-events
		if event.Type != kind || event.Payload != i || event.ReconcileRequired {
			t.Fatalf("event %d: %+v", i, event)
		}
	}
}

func BenchmarkBrokerSaturatedPublish(b *testing.B) {
	broker := NewBrokerWithOptions[int](1, 1)
	defer broker.Shutdown()
	broker.Subscribe(context.Background())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		broker.Publish(UpdatedEvent, i)
	}
}

func TestBrokerForwardPreservesReconciliation(t *testing.T) {
	broker := NewBroker[int]()
	defer broker.Shutdown()
	events := broker.Subscribe(context.Background())
	broker.PublishEvent(Event[int]{Type: UpdatedEvent, Payload: 42, ReconcileRequired: true})
	if event := <-events; !event.ReconcileRequired || event.Payload != 42 {
		t.Fatalf("forward lost reconciliation: %+v", event)
	}
}
