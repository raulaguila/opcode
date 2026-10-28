package pubsub

import (
	"context"
	"sync"
	"testing"
)

func TestPublishWhileSubscribersCancelAndBrokerShutsDown(t *testing.T) {
	b := NewBroker[int]()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		_ = b.Subscribe(ctx)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer cancel()
			for j := 0; j < 100; j++ {
				b.Publish(UpdatedEvent, j)
			}
		}()
	}
	wg.Add(2)
	go func() { defer wg.Done(); b.Shutdown() }()
	go func() { defer wg.Done(); b.Shutdown() }()
	wg.Wait()
}
