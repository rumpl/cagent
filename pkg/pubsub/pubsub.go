package pubsub

import (
	"context"
	"sync"
)

// PubSub is a generic publish-subscribe system with non-blocking publishers
// and unbounded subscriber buffers.
type PubSub[T any] struct {
	mu          sync.RWMutex
	subscribers map[uint64]*subscriber[T]
	nextID      uint64
	closed      bool
}

// New creates a new PubSub instance
func New[T any]() *PubSub[T] {
	return &PubSub[T]{
		subscribers: make(map[uint64]*subscriber[T]),
	}
}

// Publish sends an event to all subscribers. This method never blocks.
// If a subscriber is slow, events are buffered in an unbounded queue.
func (ps *PubSub[T]) Publish(payload T) {
	event := Event[T]{
		Payload: payload,
	}

	ps.mu.RLock()
	defer ps.mu.RUnlock()

	if ps.closed {
		return
	}

	for _, sub := range ps.subscribers {
		sub.send(event)
	}
}

// Subscribe returns a channel that receives all events.
// The subscription is automatically cancelled when the context is done.
// The returned channel is closed when the subscription ends.
func (ps *PubSub[T]) Subscribe(ctx context.Context) <-chan Event[T] {
	return ps.subscribe(ctx, "")
}

// subscribe creates a new subscription, optionally filtered by topic
func (ps *PubSub[T]) subscribe(ctx context.Context, topic string) <-chan Event[T] {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if ps.closed {
		ch := make(chan Event[T])
		close(ch)
		return ch
	}

	subCtx, cancel := context.WithCancel(ctx)
	sub := newSubscriber[T](ps.nextID, topic, subCtx, cancel)
	ps.subscribers[ps.nextID] = sub
	ps.nextID++

	// Start the goroutine that handles sending events to the subscriber
	go sub.run()

	// Monitor context cancellation to clean up the subscription
	go func() {
		<-subCtx.Done()
		ps.removeSubscriber(sub.id)
	}()

	return sub.ch
}

// removeSubscriber removes a subscriber from the pubsub
func (ps *PubSub[T]) removeSubscriber(id uint64) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if sub, exists := ps.subscribers[id]; exists {
		sub.close()
		delete(ps.subscribers, id)
	}
}

// Close shuts down the PubSub and all subscriptions
func (ps *PubSub[T]) Close() {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if ps.closed {
		return
	}

	ps.closed = true

	for _, sub := range ps.subscribers {
		sub.cancel()
		sub.close()
	}
	ps.subscribers = make(map[uint64]*subscriber[T])
}
