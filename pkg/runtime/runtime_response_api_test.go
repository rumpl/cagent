package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/session"
)

// TestResponseAPIToolCallHandling verifies that tool calls from the Response API
// are correctly accumulated even when they arrive with the same ID in multiple chunks
func TestResponseAPIToolCallHandling(t *testing.T) {
	// Simulate how the Response API sends tool calls:
	// 1. First event with ID and name
	// 2. Multiple events with the same ID adding arguments incrementally
	stream := newStreamBuilder().
		AddToolCallName("call_abc", "search").
		AddToolCallArguments("call_abc", `{"que`).
		AddToolCallArguments("call_abc", `ry": "te`).
		AddToolCallArguments("call_abc", `st"}`).
		AddStopWithUsage(10, 15).
		Build()

	sess := session.New(session.WithUserMessage("Search for test"))

	events := runSession(t, sess, stream)

	// Verify that we got the expected partial tool call events
	require.True(t, hasEventType(t, events, &PartialToolCallEvent{}), "Expected PartialToolCallEvent")

	// Verify the session has the complete tool call with full arguments
	messages := sess.GetAllMessages()
	var foundToolCall bool
	for _, msg := range messages {
		if msg.Message.Role == "assistant" && len(msg.Message.ToolCalls) > 0 {
			foundToolCall = true
			require.Equal(t, "call_abc", msg.Message.ToolCalls[0].ID)
			require.Equal(t, "search", msg.Message.ToolCalls[0].Function.Name)
			require.JSONEq(t, `{"query": "test"}`, msg.Message.ToolCalls[0].Function.Arguments)
		}
	}
	require.True(t, foundToolCall, "Expected to find complete tool call in session messages")
}

// TestResponseAPIMultipleToolCalls verifies that multiple tool calls
// from the Response API are correctly tracked independently
func TestResponseAPIMultipleToolCalls(t *testing.T) {
	// Simulate multiple tool calls with interleaved arguments
	stream := newStreamBuilder().
		AddToolCallName("call_1", "search").
		AddToolCallName("call_2", "calculate").
		AddToolCallArguments("call_1", `{"query": "test1"}`).
		AddToolCallArguments("call_2", `{"expression": "2+2"}`).
		AddStopWithUsage(20, 30).
		Build()

	sess := session.New(session.WithUserMessage("Search and calculate"))

	events := runSession(t, sess, stream)

	// Verify that we got partial tool call events
	require.True(t, hasEventType(t, events, &PartialToolCallEvent{}), "Expected PartialToolCallEvent")

	// Verify the session has both complete tool calls
	messages := sess.GetAllMessages()
	t.Logf("Total messages: %d", len(messages))
	for i, msg := range messages {
		t.Logf("Message %d: role=%s, toolCalls=%d", i, msg.Message.Role, len(msg.Message.ToolCalls))
	}
	var toolCalls []string
	for _, msg := range messages {
		if msg.Message.Role == "assistant" && len(msg.Message.ToolCalls) > 0 {
			for _, tc := range msg.Message.ToolCalls {
				toolCalls = append(toolCalls, tc.Function.Name)
			}
		}
	}
	require.ElementsMatch(t, []string{"search", "calculate"}, toolCalls, "Expected both tool calls")
}
