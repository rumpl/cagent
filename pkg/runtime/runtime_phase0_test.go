package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/team"
	"github.com/docker/docker-agent/pkg/tools"
)

func TestPersistentRuntime_PersistsFinalSessionStateWithoutDuplicateAssistantMessages(t *testing.T) {
	store := session.NewInMemorySessionStore()

	stream := newStreamBuilder().
		AddContent("Hello ").
		AddContent("world").
		AddStopWithUsage(4, 5).
		Build()

	prov := &mockProvider{id: "test/mock-model", stream: stream}
	root := agent.New("root", "You are a test agent", agent.WithModel(prov))
	tm := team.New(team.WithAgents(root))

	rt, err := New(tm,
		WithSessionCompaction(false),
		WithModelStore(mockModelStore{}),
		WithSessionStore(store),
	)
	require.NoError(t, err)

	sess := session.New(session.WithUserMessage("Hi"))
	sess.Title = "Persistence characterization"

	for range rt.RunStream(t.Context(), sess) {
	}

	persisted, err := store.GetSession(t.Context(), sess.ID)
	require.NoError(t, err)

	messages := persisted.GetAllMessages()
	require.Len(t, messages, 2, "streaming persistence should finalize to one user message and one assistant message")
	assert.Equal(t, chat.MessageRoleUser, messages[0].Message.Role)
	assert.Equal(t, "Hi", messages[0].Message.Content)
	assert.Equal(t, chat.MessageRoleAssistant, messages[1].Message.Role)
	assert.Equal(t, "Hello world", messages[1].Message.Content)
	assert.Equal(t, int64(4), persisted.InputTokens)
	assert.Equal(t, int64(5), persisted.OutputTokens)
}

func TestRunStream_SteerQueuedMessageInjectedAfterToolBatch(t *testing.T) {
	prov := &queueProvider{id: "test/mock-model", streams: []chat.MessageStream{
		newStreamBuilder().
			AddToolCallName("call_1", "noop").
			AddToolCallArguments("call_1", `{}`).
			Build(),
		newStreamBuilder().
			AddContent("Handled steer").
			AddStopWithUsage(6, 3).
			Build(),
	}}

	agentTools := []tools.Tool{{
		Name:       "noop",
		Parameters: map[string]any{},
		Handler: func(_ context.Context, _ tools.ToolCall) (*tools.ToolCallResult, error) {
			return tools.ResultSuccess("ok"), nil
		},
	}}

	root := agent.New("root", "You are a test agent",
		agent.WithModel(prov),
		agent.WithToolSets(newStubToolSet(nil, agentTools, nil)),
	)
	tm := team.New(team.WithAgents(root))

	rt, err := NewLocalRuntime(tm,
		WithSessionCompaction(false),
		WithModelStore(mockModelStore{}),
	)
	require.NoError(t, err)
	require.NoError(t, rt.Steer(QueuedMessage{Content: "Need more detail"}))

	sess := session.New(
		session.WithUserMessage("Start"),
		session.WithToolsApproved(true),
	)

	var events []Event
	for ev := range rt.RunStream(t.Context(), sess) {
		events = append(events, ev)
	}

	toolResponseIdx := -1
	steerUserIdx := -1
	assistantIdx := -1
	for i, ev := range events {
		switch e := ev.(type) {
		case *ToolCallResponseEvent:
			if e.ToolCallID == "call_1" {
				toolResponseIdx = i
			}
		case *UserMessageEvent:
			if e.Message == "Need more detail" {
				steerUserIdx = i
			}
		case *AgentChoiceEvent:
			if e.Content == "Handled steer" {
				assistantIdx = i
			}
		}
	}

	require.NotEqual(t, -1, toolResponseIdx, "expected tool response before steer injection")
	require.NotEqual(t, -1, steerUserIdx, "expected user_message event for steered input")
	require.NotEqual(t, -1, assistantIdx, "expected second-turn assistant response")
	assert.Less(t, toolResponseIdx, steerUserIdx)
	assert.Less(t, steerUserIdx, assistantIdx)

	var foundWrappedReminder bool
	for _, msg := range sess.GetAllMessages() {
		if msg.Message.Role != chat.MessageRoleUser {
			continue
		}
		if strings.Contains(msg.Message.Content, "Need more detail") && strings.Contains(msg.Message.Content, "<system-reminder>") {
			foundWrappedReminder = true
			break
		}
	}
	assert.True(t, foundWrappedReminder, "steered input should be stored as a wrapped reminder message in the transcript")
}

func TestRunStream_FollowUpQueuedMessageStartsFreshTurn(t *testing.T) {
	prov := &queueProvider{id: "test/mock-model", streams: []chat.MessageStream{
		newStreamBuilder().
			AddContent("First turn complete").
			AddStopWithUsage(4, 2).
			Build(),
		newStreamBuilder().
			AddContent("Second turn complete").
			AddStopWithUsage(5, 3).
			Build(),
	}}

	root := agent.New("root", "You are a test agent", agent.WithModel(prov))
	tm := team.New(team.WithAgents(root))

	rt, err := NewLocalRuntime(tm,
		WithSessionCompaction(false),
		WithModelStore(mockModelStore{}),
	)
	require.NoError(t, err)
	require.NoError(t, rt.FollowUp(QueuedMessage{Content: "Actually also do X"}))

	sess := session.New(session.WithUserMessage("Start"))

	var events []Event
	for ev := range rt.RunStream(t.Context(), sess) {
		events = append(events, ev)
	}

	firstAssistantIdx := -1
	followUpUserIdx := -1
	secondAssistantIdx := -1
	for i, ev := range events {
		switch e := ev.(type) {
		case *MessageAddedEvent:
			if e.Message != nil && e.Message.Message.Role == chat.MessageRoleAssistant && e.Message.Message.Content == "First turn complete" {
				firstAssistantIdx = i
			}
		case *UserMessageEvent:
			if e.Message == "Actually also do X" {
				followUpUserIdx = i
			}
		case *AgentChoiceEvent:
			if e.Content == "Second turn complete" {
				secondAssistantIdx = i
			}
		}
	}

	require.NotEqual(t, -1, firstAssistantIdx, "expected first-turn assistant commit")
	require.NotEqual(t, -1, followUpUserIdx, "expected follow-up user message event")
	require.NotEqual(t, -1, secondAssistantIdx, "expected second-turn response")
	assert.Less(t, firstAssistantIdx, followUpUserIdx)
	assert.Less(t, followUpUserIdx, secondAssistantIdx)

	messages := sess.GetAllMessages()
	require.Len(t, messages, 4)
	assert.Equal(t, chat.MessageRoleUser, messages[2].Message.Role)
	assert.Equal(t, "Actually also do X", messages[2].Message.Content)
}
