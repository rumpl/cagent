package runtime

import (
	"log/slog"
	"time"

	"github.com/docker/docker-agent/pkg/runtime/contract"
)

// channelSink adapts a chan Event into an [contract.EventSink]. Sends are
// blocking so back-pressure is preserved between producers and the
// consumer. Send-on-closed-channel panics are recovered and the event
// is dropped, since a closed channel signals that the consumer has
// gone away.
type channelSink struct {
	ch chan<- contract.Event
}

// NewChannelSink returns an [contract.EventSink] that writes to ch. This is the
// standard bridge between the runtime's EventSink-based internals and
// external callers that create raw event channels (e.g. app.go,
// tests).
func NewChannelSink(ch chan<- contract.Event) contract.EventSink {
	return &channelSink{ch: ch}
}

func (s *channelSink) Emit(e contract.Event) {
	defer func() { recover() }() //nolint:errcheck // swallow send-on-closed-channel panic
	s.ch <- e
}

// nonBlockingChannelSink wraps an event channel with non-blocking
// semantics: if the buffer is full, the event is dropped instead of
// blocking the producer. Use this only from long-lived goroutines
// that may outlive the event channel (e.g. the RAG file watcher);
// regular runtime code should use the blocking [channelSink] so
// back-pressure is preserved.
type nonBlockingChannelSink struct {
	ch chan<- contract.Event
}

// nonBlocking returns a non-blocking sink for sink. If sink wraps a
// channel directly, the result writes to that channel with a
// select-default; otherwise, sink is returned unchanged because
// non-channel sinks (notably [EventSinkFunc] used in tests) do not
// have an underlying buffer that can fill up.
func nonBlocking(sink contract.EventSink) contract.EventSink {
	if cs, ok := sink.(*channelSink); ok {
		return nonBlockingChannelSink{ch: cs.ch}
	}
	return sink
}

func (s nonBlockingChannelSink) Emit(e contract.Event) {
	defer func() { recover() }() //nolint:errcheck // swallow send-on-closed-channel panic
	select {
	case s.ch <- e:
	default:
	}
}

// boundedChannelSink wraps an event channel with a bounded-blocking send:
// it waits up to wait for the consumer to accept the event, then drops it.
// Use this for turn-boundary events that a live, still-draining consumer
// must reliably receive, but that must not hang teardown indefinitely when
// the consumer has genuinely gone away (#3070, #4136). Regular runtime code
// should use the unbounded blocking [channelSink] instead, so ordinary
// back-pressure is preserved.
type boundedChannelSink struct {
	ch   chan<- contract.Event
	wait time.Duration
}

// bounded returns a bounded-blocking sink for sink with the given deadline.
// If sink wraps a channel directly, the result writes to that channel with a
// timeout; otherwise, sink is returned unchanged because non-channel sinks
// (notably [EventSinkFunc] used in tests) do not have an underlying buffer
// that can fill up.
func bounded(sink contract.EventSink, wait time.Duration) contract.EventSink {
	if cs, ok := sink.(*channelSink); ok {
		return boundedChannelSink{ch: cs.ch, wait: wait}
	}
	return sink
}

func (s boundedChannelSink) Emit(e contract.Event) {
	defer func() { recover() }() //nolint:errcheck // swallow send-on-closed-channel panic
	t := time.NewTimer(s.wait)
	defer t.Stop()
	select {
	case s.ch <- e:
	case <-t.C:
		slog.Warn("dropping event: consumer did not drain within the delivery deadline", "wait", s.wait)
	}
}

// EventSinkFunc adapts a plain function into an [contract.EventSink], following
// the same adapter pattern as http.HandlerFunc. This is convenient for
// tests and one-off callbacks that don't need a full struct.
type EventSinkFunc func(contract.Event)

func (f EventSinkFunc) Emit(e contract.Event) { f(e) }
