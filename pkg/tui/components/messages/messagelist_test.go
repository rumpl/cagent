package messages

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tui/types"
)

func TestNewMessageList(t *testing.T) {
	ml := NewMessageList(nil)
	require.NotNil(t, ml)
	assert.Equal(t, 0, ml.GetMessageCount())
}

func TestAddMessage(t *testing.T) {
	ml := NewMessageList(nil)

	msg1 := types.User("Hello")
	cmd := ml.AddMessage(msg1)
	assert.Nil(t, cmd)
	assert.Equal(t, 1, ml.GetMessageCount())

	msg2 := types.Agent(types.MessageTypeAssistant, "agent1", "Hi there")
	ml.AddMessage(msg2)
	assert.Equal(t, 2, ml.GetMessageCount())

	messages := ml.GetMessages()
	assert.Len(t, messages, 2)
	assert.Equal(t, msg1, messages[0])
	assert.Equal(t, msg2, messages[1])
}

func TestUpdateMessage(t *testing.T) {
	ml := NewMessageList(nil)

	msg1 := types.User("Hello")
	ml.AddMessage(msg1)

	msg2 := types.User("Updated")
	ml.UpdateMessage(0, msg2)

	messages := ml.GetMessages()
	assert.Equal(t, msg2, messages[0])
}

func TestUpdateMessage_InvalidIndex(t *testing.T) {
	ml := NewMessageList(nil)

	msg := types.User("Hello")
	ml.AddMessage(msg)

	// Should not panic
	ml.UpdateMessage(-1, types.User("Invalid"))
	ml.UpdateMessage(10, types.User("Invalid"))

	// Original message should be unchanged
	messages := ml.GetMessages()
	assert.Equal(t, 1, len(messages))
	assert.Equal(t, msg, messages[0])
}

func TestGetMessages(t *testing.T) {
	ml := NewMessageList(nil)

	msg1 := types.User("Hello")
	msg2 := types.Agent(types.MessageTypeAssistant, "agent1", "Hi")

	ml.AddMessage(msg1)
	ml.AddMessage(msg2)

	messages := ml.GetMessages()
	assert.Len(t, messages, 2)
	assert.Equal(t, msg1, messages[0])
	assert.Equal(t, msg2, messages[1])
}

func TestGetMessageCount(t *testing.T) {
	ml := NewMessageList(nil)

	assert.Equal(t, 0, ml.GetMessageCount())

	ml.AddMessage(types.User("Hello"))
	assert.Equal(t, 1, ml.GetMessageCount())

	ml.AddMessage(types.User("World"))
	assert.Equal(t, 2, ml.GetMessageCount())
}

func TestCreateMessageView(t *testing.T) {
	ml := NewMessageList(nil)

	msg := types.User("Hello")
	view := ml.CreateMessageView(msg, 80)

	assert.NotNil(t, view)
}

func TestRemoveSpinner(t *testing.T) {
	ml := NewMessageList(nil)

	ml.AddMessage(types.User("Hello"))
	ml.AddMessage(types.Spinner())

	assert.Equal(t, 2, ml.GetMessageCount())

	ml.RemoveSpinner()

	assert.Equal(t, 1, ml.GetMessageCount())
	messages := ml.GetMessages()
	assert.Equal(t, types.MessageTypeUser, messages[0].Type)
}

func TestRemoveSpinner_NoSpinner(t *testing.T) {
	ml := NewMessageList(nil)

	ml.AddMessage(types.User("Hello"))
	ml.AddMessage(types.Agent(types.MessageTypeAssistant, "agent1", "Hi"))

	assert.Equal(t, 2, ml.GetMessageCount())

	ml.RemoveSpinner()

	// Should not remove non-spinner messages
	assert.Equal(t, 2, ml.GetMessageCount())
}

func TestRemoveSpinner_EmptyList(t *testing.T) {
	ml := NewMessageList(nil)

	// Should not panic
	ml.RemoveSpinner()

	assert.Equal(t, 0, ml.GetMessageCount())
}

func TestRemovePendingToolCalls(t *testing.T) {
	ml := NewMessageList(nil).(*messageList)

	// Add various messages
	ml.AddMessage(types.User("Hello"))
	ml.AddMessage(types.ToolCallMessage("agent1", mockToolCall(), mockTool(), types.ToolStatusPending))
	ml.AddMessage(types.ToolCallMessage("agent1", mockToolCall(), mockTool(), types.ToolStatusRunning))
	ml.AddMessage(types.ToolCallMessage("agent1", mockToolCall(), mockTool(), types.ToolStatusCompleted))
	ml.AddMessage(types.Agent(types.MessageTypeAssistant, "agent1", "Done"))

	// Create some views to match
	for range ml.messages {
		ml.views = append(ml.views, ml.CreateMessageView(types.User("test"), 80))
	}

	assert.Equal(t, 5, ml.GetMessageCount())

	ml.RemovePendingToolCalls()

	// Should only remove pending and running tool calls
	assert.Equal(t, 3, ml.GetMessageCount())
	messages := ml.GetMessages()
	assert.Equal(t, types.MessageTypeUser, messages[0].Type)
	assert.Equal(t, types.MessageTypeToolCall, messages[1].Type)
	assert.Equal(t, types.ToolStatusCompleted, messages[1].ToolStatus)
	assert.Equal(t, types.MessageTypeAssistant, messages[2].Type)

	// Views should also be updated
	assert.Equal(t, 3, len(ml.views))
}

