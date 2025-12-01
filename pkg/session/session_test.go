package session

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/tools"
)

func TestTrimMessages(t *testing.T) {
	messages := make([]chat.Message, maxMessages+10)

	// Fill with some basic messages
	for i := range messages {
		messages[i] = chat.Message{
			Role:    chat.MessageRoleUser,
			Content: "message",
		}
	}

	result := trimMessages(messages, maxMessages)
	assert.Len(t, result, maxMessages, "should trim to maxMessages")
}

func TestTrimMessagesWithToolCalls(t *testing.T) {
	messages := []chat.Message{
		{
			Role:    chat.MessageRoleUser,
			Content: "first message",
		},
		{
			Role:    chat.MessageRoleAssistant,
			Content: "response with tool",
			ToolCalls: []tools.ToolCall{
				{
					ID: "tool1",
				},
			},
		},
		{
			Role:       chat.MessageRoleTool,
			Content:    "tool result",
			ToolCallID: "tool1",
		},
		{
			Role:    chat.MessageRoleUser,
			Content: "second message",
		},
		{
			Role:    chat.MessageRoleAssistant,
			Content: "another response",
			ToolCalls: []tools.ToolCall{
				{
					ID: "tool2",
				},
			},
		},
		{
			Role:       chat.MessageRoleTool,
			Content:    "tool result 2",
			ToolCallID: "tool2",
		},
	}

	// Use 3 as the limit to force trimming
	maxItems := 3

	result := trimMessages(messages, maxItems)

	// Should keep last 3 messages, but ensure tool call consistency
	assert.Len(t, result, maxItems)

	toolCalls := make(map[string]bool)
	for _, msg := range result {
		if msg.Role == chat.MessageRoleAssistant {
			for _, tool := range msg.ToolCalls {
				toolCalls[tool.ID] = true
			}
		}
		if msg.Role == chat.MessageRoleTool {
			assert.True(t, toolCalls[msg.ToolCallID], "tool result should have corresponding assistant message")
		}
	}
}

func TestGetMessages(t *testing.T) {
	testAgent := &agent.Agent{}

	s := New()

	for range maxMessages + 10 {
		s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
			Role:    chat.MessageRoleUser,
			Content: "test message",
		}))
	}

	messages := s.GetMessages(testAgent)

	// Count non-system messages (since system messages are not limited)
	nonSystemCount := 0
	for _, msg := range messages {
		if msg.Role != chat.MessageRoleSystem {
			nonSystemCount++
		}
	}

	assert.LessOrEqual(t, nonSystemCount, maxMessages, "non-system messages should not exceed maxMessages")
}

func TestGetMessagesWithToolCalls(t *testing.T) {
	testAgent := &agent.Agent{}

	s := New()

	s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleUser,
		Content: "test message",
	}))

	s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleAssistant,
		Content: "using tool",
		ToolCalls: []tools.ToolCall{
			{
				ID: "test-tool",
			},
		},
	}))

	s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
		Role:       chat.MessageRoleTool,
		ToolCallID: "test-tool",
		Content:    "tool result",
	}))

	oldMax := maxMessages
	maxMessages = 2
	defer func() { maxMessages = oldMax }()

	messages := s.GetMessages(testAgent)

	toolCalls := make(map[string]bool)
	for _, msg := range messages {
		if msg.Role == chat.MessageRoleAssistant {
			for _, tool := range msg.ToolCalls {
				toolCalls[tool.ID] = true
			}
		}
		if msg.Role == chat.MessageRoleTool {
			assert.True(t, toolCalls[msg.ToolCallID], "tool result should have corresponding assistant message")
		}
	}
}

