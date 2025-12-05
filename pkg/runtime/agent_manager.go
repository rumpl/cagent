package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/tools"
)

type agentManager struct {
	team           *team.Team
	currentAgent   string
	requestedAgent string
	events         EventPublisher
	tracing        *tracingProvider
}

func newAgentManager(t *team.Team, events EventPublisher, tracing *tracingProvider) *agentManager {
	return &agentManager{
		team:         t,
		currentAgent: "root",
		events:       events,
		tracing:      tracing,
	}
}

func (m *agentManager) CurrentAgentName() string {
	return m.currentAgent
}

func (m *agentManager) CurrentAgent() *agent.Agent {
	current, _ := m.team.Agent(m.currentAgent)
	return current
}

func (m *agentManager) SetCurrentAgent(name string) error {
	m.requestedAgent = name
	if _, err := m.team.Agent(name); err != nil {
		return err
	}
	m.currentAgent = name
	return nil
}

func (m *agentManager) ValidateRequestedAgent() error {
	if m.requestedAgent != "" && m.requestedAgent != m.currentAgent {
		agents := m.team.AgentNames()
		return fmt.Errorf("agent not found: %s (available agents: %s)", m.requestedAgent, strings.Join(agents, ", "))
	}
	return nil
}

func (m *agentManager) CurrentAgentCommands(ctx context.Context) map[string]string {
	return m.CurrentAgent().Commands()
}

func (m *agentManager) CurrentWelcomeMessage(ctx context.Context) string {
	return m.CurrentAgent().WelcomeMessage()
}

func (m *agentManager) Team() *team.Team {
	return m.team
}

func (m *agentManager) AgentNames() []string {
	return m.team.AgentNames()
}

func (m *agentManager) Agent(name string) (*agent.Agent, error) {
	return m.team.Agent(name)
}

func (m *agentManager) GetTools(ctx context.Context, a *agent.Agent) ([]tools.Tool, error) {
	shouldEmitMCPInit := len(a.ToolSets()) > 0
	if shouldEmitMCPInit {
		m.events.Publish(MCPInitStarted(a.Name()))
	}
	defer func() {
		if shouldEmitMCPInit {
			m.events.Publish(MCPInitFinished(a.Name()))
		}
	}()

	agentTools, err := a.Tools(ctx)
	if err != nil {
		slog.Error("Failed to get agent tools", "agent", a.Name(), "error", err)
		return nil, err
	}

	slog.Debug("Retrieved agent tools", "agent", a.Name(), "tool_count", len(agentTools))
	return agentTools, nil
}

func (m *agentManager) EmitAgentWarnings(a *agent.Agent) {
	warnings := a.DrainWarnings()
	if len(warnings) == 0 {
		return
	}

	slog.Warn("Tool setup partially failed; continuing", "agent", a.Name(), "warnings", warnings)
	m.events.Publish(Warning(formatToolWarning(a, warnings), m.currentAgent))
}

func (m *agentManager) EmitAgentInfo(a *agent.Agent) {
	var modelID string
	if model := a.Model(); model != nil {
		modelID = model.ID()
	}
	m.events.Publish(AgentInfo(a.Name(), modelID, a.Description()))
}

func (m *agentManager) EmitTeamInfo() {
	availableAgents := m.team.AgentNames()
	m.events.Publish(TeamInfo(availableAgents, m.currentAgent))
}

func (m *agentManager) EmitToolsetInfo(toolCount int) {
	m.events.Publish(ToolsetInfo(toolCount, m.currentAgent))
}

func formatToolWarning(a *agent.Agent, warnings []string) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Some toolsets failed to initialize for agent '%s'.\n\n", a.Name()))
	builder.WriteString("Details:\n\n")
	for _, warning := range warnings {
		builder.WriteString("- ")
		builder.WriteString(warning)
		builder.WriteByte('\n')
	}

	return strings.TrimSuffix(builder.String(), "\n")
}
