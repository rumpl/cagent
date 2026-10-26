package eventmerge

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/runtime/contract"
	"github.com/docker/docker-agent/pkg/tools"
)

const window = 50 * time.Millisecond

func TestCoalesceDisabledReturnsInput(t *testing.T) {
	t.Parallel()

	in := make(chan contract.Event)
	var recv <-chan contract.Event = in

	assert.Equal(t, recv, Coalesce(t.Context(), in, 0))
}

func TestCoalesceForwardsFirstDeltaImmediately(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		in := make(chan contract.Event, 8)
		out := Coalesce(t.Context(), in, window)

		first := text("Hel")
		in <- first
		assert.Same(t, first, receive(t, out))
		close(in)
		assertClosed(t, out)
	})
}

func TestCoalesceMergesDeltasWithinWindow(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		in := make(chan contract.Event, 8)
		out := Coalesce(t.Context(), in, window)

		in <- text("a")
		receive(t, out)

		in <- text("b")
		in <- text("c")
		advance(window - time.Millisecond)
		assertNothing(t, out)

		advance(time.Millisecond)
		assert.Equal(t, "bc", receive(t, out).(*contract.AgentChoiceEvent).Content)

		in <- text("d")
		assertNothing(t, out)
		advance(window)
		assert.Equal(t, "d", receive(t, out).(*contract.AgentChoiceEvent).Content)

		advance(window)
		in <- text("e")
		assert.Equal(t, "e", receive(t, out).(*contract.AgentChoiceEvent).Content, "a delta after a quiet spell is not held back")

		close(in)
		assertClosed(t, out)
	})
}

func TestCoalesceFlushesBeforeOtherEvents(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		in := make(chan contract.Event, 8)
		out := Coalesce(t.Context(), in, window)

		in <- text("a")
		receive(t, out)

		stopped := contract.StreamStopped("s", "root", "normal")
		in <- text("b")
		in <- text("c")
		in <- stopped
		assert.Equal(t, "bc", receive(t, out).(*contract.AgentChoiceEvent).Content)
		assert.Same(t, stopped, receive(t, out))
		close(in)
		assertClosed(t, out)
	})
}

func TestCoalesceKeepsParallelToolCallsApart(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		in := make(chan contract.Event, 8)
		out := Coalesce(t.Context(), in, window)

		in <- contract.ToolCallOutput("call-1", noTool, "1a", "root")
		receive(t, out)

		in <- contract.ToolCallOutput("call-1", noTool, "1b", "root")
		in <- contract.ToolCallOutput("call-2", noTool, "2a", "root")
		in <- contract.ToolCallOutput("call-2", noTool, "2b", "root")
		close(in)
		first := receive(t, out).(*contract.ToolCallOutputEvent)
		second := receive(t, out).(*contract.ToolCallOutputEvent)
		assert.Equal(t, "call-1", first.ToolCallID)
		assert.Equal(t, "1b", first.Output)
		assert.Equal(t, "call-2", second.ToolCallID)
		assert.Equal(t, "2a2b", second.Output)
		assertClosed(t, out)
	})
}

func TestCoalesceFlushesPendingOnClose(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		in := make(chan contract.Event, 8)
		out := Coalesce(t.Context(), in, window)

		in <- text("a")
		receive(t, out)

		in <- text("b")
		in <- text("c")
		close(in)
		assert.Equal(t, "bc", receive(t, out).(*contract.AgentChoiceEvent).Content)
		assertClosed(t, out)
	})
}

func TestCoalesceFlushesEarlyWhenPendingIsLarge(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		in := make(chan contract.Event, 8)
		out := Coalesce(t.Context(), in, window)

		in <- text("a")
		receive(t, out)

		half := strings.Repeat("x", maxPendingBytes/2)
		in <- text(half)
		assertNothing(t, out)

		in <- text(half)
		assert.Len(t, receive(t, out).(*contract.AgentChoiceEvent).Content, maxPendingBytes)
		close(in)
		assertClosed(t, out)
	})
}

