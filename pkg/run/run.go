// Package run provides simplified functions for running agents and teams.
// It hides the complexity of setting up runtimes and sessions.
package run

import (
	"context"
	"errors"
	"fmt"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
)

// Agent runs a single agent with the given prompt and returns the final response.
// The agent must have at least one model configured via agent.WithModel.
func Agent(ctx context.Context, a *agent.Agent, prompt string) (string, error) {
	if a == nil {
		return "", errors.New("agent is required")
	}
	if prompt == "" {
		return "", errors.New("prompt is required")
	}

	t := team.New(team.WithAgents(a))

	rt, err := runtime.New(t)
	if err != nil {
		return "", fmt.Errorf("creating runtime: %w", err)
	}
	defer func() { _ = t.StopToolSets(ctx) }()

	sess := session.New(
		session.WithUserMessage(prompt),
		session.WithToolsApproved(true),
	)

	_, err = rt.Run(ctx, sess)
	if err != nil {
		return "", fmt.Errorf("running agent: %w", err)
	}

	return sess.GetLastAssistantMessageContent(), nil
}

// Team runs a team of agents with the given prompt and returns the final response.
// The root agent receives the initial prompt and can delegate to other agents.
func Team(ctx context.Context, t *team.Team, prompt string) (string, error) {
	if t == nil {
		return "", errors.New("team is required")
	}
	if prompt == "" {
		return "", errors.New("prompt is required")
	}

	rt, err := runtime.New(t)
	if err != nil {
		return "", fmt.Errorf("creating runtime: %w", err)
	}
	defer func() { _ = t.StopToolSets(ctx) }()

	sess := session.New(
		session.WithUserMessage(prompt),
		session.WithToolsApproved(true),
	)

	_, err = rt.Run(ctx, sess)
	if err != nil {
		return "", fmt.Errorf("running team: %w", err)
	}

	return sess.GetLastAssistantMessageContent(), err
}
