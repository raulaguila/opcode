package pubsub

import "context"

const (
	CreatedEvent EventType = "created"
	UpdatedEvent EventType = "updated"
	DeletedEvent EventType = "deleted"
)

type Suscriber[T any] interface {
	Subscribe(context.Context) <-chan Event[T]
}

type (
	// EventType identifies the type of event
	EventType string

	// Event represents an event in the lifecycle of a resource
	Event[T any] struct {
		Type    EventType
		Payload T
		// ReconcileRequired means a bounded subscriber overflowed. Consumers
		// must reload authoritative state before applying further transitions.
		// It also propagates across repeated saturation because every overflow
		// marks the newest retained event.
		ReconcileRequired bool
	}

	Publisher[T any] interface {
		Publish(EventType, T)
	}
)
