package messages

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/types"
)

// mockView implements layout.Model for testing
type mockView struct {
	content string
}

func (m *mockView) Init() tea.Cmd {
	return nil
}

func (m *mockView) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	return m, nil
}

func (m *mockView) View() string {
	return m.content
}

func (m *mockView) SetSize(width, height int) tea.Cmd {
	return nil
}

func TestNewRenderCache(t *testing.T) {
	rc := NewRenderCache()
	require.NotNil(t, rc)
	assert.Empty(t, rc.GetContent())
	assert.Equal(t, 0, rc.GetTotalHeight())
}

func TestInvalidate(t *testing.T) {
	rc := NewRenderCache().(*renderCache)

	// Add an item to cache
	rc.renderedItems[0] = renderedItem{view: "test", height: 1}

	rc.Invalidate(0)

	_, exists := rc.renderedItems[0]
	assert.False(t, exists)
}

func TestInvalidateAll(t *testing.T) {
	rc := NewRenderCache().(*renderCache)

	// Add items to cache
	rc.renderedItems[0] = renderedItem{view: "test1", height: 1}
	rc.renderedItems[1] = renderedItem{view: "test2", height: 2}
	rc.rendered = "test content"
	rc.totalHeight = 10

	rc.InvalidateAll()

	assert.Empty(t, rc.renderedItems)
	assert.Empty(t, rc.rendered)
	assert.Equal(t, 0, rc.totalHeight)
}

