package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/team"
	"github.com/docker/docker-agent/pkg/tools"
)

func TestRunStream_ResumesPendingToolApprovalFromSession(t *testing.T) {
	tc := tools.ToolCall{
		ID:   "call-1",
		Type: "function",
		Function: tools.FunctionCall{
			Name:      "shell",
			Arguments: `{"cmd":"echo ok"}`,
		},
	}

	var ran bool
	tool := tools.Tool{
		Name: "shell",
		Handler: func(context.Context, tools.ToolCall) (*tools.ToolCallResult, error) {
			ran = true
			return tools.ResultSuccess("tool output"), nil
		},
	}

	sess := session.New(session.WithMessages([]session.Item{
		session.NewMessageItem(session.UserMessage("please run this")),
		session.NewMessageItem(session.NewAgentMessage("root", &chat.Message{
			Role:            chat.MessageRoleAssistant,
			ToolCalls:       []tools.ToolCall{tc},
			ToolDefinitions: []tools.Tool{tool},
			FinishReason:    chat.FinishReasonToolCalls,
		})),
	}))
	require.True(t, sess.HasPendingToolCalls())

	prov := &mockProvider{
		id:     "test/mock-model",
		stream: newStreamBuilder().AddContent("final answer").AddStopWithUsage(1, 1).Build(),
	}
	root := agent.New("root", "You are a test agent", agent.WithModel(prov), agent.WithToolSets(newStubToolSet(nil, []tools.Tool{tool}, nil)))
	tm := team.New(team.WithAgents(root))

	rt, err := NewLocalRuntime(tm, WithSessionCompaction(false), WithModelStore(mockModelStore{}))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	var events []Event
	for ev := range rt.RunStream(ctx, sess) {
		events = append(events, ev)
		if _, ok := ev.(*ToolCallConfirmationEvent); ok {
			rt.Resume(ctx, ResumeApprove())
		}
	}
	require.NoError(t, ctx.Err())

	assert.True(t, ran, "approved pending tool call should execute")
	assert.False(t, sess.HasPendingToolCalls(), "tool response should clear pending state")
	assert.False(t, hasEventType(t, events, &UserMessageEvent{}), "restoring a pending tool call must not replay the last assistant/tool message as a user message")

	confirmationIdx := -1
	toolCallIdx := -1
	responseIdx := -1
	for i, ev := range events {
		switch ev.(type) {
		case *ToolCallConfirmationEvent:
			confirmationIdx = i
		case *ToolCallEvent:
			toolCallIdx = i
		case *ToolCallResponseEvent:
			responseIdx = i
		}
	}
	require.NotEqual(t, -1, confirmationIdx, "expected restored approval prompt")
	require.NotEqual(t, -1, toolCallIdx, "expected tool execution event after approval")
	require.NotEqual(t, -1, responseIdx, "expected tool response event")
	assert.Less(t, confirmationIdx, toolCallIdx)
	assert.Less(t, toolCallIdx, responseIdx)

	messages := sess.GetAllMessages()
	require.Len(t, messages, 4)
	assert.Equal(t, chat.MessageRoleTool, messages[2].Message.Role)
	assert.Equal(t, "tool output", messages[2].Message.Content)
	assert.Equal(t, chat.MessageRoleAssistant, messages[3].Message.Role)
	assert.Equal(t, "final answer", messages[3].Message.Content)
}
