package runtime

import (
	"log/slog"
	"sync"
)

type EventPublisher interface {
	Publish(event Event)
}

type EventSubscriber interface {
	Subscribe(id string) <-chan Event
	Unsubscribe(id string)
}

type EventBrokerInterface interface {
	EventPublisher
	EventSubscriber
	Close()
}

type EventBroker struct {
	mu          sync.RWMutex
	subscribers map[string]chan Event
	buffer      int
	closed      bool
}

func NewEventBroker(bufferSize int) *EventBroker {
	return &EventBroker{
		subscribers: make(map[string]chan Event),
		buffer:      bufferSize,
	}
}

func (b *EventBroker) Subscribe(id string) <-chan Event {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		ch := make(chan Event)
		close(ch)
		return ch
	}

	if ch, exists := b.subscribers[id]; exists {
		return ch
	}

	ch := make(chan Event, b.buffer)
	b.subscribers[id] = ch
	return ch
}

func (b *EventBroker) Unsubscribe(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if ch, exists := b.subscribers[id]; exists {
		close(ch)
		delete(b.subscribers, id)
	}
}

func (b *EventBroker) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return
	}

	for id, ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			slog.Warn("Event dropped, subscriber channel full", "subscriber", id)
		}
	}
}

func (b *EventBroker) PublishSync(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return
	}

	for _, ch := range b.subscribers {
		ch <- event
	}
}

func (b *EventBroker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	b.closed = true
	for id, ch := range b.subscribers {
		close(ch)
		delete(b.subscribers, id)
	}
}

func (b *EventBroker) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}
