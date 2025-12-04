package runtime

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/model/provider"
)

func TestTruncateTitleFunction(t *testing.T) {
	tests := []struct {
		name      string
		title     string
		maxLength int
		want      string
	}{
		{
			name:      "short title",
			title:     "Hello",
			maxLength: 50,
			want:      "Hello",
		},
		{
			name:      "exact length",
			title:     "12345",
			maxLength: 5,
			want:      "12345",
		},
		{
			name:      "needs truncation",
			title:     "This is a very long title that needs to be truncated",
			maxLength: 20,
			want:      "This is a very lo...",
		},
		{
			name:      "very short max",
			title:     "Hello",
			maxLength: 2,
			want:      "...",
		},
		{
			name:      "max length 3",
			title:     "Hello",
			maxLength: 3,
			want:      "...",
		},
		{
			name:      "empty title",
			title:     "",
			maxLength: 50,
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateTitle(tt.title, tt.maxLength)
			assert.Equal(t, tt.want, got)
		})
	}
}

type mockTitleEventPublisher struct {
	mu     sync.Mutex
	events []Event
}

func (m *mockTitleEventPublisher) Publish(event Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
}

func (m *mockTitleEventPublisher) Events() []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Event{}, m.events...)
}

func TestTitleGenerator_Wait(t *testing.T) {
	publisher := &mockTitleEventPublisher{}

	gen := newTitleGenerator(
		publisher,
		func() provider.Provider { return nil },
		func() string { return "test-agent" },
	)

	done := make(chan struct{})
	go func() {
		gen.Wait()
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(100 * time.Millisecond):
		t.Error("Wait() blocked unexpectedly")
	}
}

func TestNewTitleGenerator(t *testing.T) {
	publisher := &mockTitleEventPublisher{}
	getModel := func() provider.Provider { return nil }
	currentAgent := func() string { return "test-agent" }

	gen := newTitleGenerator(publisher, getModel, currentAgent)

	require.NotNil(t, gen)
	require.NotNil(t, gen.events)
	require.NotNil(t, gen.getModel)
	require.NotNil(t, gen.currentAgent)
	assert.Equal(t, "test-agent", gen.currentAgent())
}
