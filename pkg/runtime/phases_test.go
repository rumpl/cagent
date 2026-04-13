package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/team"
	"github.com/docker/docker-agent/pkg/tools"
)

func TestLifecycleHooks_FireInTurnOrder(t *testing.T) {
	stream := newStreamBuilder().
		AddContent("Hello").
		AddStopWithUsage(3, 2).
		Build()

	prov := &mockProvider{id: "test/mock-model", stream: stream}
	root := agent.New("root", "You are a test agent", agent.WithModel(prov))
	tm := team.New(team.WithAgents(root))

	var order []string
	rt, err := NewLocalRuntime(
		tm,
		WithSessionCompaction(false),
		WithModelStore(mockModelStore{}),
		WithLifecycleHooks(RuntimeLifecycleHooks{
			SessionStart: []SessionHook{func(context.Context, *SessionPhase) error {
				order = append(order, "session_start")
				return nil
			}},
			BeforeUserMessage: []UserMessageHook{func(context.Context, *UserMessagePhase) error {
				order = append(order, "before_user_message")
				return nil
			}},
			AfterUserMessage: []UserMessageHook{func(context.Context, *UserMessagePhase) error {
				order = append(order, "after_user_message")
				return nil
			}},
			TurnStart: []TurnHook{func(context.Context, *TurnPhase) error {
				order = append(order, "turn_start")
				return nil
			}},
			BeforeAssistantCommit: []AssistantCommitHook{func(context.Context, *AssistantCommitPhase) error {
				order = append(order, "before_assistant_commit")
				return nil
			}},
			AfterAssistantCommit: []AssistantCommitHook{func(context.Context, *AssistantCommitPhase) error {
				order = append(order, "after_assistant_commit")
				return nil
			}},
			TurnEnd: []TurnHook{func(context.Context, *TurnPhase) error {
				order = append(order, "turn_end")
				return nil
			}},
			SessionEnd: []SessionHook{func(context.Context, *SessionPhase) error {
				order = append(order, "session_end")
				return nil
			}},
		}),
	)
	require.NoError(t, err)

	sess := session.New(session.WithUserMessage("Hi"))
	for range rt.RunStream(t.Context(), sess) {
	}

	assert.Equal(t, []string{
		"session_start",
		"before_user_message",
		"after_user_message",
		"turn_start",
		"before_assistant_commit",
		"after_assistant_commit",
		"turn_end",
		"session_end",
	}, order)
}

func TestMiddlewares_WrapContextModelAndToolPhases(t *testing.T) {
	prov := &queueProvider{id: "test/mock-model", streams: []chat.MessageStream{
		newStreamBuilder().
			AddToolCallName("call_1", "noop").
			AddToolCallArguments("call_1", `{}`).
			Build(),
		newStreamBuilder().
			AddContent("Done").
			AddStopWithUsage(4, 2).
			Build(),
	}}

	toolset := newStubToolSet(nil, []tools.Tool{{
		Name:       "noop",
		Parameters: map[string]any{},
		Handler: func(_ context.Context, _ tools.ToolCall) (*tools.ToolCallResult, error) {
			return tools.ResultSuccess("ok"), nil
		},
	}}, nil)

	root := agent.New("root", "base instruction", agent.WithModel(prov), agent.WithToolSets(toolset))
	tm := team.New(team.WithAgents(root))

	var sawInjectedPrompt bool
	var toolMiddlewareOrder []string
	rt, err := NewLocalRuntime(
		tm,
		WithSessionCompaction(false),
		WithModelStore(mockModelStore{}),
		WithBuildContextMiddlewares(func(ctx context.Context, phase *BuildContextPhase, next BuildContextHandler) error {
			if err := next(ctx, phase); err != nil {
				return err
			}
			phase.PromptContext.ContextSystemMessages = append(phase.PromptContext.ContextSystemMessages, chat.Message{
				Role:    chat.MessageRoleSystem,
				Content: "Injected middleware context",
			})
			phase.PromptContext.Messages = append([]chat.Message{{
				Role:    chat.MessageRoleSystem,
				Content: "Injected middleware context",
			}}, phase.PromptContext.Messages...)
			return nil
		}),
		WithModelMiddlewares(func(ctx context.Context, phase *ModelPhase, next ModelHandler) (*ModelResult, error) {
			for _, msg := range phase.Messages {
				if msg.Role == chat.MessageRoleSystem && msg.Content == "Injected middleware context" {
					sawInjectedPrompt = true
					break
				}
			}
			return next(ctx, phase)
		}),
		WithToolMiddlewares(func(ctx context.Context, phase *ToolCallPhase, next ToolHandler) (*ToolExecutionResult, error) {
			toolMiddlewareOrder = append(toolMiddlewareOrder, "before:"+phase.ToolCall.Function.Name)
			res, err := next(ctx, phase)
			toolMiddlewareOrder = append(toolMiddlewareOrder, "after:"+phase.ToolCall.Function.Name)
			return res, err
		}),
	)
	require.NoError(t, err)

	sess := session.New(
		session.WithUserMessage("Run the tool"),
		session.WithToolsApproved(true),
	)
	for range rt.RunStream(t.Context(), sess) {
	}

	assert.True(t, sawInjectedPrompt, "model middleware should see the prompt injected by build-context middleware")
	assert.Equal(t, []string{"before:noop", "after:noop"}, toolMiddlewareOrder)
}
