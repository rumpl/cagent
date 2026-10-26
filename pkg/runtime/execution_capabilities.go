package runtime

import (
	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/runtime/inspection"
)

const (
	ExecutionModeNative           = inspection.ExecutionModeNative
	ExecutionModeHarness          = inspection.ExecutionModeHarness
	ToolExecutionDockerAgent      = inspection.ToolExecutionDockerAgent
	ToolExecutionHarnessReported  = inspection.ToolExecutionHarnessReported
	ToolApprovalDockerAgentPolicy = inspection.ToolApprovalDockerAgentPolicy
	ToolApprovalExternal          = inspection.ToolApprovalExternal
)

// ExecutionCapabilities describes an agent's execution integration.
type ExecutionCapabilities = inspection.ExecutionCapabilities

// AgentExecutionCapabilities inspects the named agent without starting tools or drivers.
// Unknown agents return nil; callers must not interpret absence as native support.
func (r *LocalRuntime) AgentExecutionCapabilities(name string) *ExecutionCapabilities {
	if r == nil || r.team == nil {
		return nil
	}
	a, err := r.team.Agent(name)
	if err != nil {
		return nil
	}
	return executionCapabilities(a)
}

func executionCapabilities(a *agent.Agent) *ExecutionCapabilities {
	return inspection.Capabilities(a)
}
