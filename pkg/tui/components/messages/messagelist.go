package messages

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/tui/components/markdown"
	"github.com/docker/cagent/pkg/tui/components/message"
	"github.com/docker/cagent/pkg/tui/components/tool"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/types"
)

// MessageList manages a collection of messages and their views
type MessageList interface {
	// Message operations
	AddMessage(msg *types.Message) tea.Cmd
	UpdateMessage(index int, msg *types.Message)
	GetMessages() []*types.Message
	GetMessageCount() int

	// View management
	GetViews() []layout.Model
	GetView(index int) layout.Model
	CreateMessageView(msg *types.Message, width int) layout.Model
	CreateToolCallView(msg *types.Message, width int) layout.Model
	UpdateView(index, width int) tea.Cmd
	AddView(view layout.Model)

	// Special operations
	RemoveSpinner()
	RemovePendingToolCalls()

	// State queries
	GetLastMessage() *types.Message

	// Batch operations
	SetAllViewSizes(width int)
	InitAllViews() tea.Cmd
	UpdateAllViews(msg tea.Msg) tea.Cmd
	RenderAllViews() string
}

// messageList implements MessageList
type messageList struct {
	messages     []*types.Message
	views        []layout.Model
	sessionState *service.SessionState
}

// NewMessageList creates a new message list
func NewMessageList(sessionState *service.SessionState) MessageList {
	return &messageList{
		sessionState: sessionState,
	}
}

// AddMessage adds a new message and creates its view
func (ml *messageList) AddMessage(msg *types.Message) tea.Cmd {
	ml.messages = append(ml.messages, msg)
	return nil
}

// UpdateMessage updates a message at the given index
func (ml *messageList) UpdateMessage(index int, msg *types.Message) {
	if index >= 0 && index < len(ml.messages) {
		ml.messages[index] = msg
	}
}

// GetMessages returns all messages
func (ml *messageList) GetMessages() []*types.Message {
	return ml.messages
}

// GetMessageCount returns the number of messages
func (ml *messageList) GetMessageCount() int {
	return len(ml.messages)
}

// GetViews returns all views
func (ml *messageList) GetViews() []layout.Model {
	return ml.views
}

// GetView returns the view at the given index
func (ml *messageList) GetView(index int) layout.Model {
	if index >= 0 && index < len(ml.views) {
		return ml.views[index]
	}
	return nil
}

// CreateMessageView creates a view for a regular message
func (ml *messageList) CreateMessageView(msg *types.Message, width int) layout.Model {
	view := message.New(msg)
	view.SetSize(width, 0)
	return view
}

// CreateToolCallView creates a view for a tool call message
func (ml *messageList) CreateToolCallView(msg *types.Message, width int) layout.Model {
	// -4 because of the padding in the tool calls
	view := tool.New(msg, markdown.NewRenderer(width-4), ml.sessionState)
	view.SetSize(width, 0)
	return view
}

// UpdateView updates the view at the given index
func (ml *messageList) UpdateView(index, width int) tea.Cmd {
	if index < 0 || index >= len(ml.messages) {
		return nil
	}

	msg := ml.messages[index]
	var view layout.Model

	if msg.Type == types.MessageTypeToolCall || msg.Type == types.MessageTypeToolResult {
		view = ml.CreateToolCallView(msg, width)
	} else {
		view = ml.CreateMessageView(msg, width)
	}

	// Update views slice to match
	if index >= len(ml.views) {
		ml.views = append(ml.views, view)
	} else {
		ml.views[index] = view
	}

	return view.Init()
}

// AddView adds a view to the views list
func (ml *messageList) AddView(view layout.Model) {
	ml.views = append(ml.views, view)
}

// RemoveSpinner removes the last message if it's a spinner
func (ml *messageList) RemoveSpinner() {
	if len(ml.messages) > 0 {
		lastIdx := len(ml.messages) - 1
		lastMessage := ml.messages[lastIdx]

		if lastMessage.Type == types.MessageTypeSpinner {
			ml.messages = ml.messages[:lastIdx]
			if len(ml.views) > lastIdx {
				ml.views = ml.views[:lastIdx]
			}
		}
	}
}

// RemovePendingToolCalls removes any tool call messages that are in pending or running state
func (ml *messageList) RemovePendingToolCalls() {
	var newMessages []*types.Message
	var newViews []layout.Model

	for i, msg := range ml.messages {
		shouldRemove := msg.Type == types.MessageTypeToolCall &&
			(msg.ToolStatus == types.ToolStatusPending || msg.ToolStatus == types.ToolStatusRunning)

		if !shouldRemove {
			newMessages = append(newMessages, msg)
			if i < len(ml.views) {
				newViews = append(newViews, ml.views[i])
			}
		}
	}

	ml.messages = newMessages
	ml.views = newViews
}

// GetLastMessage returns the last message, or nil if there are no messages
func (ml *messageList) GetLastMessage() *types.Message {
	if len(ml.messages) == 0 {
		return nil
	}
	return ml.messages[len(ml.messages)-1]
}

// SetAllViewSizes updates the size of all views
func (ml *messageList) SetAllViewSizes(width int) {
	for _, view := range ml.views {
		view.SetSize(width, 0)
	}
}

// InitAllViews initializes all views and returns batched commands
func (ml *messageList) InitAllViews() tea.Cmd {
	var cmds []tea.Cmd
	for _, view := range ml.views {
		if cmd := view.Init(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

// UpdateAllViews forwards a message to all views and returns batched commands
func (ml *messageList) UpdateAllViews(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	for i, view := range ml.views {
		updatedView, cmd := view.Update(msg)
		ml.views[i] = updatedView
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

// RenderAllViews renders all views into a single string with separators
func (ml *messageList) RenderAllViews() string {
	if len(ml.views) == 0 {
		return ""
	}

	var allLines []string
	for i, view := range ml.views {
		rendered := view.View()
		if rendered != "" {
			allLines = append(allLines, rendered)
		}

		// Add separator between messages (but not after last message)
		if i < len(ml.views)-1 && rendered != "" {
			allLines = append(allLines, "")
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, allLines...)
}
