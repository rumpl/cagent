package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/runtime/contract"
	"github.com/docker/docker-agent/pkg/runtime/contract/contracttest"
	"github.com/docker/docker-agent/pkg/team"
)

func TestLocalRuntime_Contract(t *testing.T) {
	t.Parallel()

	contracttest.Run(t, func(t *testing.T) contract.Runtime {
		t.Helper()
		prov := &mockProvider{id: "test/mock-model", stream: &mockStream{}}
		root := agent.New("root", "You are a test agent", agent.WithModel(prov))
		tm := team.New(team.WithAgents(root))

		rt, err := New(t.Context(), tm, WithSessionCompaction(false), WithModelStore(mockModelStore{}))
		require.NoError(t, err)
		return rt
	})
}
