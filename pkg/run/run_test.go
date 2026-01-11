package run

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/team"
)

func TestAgent_ValidationErrors(t *testing.T) {
	ctx := t.Context()

	t.Run("nil agent", func(t *testing.T) {
		_, err := Agent(ctx, nil, "test")
		assert.ErrorContains(t, err, "agent is required")
	})

	t.Run("empty prompt", func(t *testing.T) {
		a := agent.New("root", "You are helpful")
		_, err := Agent(ctx, a, "")
		assert.ErrorContains(t, err, "prompt is required")
	})
}

func TestTeam_ValidationErrors(t *testing.T) {
	ctx := t.Context()

	t.Run("nil team", func(t *testing.T) {
		_, err := Team(ctx, nil, "test")
		assert.ErrorContains(t, err, "team is required")
	})

	t.Run("empty prompt", func(t *testing.T) {
		a := agent.New("root", "You are helpful")
		tm := team.New(team.WithAgents(a))
		_, err := Team(ctx, tm, "")
		assert.ErrorContains(t, err, "prompt is required")
	})
}
