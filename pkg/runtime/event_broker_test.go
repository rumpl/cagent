package runtime

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventBroker_Subscribe(t *testing.T) {
	broker := NewEventBroker(10)
	defer broker.Close()

	ch := broker.Subscribe("test-1")
	require.NotNil(t, ch)
	assert.Equal(t, 1, broker.SubscriberCount())
}

func TestEventBroker_SubscribeDuplicate(t *testing.T) {
	broker := NewEventBroker(10)
	defer broker.Close()

	ch1 := broker.Subscribe("test-1")
	ch2 := broker.Subscribe("test-1")

	assert.Equal(t, ch1, ch2, "expected same channel for duplicate subscription")
	assert.Equal(t, 1, broker.SubscriberCount())
}

func TestEventBroker_Unsubscribe(t *testing.T) {
	broker := NewEventBroker(10)
	defer broker.Close()

	ch := broker.Subscribe("test-1")
	broker.Unsubscribe("test-1")

	_, ok := <-ch
	assert.False(t, ok, "expected channel to be closed")
	assert.Equal(t, 0, broker.SubscriberCount())
}

func TestEventBroker_UnsubscribeNonExistent(t *testing.T) {
	broker := NewEventBroker(10)
	defer broker.Close()

	broker.Unsubscribe("non-existent")
	assert.Equal(t, 0, broker.SubscriberCount())
}

func TestEventBroker_Publish(t *testing.T) {
	broker := NewEventBroker(10)
	defer broker.Close()

	ch := broker.Subscribe("test-1")

	event := Error("test error")
	broker.Publish(event)

	select {
	case received := <-ch:
		errEvent, ok := received.(*ErrorEvent)
		require.True(t, ok)
		assert.Equal(t, "test error", errEvent.Error)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for event")
	}
}

func TestEventBroker_PublishMultipleSubscribers(t *testing.T) {
	broker := NewEventBroker(10)
	defer broker.Close()

	ch1 := broker.Subscribe("test-1")
	ch2 := broker.Subscribe("test-2")

	event := Error("test error")
	broker.Publish(event)

	for i, ch := range []<-chan Event{ch1, ch2} {
		select {
		case received := <-ch:
			errEvent, ok := received.(*ErrorEvent)
			require.True(t, ok, "subscriber %d: expected ErrorEvent", i)
			assert.Equal(t, "test error", errEvent.Error, "subscriber %d: unexpected error message", i)
		case <-time.After(100 * time.Millisecond):
			t.Errorf("subscriber %d: timeout waiting for event", i)
		}
	}
}

func TestEventBroker_PublishFullBuffer(t *testing.T) {
	broker := NewEventBroker(1)
	defer broker.Close()

	ch := broker.Subscribe("test-1")

	broker.Publish(Error("event 1"))
	broker.Publish(Error("event 2"))

	select {
	case <-ch:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for event")
	}

	select {
	case <-ch:
		t.Error("unexpected second event - should have been dropped")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestEventBroker_PublishNoSubscribers(t *testing.T) {
	broker := NewEventBroker(10)
	defer broker.Close()

	broker.Publish(Error("test"))
}

func TestEventBroker_PublishSync(t *testing.T) {
	broker := NewEventBroker(10)
	defer broker.Close()

	ch := broker.Subscribe("test-1")

	go func() {
		broker.PublishSync(Error("sync event"))
	}()

	select {
	case received := <-ch:
		errEvent, ok := received.(*ErrorEvent)
		require.True(t, ok)
		assert.Equal(t, "sync event", errEvent.Error)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for sync event")
	}
}

func TestEventBroker_Close(t *testing.T) {
	broker := NewEventBroker(10)

	ch1 := broker.Subscribe("test-1")
	ch2 := broker.Subscribe("test-2")

	broker.Close()

	_, ok1 := <-ch1
	_, ok2 := <-ch2

	assert.False(t, ok1, "expected ch1 to be closed")
	assert.False(t, ok2, "expected ch2 to be closed")

	broker.Publish(Error("test"))

	ch3 := broker.Subscribe("test-3")
	_, ok3 := <-ch3
	assert.False(t, ok3, "expected closed channel from subscribe after close")
}

func TestEventBroker_CloseIdempotent(t *testing.T) {
	broker := NewEventBroker(10)

	broker.Subscribe("test-1")

	broker.Close()
	broker.Close()
}

func TestEventBroker_ConcurrentAccess(t *testing.T) {
	broker := NewEventBroker(100)
	defer broker.Close()

	var wg sync.WaitGroup

	for i := range 10 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ch := broker.Subscribe(fmt.Sprintf("sub-%d", id))

			for range 5 {
				select {
				case <-ch:
				case <-time.After(100 * time.Millisecond):
				}
			}
		}(i)
	}

	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				broker.Publish(Error("test"))
			}
		}()
	}

	wg.Wait()
}

func TestEventBroker_ConcurrentSubscribeUnsubscribe(t *testing.T) {
	broker := NewEventBroker(10)
	defer broker.Close()

	var wg sync.WaitGroup

	for i := range 50 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			subID := fmt.Sprintf("sub-%d", id)
			broker.Subscribe(subID)
			time.Sleep(time.Millisecond)
			broker.Unsubscribe(subID)
		}(i)
	}

	wg.Wait()
	assert.Equal(t, 0, broker.SubscriberCount())
}

func TestEventBroker_SubscriberCount(t *testing.T) {
	broker := NewEventBroker(10)
	defer broker.Close()

	assert.Equal(t, 0, broker.SubscriberCount())

	broker.Subscribe("test-1")
	assert.Equal(t, 1, broker.SubscriberCount())

	broker.Subscribe("test-2")
	assert.Equal(t, 2, broker.SubscriberCount())

	broker.Unsubscribe("test-1")
	assert.Equal(t, 1, broker.SubscriberCount())

	broker.Unsubscribe("test-2")
	assert.Equal(t, 0, broker.SubscriberCount())
}
