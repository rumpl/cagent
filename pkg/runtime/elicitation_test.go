package runtime

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/tools"
)

type mockEventPublisher struct {
	mu     sync.Mutex
	events []Event
}

func (m *mockEventPublisher) Publish(event Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
}

func (m *mockEventPublisher) Events() []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Event{}, m.events...)
}

func TestElicitationHandler_Handler(t *testing.T) {
	publisher := &mockEventPublisher{}

	handler := newElicitationHandler(
		publisher,
		func() string { return "test-agent" },
	)

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	var result tools.ElicitationResult
	var handlerErr error
	done := make(chan struct{})

	go func() {
		result, handlerErr = handler.Handler(ctx, &mcp.ElicitParams{
			Message:         "Please confirm",
			RequestedSchema: map[string]any{"type": "object"},
		})
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)

	events := publisher.Events()
	require.Len(t, events, 1)

	elicitEvent, ok := events[0].(*ElicitationRequestEvent)
	require.True(t, ok, "expected ElicitationRequestEvent, got %T", events[0])
	assert.Equal(t, "Please confirm", elicitEvent.Message)

	err := handler.Resume(ctx, tools.ElicitationActionAccept, map[string]any{"confirmed": true})
	require.NoError(t, err)

	<-done

	require.NoError(t, handlerErr)
	assert.Equal(t, tools.ElicitationActionAccept, result.Action)
}

func TestElicitationHandler_HandlerTimeout(t *testing.T) {
	publisher := &mockEventPublisher{}

	handler := newElicitationHandler(
		publisher,
		func() string { return "test-agent" },
	)

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, err := handler.Handler(ctx, &mcp.ElicitParams{
		Message: "Please confirm",
	})

	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestElicitationHandler_ResumeNoRequest(t *testing.T) {
	publisher := &mockEventPublisher{}

	handler := newElicitationHandler(
		publisher,
		func() string { return "test-agent" },
	)

	err := handler.Resume(t.Context(), tools.ElicitationActionAccept, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no elicitation request in progress")
}

func TestElicitationHandler_NilPublisher(t *testing.T) {
	handler := newElicitationHandler(
		nil,
		func() string { return "test-agent" },
	)

	_, err := handler.Handler(t.Context(), &mcp.ElicitParams{
		Message: "Please confirm",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no events publisher available")
}

func TestElicitationHandler_GetHandlerFunc(t *testing.T) {
	publisher := &mockEventPublisher{}

	handler := newElicitationHandler(
		publisher,
		func() string { return "test-agent" },
	)

	handlerFunc := handler.GetHandlerFunc()
	require.NotNil(t, handlerFunc)
}

func TestElicitationHandler_ResumeContextCancelled(t *testing.T) {
	publisher := &mockEventPublisher{}

	handler := newElicitationHandler(
		publisher,
		func() string { return "test-agent" },
	)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := handler.Resume(ctx, tools.ElicitationActionAccept, nil)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestElicitationHandler_DeclineAction(t *testing.T) {
	publisher := &mockEventPublisher{}

	handler := newElicitationHandler(
		publisher,
		func() string { return "test-agent" },
	)

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	var result tools.ElicitationResult
	var handlerErr error
	done := make(chan struct{})

	go func() {
		result, handlerErr = handler.Handler(ctx, &mcp.ElicitParams{
			Message: "Please confirm",
		})
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)

	err := handler.Resume(ctx, tools.ElicitationActionDecline, nil)
	require.NoError(t, err)

	<-done

	require.NoError(t, handlerErr)
	assert.Equal(t, tools.ElicitationActionDecline, result.Action)
}