func TestClearOldToolResults(t *testing.T) {
	// Create a string that's about 40k tokens (40k * 4 = 160k characters)
	largeContent := make([]byte, 160001)
	for i := range largeContent {
		largeContent[i] = 'x'
	}

	tests := []struct {
		name     string
		messages []chat.Message
		want     func(t *testing.T, result []chat.Message)
	}{
		{
			name: "no clearing when under threshold",
			messages: []chat.Message{
				{Role: chat.MessageRoleUser, Content: "hello"},
				{Role: chat.MessageRoleAssistant, Content: "hi"},
				{Role: chat.MessageRoleTool, ToolCallID: "1", Content: "tool result"},
			},
			want: func(t *testing.T, result []chat.Message) {
				t.Helper()
				assert.Equal(t, "tool result", result[2].Content)
			},
		},
		{
			name: "clear old tool results when over threshold",
			messages: []chat.Message{
				{Role: chat.MessageRoleTool, ToolCallID: "1", Content: "old tool result"},
				{Role: chat.MessageRoleUser, Content: string(largeContent)}, // This pushes old content past threshold
				{Role: chat.MessageRoleTool, ToolCallID: "2", Content: "recent tool result"},
			},
			want: func(t *testing.T, result []chat.Message) {
				t.Helper()
				assert.Equal(t, "[Content cleared]", result[0].Content)
				assert.Equal(t, "recent tool result", result[2].Content)
			},
		},
		{
			name: "preserve non-tool messages",
			messages: []chat.Message{
				{Role: chat.MessageRoleUser, Content: "old user message"},
				{Role: chat.MessageRoleAssistant, Content: "old assistant message"},
				{Role: chat.MessageRoleTool, ToolCallID: "1", Content: "old tool result"},
				{Role: chat.MessageRoleUser, Content: string(largeContent)},
			},
			want: func(t *testing.T, result []chat.Message) {
				t.Helper()
				assert.Equal(t, "old user message", result[0].Content)
				assert.Equal(t, "old assistant message", result[1].Content)
				assert.Equal(t, "[Content cleared]", result[2].Content) // Tool result cleared
				assert.Equal(t, string(largeContent), result[3].Content)
			},
		},
		{
			name:     "empty messages",
			messages: []chat.Message{},
			want: func(t *testing.T, result []chat.Message) {
				t.Helper()
				assert.Empty(t, result)
			},
		},
		{
			name: "preserve tool call ID when clearing",
			messages: []chat.Message{
				{Role: chat.MessageRoleTool, ToolCallID: "my-tool-id", Content: "old result"},
				{Role: chat.MessageRoleUser, Content: string(largeContent)},
			},
			want: func(t *testing.T, result []chat.Message) {
				t.Helper()
				assert.Equal(t, "[Content cleared]", result[0].Content)
				assert.Equal(t, "my-tool-id", result[0].ToolCallID) // ID preserved
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := clearOldToolResults(tt.messages)
			tt.want(t, result)
		})
	}
}

func TestGetMessagesWithSummary(t *testing.T) {
	testAgent := &agent.Agent{}

	s := New()

	s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleUser,
		Content: "first message",
	}))
	s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleAssistant,
		Content: "first response",
	}))

	s.Messages = append(s.Messages, Item{Summary: "This is a summary of the conversation so far"})

	s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleUser,
		Content: "message after summary",
	}))
	s.AddMessage(NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleAssistant,
		Content: "response after summary",
	}))

	messages := s.GetMessages(testAgent)

	// Count non-system messages (user and assistant only)
	userAssistantMessages := 0
	summaryFound := false
	for _, msg := range messages {
		if msg.Role == chat.MessageRoleUser || msg.Role == chat.MessageRoleAssistant {
			userAssistantMessages++
		}
		if msg.Role == chat.MessageRoleSystem && msg.Content == "Session Summary: This is a summary of the conversation so far" {
			summaryFound = true
		}
	}

	// We should have:
	// - 1 summary system message
	// - 2 messages after the summary (user + assistant)
	// - Various other system messages from agent setup
	assert.True(t, summaryFound, "should include summary as system message")
	assert.Equal(t, 2, userAssistantMessages, "should only include messages after summary")
}
