package runtime

import (
	"context"
	"io"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/model/provider/base"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/tools"
)

type mockAgentEventPublisher struct {
	mu     sync.Mutex
	events []Event
}

func (m *mockAgentEventPublisher) Publish(event Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
}

func (m *mockAgentEventPublisher) Events() []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Event{}, m.events...)
}

type agentTestProvider struct {
	id string
}

func (m *agentTestProvider) ID() string { return m.id }

func (m *agentTestProvider) CreateChatCompletionStream(context.Context, []chat.Message, []tools.Tool) (chat.MessageStream, error) {
	return &agentTestStream{}, nil
}

func (m *agentTestProvider) BaseConfig() base.Config { return base.Config{} }

func (m *agentTestProvider) MaxTokens() int { return 0 }

type agentTestStream struct{}

func (m *agentTestStream) Recv() (chat.MessageStreamResponse, error) {
	return chat.MessageStreamResponse{}, io.EOF
}

func (m *agentTestStream) Close() {}

func TestAgentManager_CurrentAgentName(t *testing.T) {
	publisher := &mockAgentEventPublisher{}
	tracing := newTracingProvider(nil)

	testAgent := agent.New("test-agent", "test system prompt")
	testTeam := team.New(team.WithAgents(testAgent))

	mgr := newAgentManager(testTeam, publisher, tracing)
	err := mgr.SetCurrentAgent("test-agent")
	require.NoError(t, err)

	assert.Equal(t, "test-agent", mgr.CurrentAgentName())
}

func TestAgentManager_CurrentAgent(t *testing.T) {
	publisher := &mockAgentEventPublisher{}
	tracing := newTracingProvider(nil)

	testAgent := agent.New("test-agent", "test system prompt")
	testTeam := team.New(team.WithAgents(testAgent))

	mgr := newAgentManager(testTeam, publisher, tracing)
	err := mgr.SetCurrentAgent("test-agent")
	require.NoError(t, err)

	current := mgr.CurrentAgent()
	require.NotNil(t, current)
	assert.Equal(t, "test-agent", current.Name())
}

func TestAgentManager_SetCurrentAgent_NotFound(t *testing.T) {
	publisher := &mockAgentEventPublisher{}
	tracing := newTracingProvider(nil)

	testAgent := agent.New("test-agent", "test system prompt")
	testTeam := team.New(team.WithAgents(testAgent))

	mgr := newAgentManager(testTeam, publisher, tracing)

	err := mgr.SetCurrentAgent("nonexistent")
	require.Error(t, err)
}

func TestAgentManager_EmitAgentInfo(t *testing.T) {
	publisher := &mockAgentEventPublisher{}
	tracing := newTracingProvider(nil)

	prov := &agentTestProvider{id: "test/mock-model"}
	testAgent := agent.New("test-agent", "test system prompt", agent.WithModel(prov))
	testTeam := team.New(team.WithAgents(testAgent))

	mgr := newAgentManager(testTeam, publisher, tracing)
	err := mgr.SetCurrentAgent("test-agent")
	require.NoError(t, err)

	mgr.EmitAgentInfo(testAgent)

	events := publisher.Events()
	require.Len(t, events, 1)

	infoEvent, ok := events[0].(*AgentInfoEvent)
	require.True(t, ok)
	assert.Equal(t, "test-agent", infoEvent.AgentName)
	assert.Equal(t, "test/mock-model", infoEvent.Model)
}

func TestAgentManager_EmitTeamInfo(t *testing.T) {
	publisher := &mockAgentEventPublisher{}
	tracing := newTracingProvider(nil)

	agent1 := agent.New("agent-1", "prompt 1")
	agent2 := agent.New("agent-2", "prompt 2")
	testTeam := team.New(team.WithAgents(agent1, agent2))

	mgr := newAgentManager(testTeam, publisher, tracing)
	err := mgr.SetCurrentAgent("agent-1")
	require.NoError(t, err)

	mgr.EmitTeamInfo()

	events := publisher.Events()
	require.Len(t, events, 1)

	teamEvent, ok := events[0].(*TeamInfoEvent)
	require.True(t, ok)
	assert.Len(t, teamEvent.AvailableAgents, 2)
}

