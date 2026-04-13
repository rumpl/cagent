package session

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/tools"
)

func TestBuildPromptContext_ComposesNamedSections(t *testing.T) {
	items := []Item{
		NewMessageItem(&Message{Message: chat.Message{Role: chat.MessageRoleUser, Content: "m1"}}),
		NewMessageItem(&Message{Message: chat.Message{Role: chat.MessageRoleAssistant, Content: "m2"}}),
		NewMessageItem(&Message{Message: chat.Message{Role: chat.MessageRoleUser, Content: "m3"}}),
		NewMessageItem(&Message{Message: chat.Message{Role: chat.MessageRoleAssistant, Content: "m4"}}),
		{Summary: "summarized earlier context", FirstKeptEntry: 3},
	}

	sess := New(WithMessages(items))
	a := agent.New("root", "base instruction")

	ctx := sess.BuildPromptContext(a)
	require.NotNil(t, ctx)

	require.Len(t, ctx.InvariantSystemMessages, 1)
	assert.Equal(t, chat.MessageRoleSystem, ctx.InvariantSystemMessages[0].Role)
	assert.Equal(t, "base instruction", ctx.InvariantSystemMessages[0].Content)
	assert.Empty(t, ctx.ContextSystemMessages)

	require.Len(t, ctx.SummaryMessages, 1)
	assert.Equal(t, chat.MessageRoleUser, ctx.SummaryMessages[0].Role)
	assert.Contains(t, ctx.SummaryMessages[0].Content, "Session Summary: summarized earlier context")

	require.Len(t, ctx.TranscriptMessages, 1)
	assert.Equal(t, "m4", ctx.TranscriptMessages[0].Content)

	require.Len(t, ctx.Messages, 3)
	assert.Equal(t, []chat.Message{
		ctx.InvariantSystemMessages[0],
		ctx.SummaryMessages[0],
		ctx.TranscriptMessages[0],
	}, ctx.Messages)
	assert.Equal(t, 3, ctx.Metadata.SummaryStartIndex)
	assert.Equal(t, 1, ctx.Metadata.SystemMessages)
	assert.Equal(t, 2, ctx.Metadata.ConversationMessages)
}

func TestBuildPromptContext_AppliesHistoryAndToolPolicies(t *testing.T) {
	assistantWithToolCall := &Message{Message: chat.Message{
		Role: chat.MessageRoleAssistant,
		ToolCalls: []tools.ToolCall{{
			ID:   "call_1",
			Type: "function",
			Function: tools.FunctionCall{
				Name:      "shell",
				Arguments: `{"cmd":"echo hi"}`,
			},
		}},
	}}
	toolResult := &Message{Message: chat.Message{
		Role:       chat.MessageRoleTool,
		ToolCallID: "call_1",
		Content:    "this is a long tool result that should be truncated by the prompt builder",
	}}

	sess := New(
		WithMessages([]Item{
			NewMessageItem(&Message{Message: chat.Message{Role: chat.MessageRoleAssistant, Content: "old response"}}),
			NewMessageItem(UserMessage("second")),
			NewMessageItem(assistantWithToolCall),
			NewMessageItem(toolResult),
		}),
		WithMaxOldToolCallTokens(1),
	)
	a := agent.New("root", "base instruction", agent.WithNumHistoryItems(3))

	ctx := sess.BuildPromptContext(a)
	require.NotNil(t, ctx)
	assert.Equal(t, 3, ctx.Metadata.MaxHistoryItems)
	assert.Equal(t, 1, ctx.Metadata.MaxOldToolCallTokens)

	// The prompt builder should keep only the newest conversation messages once
	// the history cap is applied, while still preserving the system instruction.
	var conversation []chat.Message
	for _, msg := range ctx.Messages {
		if msg.Role != chat.MessageRoleSystem {
			conversation = append(conversation, msg)
		}
	}
	require.Len(t, conversation, 3)
	assert.Equal(t, "second", conversation[0].Content)
	assert.Equal(t, chat.MessageRoleAssistant, conversation[1].Role)
	assert.Equal(t, toolContentPlaceholder, conversation[2].Content)
}
