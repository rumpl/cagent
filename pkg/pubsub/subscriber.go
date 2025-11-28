package pubsub

import (
	"context"
	"sync"
)

// subscriber represents a single subscription to the PubSub system
type subscriber[T any] struct {
	id     uint64
	ch     chan Event[T]
	ctx    context.Context
	cancel context.CancelFunc
	topic  string // Empty means all topics

	// Unbounded buffer to ensure publishers never block
	bufferMu sync.Mutex
	buffer   []Event[T]
	notify   chan struct{}
	closed   bool
}

// newSubscriber creates a new subscriber
func newSubscriber[T any](id uint64, topic string, ctx context.Context, cancel context.CancelFunc) *subscriber[T] {
	return &subscriber[T]{
		id:     id,
		ch:     make(chan Event[T], 128), // Buffer for immediate delivery
		ctx:    ctx,
		cancel: cancel,
		topic:  topic,
		buffer: make([]Event[T], 0),
		notify: make(chan struct{}, 1),
	}
}

// send adds an event to the subscriber's buffer (non-blocking)
func (s *subscriber[T]) send(event Event[T]) {
	s.bufferMu.Lock()
	if s.closed {
		s.bufferMu.Unlock()
		return
	}
	s.buffer = append(s.buffer, event)
	s.bufferMu.Unlock()

	// Signal that there are events to process
	select {
	case s.notify <- struct{}{}:
	default:
		// Already notified, no need to send again
	}
}

// run processes the buffer and sends events to the channel
func (s *subscriber[T]) run() {
	defer close(s.ch)

	for {
		select {
		case <-s.ctx.Done():
			// Drain any remaining events before closing
			s.drainBuffer()
			return
		case <-s.notify:
			s.drainBuffer()
		}
	}
}

// drainBuffer sends all buffered events to the channel
func (s *subscriber[T]) drainBuffer() {
	for {
		s.bufferMu.Lock()
		if len(s.buffer) == 0 || s.closed {
			s.bufferMu.Unlock()
			return
		}
		// Take all events from buffer
		events := s.buffer
		s.buffer = make([]Event[T], 0)
		s.bufferMu.Unlock()

		// Send events to channel
		for _, event := range events {
			select {
			case s.ch <- event:
			case <-s.ctx.Done():
				return
			}
		}
	}
}

// close marks the subscriber as closed
func (s *subscriber[T]) close() {
	s.bufferMu.Lock()
	s.closed = true
	s.bufferMu.Unlock()
}