func TestAgentManager_AgentNames(t *testing.T) {
	publisher := &mockAgentEventPublisher{}
	tracing := newTracingProvider(nil)

	agent1 := agent.New("agent-1", "prompt 1")
	agent2 := agent.New("agent-2", "prompt 2")
	testTeam := team.New(team.WithAgents(agent1, agent2))

	mgr := newAgentManager(testTeam, publisher, tracing)

	names := mgr.AgentNames()
	assert.Len(t, names, 2)
}

func TestAgentManager_Agent(t *testing.T) {
	publisher := &mockAgentEventPublisher{}
	tracing := newTracingProvider(nil)

	testAgent := agent.New("test-agent", "test system prompt")
	testTeam := team.New(team.WithAgents(testAgent))

	mgr := newAgentManager(testTeam, publisher, tracing)

	a, err := mgr.Agent("test-agent")
	require.NoError(t, err)
	assert.Equal(t, "test-agent", a.Name())

	_, err = mgr.Agent("nonexistent")
	require.Error(t, err)
}

func TestAgentManager_Team(t *testing.T) {
	publisher := &mockAgentEventPublisher{}
	tracing := newTracingProvider(nil)

	testAgent := agent.New("test-agent", "test system prompt")
	testTeam := team.New(team.WithAgents(testAgent))

	mgr := newAgentManager(testTeam, publisher, tracing)

	assert.Same(t, testTeam, mgr.Team())
}

func TestAgentManager_EmitToolsetInfo(t *testing.T) {
	publisher := &mockAgentEventPublisher{}
	tracing := newTracingProvider(nil)

	testAgent := agent.New("test-agent", "test system prompt")
	testTeam := team.New(team.WithAgents(testAgent))

	mgr := newAgentManager(testTeam, publisher, tracing)
	err := mgr.SetCurrentAgent("test-agent")
	require.NoError(t, err)

	mgr.EmitToolsetInfo(5)

	events := publisher.Events()
	require.Len(t, events, 1)

	toolsetEvent, ok := events[0].(*ToolsetInfoEvent)
	require.True(t, ok)
	assert.Equal(t, 5, toolsetEvent.AvailableTools)
	assert.Equal(t, "test-agent", toolsetEvent.AgentName)
}

func TestAgentManager_GetTools(t *testing.T) {
	publisher := &mockAgentEventPublisher{}
	tracing := newTracingProvider(nil)

	testAgent := agent.New("test-agent", "test system prompt")
	testTeam := team.New(team.WithAgents(testAgent))

	mgr := newAgentManager(testTeam, publisher, tracing)

	agentTools, err := mgr.GetTools(t.Context(), testAgent)
	require.NoError(t, err)
	assert.Empty(t, agentTools)
}

func TestAgentManager_CurrentAgentCommands(t *testing.T) {
	publisher := &mockAgentEventPublisher{}
	tracing := newTracingProvider(nil)

	testAgent := agent.New("test-agent", "test system prompt")
	testTeam := team.New(team.WithAgents(testAgent))

	mgr := newAgentManager(testTeam, publisher, tracing)
	err := mgr.SetCurrentAgent("test-agent")
	require.NoError(t, err)

	commands := mgr.CurrentAgentCommands(t.Context())
	assert.Empty(t, commands)
}

func TestAgentManager_CurrentWelcomeMessage(t *testing.T) {
	publisher := &mockAgentEventPublisher{}
	tracing := newTracingProvider(nil)

	testAgent := agent.New("test-agent", "test system prompt", agent.WithWelcomeMessage("Welcome!"))
	testTeam := team.New(team.WithAgents(testAgent))

	mgr := newAgentManager(testTeam, publisher, tracing)
	err := mgr.SetCurrentAgent("test-agent")
	require.NoError(t, err)

	msg := mgr.CurrentWelcomeMessage(t.Context())
	assert.Equal(t, "Welcome!", msg)
}

func TestFormatToolWarning(t *testing.T) {
	testAgent := agent.New("test-agent", "test system prompt")
	warnings := []string{"warning 1", "warning 2"}

	result := formatToolWarning(testAgent, warnings)

	assert.Contains(t, result, "test-agent")
	assert.Contains(t, result, "warning 1")
	assert.Contains(t, result, "warning 2")
}
