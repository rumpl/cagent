package session

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/tools"
)

func TestPendingToolCalls_TrailingAssistantToolCall(t *testing.T) {
	tc := tools.ToolCall{ID: "call-1", Function: tools.FunctionCall{Name: "shell", Arguments: "{}"}}
	toolDef := tools.Tool{Name: "shell", Description: "Run a command"}
	sess := New(WithMessages([]Item{
		NewMessageItem(UserMessage("hello")),
		NewMessageItem(NewAgentMessage("root", &chat.Message{
			Role:            chat.MessageRoleAssistant,
			ToolCalls:       []tools.ToolCall{tc},
			ToolDefinitions: []tools.Tool{toolDef},
		})),
	}))

	pending := sess.PendingToolCalls()
	require.Len(t, pending, 1)
	assert.Equal(t, tc, pending[0].ToolCall)
	assert.Equal(t, toolDef, pending[0].ToolDefinition)
	assert.True(t, sess.HasPendingToolCalls())
}

func TestPendingToolCalls_PartiallySatisfiedBatch(t *testing.T) {
	first := tools.ToolCall{ID: "call-1", Function: tools.FunctionCall{Name: "first", Arguments: "{}"}}
	second := tools.ToolCall{ID: "call-2", Function: tools.FunctionCall{Name: "second", Arguments: "{}"}}
	sess := New(WithMessages([]Item{
		NewMessageItem(UserMessage("hello")),
		NewMessageItem(NewAgentMessage("root", &chat.Message{
			Role:      chat.MessageRoleAssistant,
			ToolCalls: []tools.ToolCall{first, second},
		})),
		NewMessageItem(NewAgentMessage("root", &chat.Message{
			Role:       chat.MessageRoleTool,
			ToolCallID: first.ID,
			Content:    "ok",
		})),
	}))

	pending := sess.PendingToolCalls()
	require.Len(t, pending, 1)
	assert.Equal(t, second, pending[0].ToolCall)
}

func TestPendingToolCalls_NotPendingAfterBoundaryOrResult(t *testing.T) {
	tc := tools.ToolCall{ID: "call-1", Function: tools.FunctionCall{Name: "shell", Arguments: "{}"}}

	tests := []struct {
		name  string
		items []Item
	}{
		{
			name: "result provided",
			items: []Item{
				NewMessageItem(UserMessage("hello")),
				NewMessageItem(NewAgentMessage("root", &chat.Message{Role: chat.MessageRoleAssistant, ToolCalls: []tools.ToolCall{tc}})),
				NewMessageItem(NewAgentMessage("root", &chat.Message{Role: chat.MessageRoleTool, ToolCallID: tc.ID, Content: "ok"})),
			},
		},
		{
			name: "new user message supersedes pending call",
			items: []Item{
				NewMessageItem(UserMessage("hello")),
				NewMessageItem(NewAgentMessage("root", &chat.Message{Role: chat.MessageRoleAssistant, ToolCalls: []tools.ToolCall{tc}})),
				NewMessageItem(UserMessage("never mind")),
			},
		},
		{
			name: "new assistant message supersedes pending call",
			items: []Item{
				NewMessageItem(UserMessage("hello")),
				NewMessageItem(NewAgentMessage("root", &chat.Message{Role: chat.MessageRoleAssistant, ToolCalls: []tools.ToolCall{tc}})),
				NewMessageItem(NewAgentMessage("root", &chat.Message{Role: chat.MessageRoleAssistant, Content: "done"})),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess := New(WithMessages(tt.items))
			assert.Empty(t, sess.PendingToolCalls())
			assert.False(t, sess.HasPendingToolCalls())
		})
	}
}