func TestRemovePendingToolCalls_NoToolCalls(t *testing.T) {
	ml := NewMessageList(nil)

	ml.AddMessage(types.User("Hello"))
	ml.AddMessage(types.Agent(types.MessageTypeAssistant, "agent1", "Hi"))

	assert.Equal(t, 2, ml.GetMessageCount())

	ml.RemovePendingToolCalls()

	// Should not remove regular messages
	assert.Equal(t, 2, ml.GetMessageCount())
}

func TestGetLastMessage(t *testing.T) {
	ml := NewMessageList(nil)

	// Empty list
	assert.Nil(t, ml.GetLastMessage())

	msg1 := types.User("Hello")
	ml.AddMessage(msg1)
	assert.Equal(t, msg1, ml.GetLastMessage())

	msg2 := types.Agent(types.MessageTypeAssistant, "agent1", "Hi")
	ml.AddMessage(msg2)
	assert.Equal(t, msg2, ml.GetLastMessage())
}

func TestGetViews(t *testing.T) {
	ml := NewMessageList(nil).(*messageList)

	ml.AddMessage(types.User("Hello"))
	view := ml.CreateMessageView(types.User("Hello"), 80)
	ml.AddView(view)

	views := ml.GetViews()
	assert.Len(t, views, 1)
}

func TestGetView(t *testing.T) {
	ml := NewMessageList(nil).(*messageList)

	ml.AddMessage(types.User("Hello"))
	view := ml.CreateMessageView(types.User("Hello"), 80)
	ml.AddView(view)

	t.Run("valid index", func(t *testing.T) {
		v := ml.GetView(0)
		assert.NotNil(t, v)
		assert.Equal(t, view, v)
	})

	t.Run("invalid negative index", func(t *testing.T) {
		v := ml.GetView(-1)
		assert.Nil(t, v)
	})

	t.Run("invalid out of bounds index", func(t *testing.T) {
		v := ml.GetView(10)
		assert.Nil(t, v)
	})
}

func TestUpdateView(t *testing.T) {
	ml := NewMessageList(nil).(*messageList)

	msg := types.User("Hello")
	ml.AddMessage(msg)

	cmd := ml.UpdateView(0, 80)
	// Command may be nil if Init() returns nil
	_ = cmd

	// View should be created
	assert.Equal(t, 1, len(ml.views))
}

func TestUpdateView_InvalidIndex(t *testing.T) {
	ml := NewMessageList(nil)

	cmd := ml.UpdateView(-1, 80)
	assert.Nil(t, cmd)

	cmd = ml.UpdateView(10, 80)
	assert.Nil(t, cmd)
}

func TestSetAllViewSizes(t *testing.T) {
	ml := NewMessageList(nil).(*messageList)

	ml.AddMessage(types.User("Hello"))
	view := ml.CreateMessageView(types.User("Hello"), 80)
	ml.AddView(view)

	// Should not panic
	ml.SetAllViewSizes(100)
}

func TestInitAllViews(t *testing.T) {
	ml := NewMessageList(nil).(*messageList)

	ml.AddMessage(types.User("Hello"))
	view := ml.CreateMessageView(types.User("Hello"), 80)
	ml.AddView(view)

	cmd := ml.InitAllViews()
	// Command may be nil or batched
	_ = cmd
}

func TestRenderAllViews(t *testing.T) {
	ml := NewMessageList(nil).(*messageList)

	t.Run("empty list", func(t *testing.T) {
		result := ml.RenderAllViews()
		assert.Empty(t, result)
	})

	t.Run("with messages", func(t *testing.T) {
		ml.AddMessage(types.User("Hello"))
		view := ml.CreateMessageView(types.User("Hello"), 80)
		ml.AddView(view)

		result := ml.RenderAllViews()
		assert.NotEmpty(t, result)
	})
}

// Helper functions for testing
func mockToolCall() tools.ToolCall {
	return tools.ToolCall{
		ID: "test-id",
		Type: "function",
		Function: tools.FunctionCall{
			Name:      "test-function",
			Arguments: "{}",
		},
	}
}

func mockTool() tools.Tool {
	return tools.Tool{
		Name:        "test-tool",
		Description: "test description",
	}
}