func TestShouldCache(t *testing.T) {
	rc := NewRenderCache()

	tests := []struct {
		name     string
		msg      *types.Message
		expected bool
	}{
		{
			name:     "nil message",
			msg:      nil,
			expected: false,
		},
		{
			name:     "user message",
			msg:      types.User("Hello"),
			expected: true,
		},
		{
			name:     "welcome message",
			msg:      types.Welcome("Welcome!"),
			expected: true,
		},
		{
			name:     "cancelled message",
			msg:      types.Cancelled(),
			expected: true,
		},
		{
			name:     "error message",
			msg:      types.Error("Error!"),
			expected: true,
		},
		{
			name:     "shell output message",
			msg:      types.ShellOutput("output"),
			expected: true,
		},
		{
			name:     "assistant message with content",
			msg:      types.Agent(types.MessageTypeAssistant, "agent1", "Hello"),
			expected: true,
		},
		{
			name:     "assistant message without content",
			msg:      types.Agent(types.MessageTypeAssistant, "agent1", ""),
			expected: false,
		},
		{
			name:     "assistant message with only whitespace",
			msg:      types.Agent(types.MessageTypeAssistant, "agent1", "  \n\t  "),
			expected: false,
		},
		{
			name:     "spinner message",
			msg:      types.Spinner(),
			expected: false,
		},
		{
			name: "tool call pending",
			msg: types.ToolCallMessage("agent1", tools.ToolCall{ID: "1"},
				tools.Tool{}, types.ToolStatusPending),
			expected: false,
		},
		{
			name: "tool call running",
			msg: types.ToolCallMessage("agent1", tools.ToolCall{ID: "1"},
				tools.Tool{}, types.ToolStatusRunning),
			expected: false,
		},
		{
			name: "tool call completed",
			msg: types.ToolCallMessage("agent1", tools.ToolCall{ID: "1"},
				tools.Tool{}, types.ToolStatusCompleted),
			expected: true,
		},
		{
			name: "tool call error",
			msg: types.ToolCallMessage("agent1", tools.ToolCall{ID: "1"},
				tools.Tool{}, types.ToolStatusError),
			expected: true,
		},
		{
			name: "tool call confirmation",
			msg: types.ToolCallMessage("agent1", tools.ToolCall{ID: "1"},
				tools.Tool{}, types.ToolStatusConfirmation),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rc.ShouldCache(tt.msg)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRenderItem_WithCaching(t *testing.T) {
	rc := NewRenderCache().(*renderCache)

	msg := types.User("Hello")
	view := &mockView{content: "Rendered: Hello"}

	// First render - should not be cached yet
	item1 := rc.RenderItem(0, msg, view)
	assert.Equal(t, "Rendered: Hello", item1.view)
	assert.Equal(t, 1, item1.height)

	// Verify it was cached
	cached, exists := rc.renderedItems[0]
	assert.True(t, exists)
	assert.Equal(t, item1, cached)

	// Second render - should return cached version
	view.content = "Changed"
	item2 := rc.RenderItem(0, msg, view)
	assert.Equal(t, "Rendered: Hello", item2.view) // Still old content
	assert.Equal(t, item1, item2)
}

func TestRenderItem_WithoutCaching(t *testing.T) {
	rc := NewRenderCache()

	msg := types.Spinner() // Spinners should not be cached
	view := &mockView{content: "Spinner frame 1"}

	// First render
	item1 := rc.RenderItem(0, msg, view)
	assert.Equal(t, "Spinner frame 1", item1.view)

	// Second render with updated content
	view.content = "Spinner frame 2"
	item2 := rc.RenderItem(0, msg, view)
	assert.Equal(t, "Spinner frame 2", item2.view)

	// Should not be cached
	rcImpl := rc.(*renderCache)
	_, exists := rcImpl.renderedItems[0]
	assert.False(t, exists)
}

func TestRenderItem_MultilineContent(t *testing.T) {
	rc := NewRenderCache()

	msg := types.User("Hello")
	view := &mockView{content: "Line 1\nLine 2\nLine 3"}

	item := rc.RenderItem(0, msg, view)
	assert.Equal(t, "Line 1\nLine 2\nLine 3", item.view)
	assert.Equal(t, 3, item.height)
}

func TestRenderItem_EmptyContent(t *testing.T) {
	rc := NewRenderCache()

	msg := types.User("Hello")
	view := &mockView{content: ""}

	item := rc.RenderItem(0, msg, view)
	assert.Empty(t, item.view)
	assert.Equal(t, 0, item.height)
}

func TestRenderAll_Empty(t *testing.T) {
	rc := NewRenderCache()

	result := rc.RenderAll(nil, nil)
	assert.Empty(t, result)
	assert.Equal(t, 0, rc.GetTotalHeight())
}

func TestRenderAll_SingleMessage(t *testing.T) {
	rc := NewRenderCache()

	messages := []*types.Message{types.User("Hello")}
	views := []layout.Model{&mockView{content: "User: Hello"}}

	result := rc.RenderAll(messages, views)
	assert.Equal(t, "User: Hello", result)
	assert.Equal(t, 1, rc.GetTotalHeight())
}

func TestRenderAll_MultipleMessages(t *testing.T) {
	rc := NewRenderCache()

	messages := []*types.Message{
		types.User("Hello"),
		types.Agent(types.MessageTypeAssistant, "agent1", "Hi there"),
	}
	views := []layout.Model{
		&mockView{content: "User: Hello"},
		&mockView{content: "Agent: Hi there"},
	}

	result := rc.RenderAll(messages, views)
	expected := "User: Hello\n\nAgent: Hi there"
	assert.Equal(t, expected, result)
	assert.Equal(t, 3, rc.GetTotalHeight()) // 2 content lines + 1 separator
}

func TestRenderAll_WithMultilineMessages(t *testing.T) {
	rc := NewRenderCache()

	messages := []*types.Message{
		types.User("Hello"),
		types.Agent(types.MessageTypeAssistant, "agent1", "Response"),
	}
	views := []layout.Model{
		&mockView{content: "User:\nHello\nWorld"},
		&mockView{content: "Agent:\nResponse"},
	}

	result := rc.RenderAll(messages, views)
	expected := "User:\nHello\nWorld\n\nAgent:\nResponse"
	assert.Equal(t, expected, result)
	assert.Equal(t, 6, rc.GetTotalHeight()) // 3 + 1 separator + 2
}

func TestRenderAll_UpdatesCacheState(t *testing.T) {
	rc := NewRenderCache()

	messages := []*types.Message{types.User("Hello")}
	views := []layout.Model{&mockView{content: "Content"}}

	result := rc.RenderAll(messages, views)

	assert.Equal(t, result, rc.GetContent())
	assert.Positive(t, rc.GetTotalHeight())
}

func TestRenderAll_MismatchedMessageCount(t *testing.T) {
	rc := NewRenderCache()

	// More views than messages (shouldn't panic)
	messages := []*types.Message{types.User("Hello")}
	views := []layout.Model{
		&mockView{content: "View 1"},
		&mockView{content: "View 2"},
	}

	result := rc.RenderAll(messages, views)
	assert.NotEmpty(t, result)
}

func TestGetContent(t *testing.T) {
	rc := NewRenderCache().(*renderCache)

	rc.rendered = "test content"
	assert.Equal(t, "test content", rc.GetContent())
}

func TestGetTotalHeight(t *testing.T) {
	rc := NewRenderCache().(*renderCache)

	rc.totalHeight = 42
	assert.Equal(t, 42, rc.GetTotalHeight())
}