func TestCoalesceDrainsInputOnceContextIsDone(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		in := make(chan contract.Event)
		out := Coalesce(ctx, in, window)

		in <- text("never read")
		cancel()

		// With nobody reading out, the producer must still be able to finish.
		for range 3 {
			in <- text("dropped")
		}
		in <- contract.StreamStopped("s", "root", "canceled")
		close(in)
		assertClosed(t, out)
	})
}

func TestCoalescePreservesContentAndOrder(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		script := []contract.Event{
			contract.StreamStarted("s", "root"),
			contract.AgentChoiceReasoning("root", "s", "think ", "m1"),
			contract.AgentChoiceReasoning("root", "s", "hard", "m1"),
			contract.AgentChoice("root", "s", "Hello ", "m1"),
			contract.AgentChoice("root", "s", "world", "m1"),
			contract.ToolCall(tools.ToolCall{ID: "call-1"}, noTool, "root"),
			contract.ToolCallOutput("call-1", noTool, "out ", "root"),
			contract.ToolCallOutput("call-1", noTool, "put", "root"),
			contract.AgentChoice("root", "s", "Done", "m2"),
			contract.AgentChoice("root", "s", ".", "m2"),
			contract.StreamStopped("s", "root", "normal"),
		}
		in := make(chan contract.Event, len(script))
		out := Coalesce(t.Context(), in, window)

		var got []contract.Event
		done := make(chan struct{})
		go func() {
			defer close(done)
			for event := range out {
				got = append(got, event)
			}
		}()
		for _, event := range script {
			in <- event
			advance(10 * time.Millisecond)
		}
		close(in)
		<-done

		assert.Equal(t, transcript(script), transcript(got))
		assert.Less(t, len(got), len(script))
	})
}

var noTool = tools.Tool{Name: "shell"}

// advance moves the synctest bubble's fake clock forward.
func advance(d time.Duration) {
	time.Sleep(d) //nolint:forbidigo // synctest bubble: advances fake time
}

func text(content string) contract.Event {
	return contract.AgentChoice("root", "s", content)
}

func receive(t *testing.T, out <-chan contract.Event) contract.Event {
	t.Helper()
	synctest.Wait()
	select {
	case event, ok := <-out:
		require.True(t, ok, "output closed early")
		return event
	default:
		require.FailNow(t, "expected an event")
		return nil
	}
}

func assertNothing(t *testing.T, out <-chan contract.Event) {
	t.Helper()
	synctest.Wait()
	select {
	case event := <-out:
		assert.Failf(t, "unexpected event", "%#v", event)
	default:
	}
}

func assertClosed(t *testing.T, out <-chan contract.Event) {
	t.Helper()
	synctest.Wait()
	select {
	case event, ok := <-out:
		assert.False(t, ok, "expected output to be closed, got %#v", event)
	default:
		assert.Fail(t, "expected output to be closed")
	}
}

// transcript renders a stream as type-tagged text, joining consecutive
// deltas of the same kind and target, so a stream and its merged form
// render identically.
func transcript(events []contract.Event) []string {
	var lines []string
	var last string
	for _, event := range events {
		var key, content string
		switch ev := event.(type) {
		case *contract.AgentChoiceEvent:
			key, content = "text "+ev.MessageID, ev.Content
		case *contract.AgentChoiceReasoningEvent:
			key, content = "reasoning "+ev.MessageID, ev.Content
		case *contract.ToolCallOutputEvent:
			key, content = "output "+ev.ToolCallID, ev.Output
		case *contract.PartialToolCallEvent:
			key, content = "args "+ev.ToolCall.ID, ev.ToolCall.Function.Arguments
		default:
			lines = append(lines, fmt.Sprintf("%T", event))
			last = ""
			continue
		}
		if key == last {
			lines[len(lines)-1] += content
			continue
		}
		lines = append(lines, key+": "+content)
		last = key
	}
	return lines
}
