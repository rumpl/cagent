package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/runtime/contract"
	"github.com/docker/docker-agent/pkg/tools"
)

func TestMergeEventsMergesUnstampedRuns(t *testing.T) {
	t.Parallel()

	events := []tea.Msg{
		contract.AgentChoice("root", "s", "a"),
		contract.AgentChoice("root", "s", "b"),
		contract.ToolCallOutput("call-1", tools.Tool{}, "1", "root"),
		contract.ToolCallOutput("call-1", tools.Tool{}, "2", "root"),
	}

	merged := (&App{}).mergeEvents(events)

	require.Len(t, merged, 2)
	assert.Equal(t, "ab", merged[0].(*contract.AgentChoiceEvent).Content)
	assert.Equal(t, "12", merged[1].(*contract.ToolCallOutputEvent).Output)
}

func TestMergeEventsNeverMergesAcrossGenerations(t *testing.T) {
	t.Parallel()

	stamp := func(generation uint64, content string) tea.Msg {
		return generationEvent{generation: generation, inner: contract.AgentChoice("root", "s", content)}
	}
	events := []tea.Msg{
		stamp(1, "a"),
		stamp(1, "b"),
		stamp(2, "c"),
		contract.AgentChoice("root", "s", "d"),
		stamp(2, "e"),
	}

	merged := (&App{}).mergeEvents(events)

	require.Len(t, merged, 4)
	for i, want := range []struct {
		generation uint64
		content    string
	}{{1, "ab"}, {2, "c"}, {0, "d"}, {2, "e"}} {
		msg := merged[i]
		if want.generation != 0 {
			stamped, ok := msg.(generationEvent)
			require.True(t, ok)
			assert.Equal(t, want.generation, stamped.generation)
			msg = stamped.inner
		}
		assert.Equal(t, want.content, msg.(*contract.AgentChoiceEvent).Content)
	}
}
