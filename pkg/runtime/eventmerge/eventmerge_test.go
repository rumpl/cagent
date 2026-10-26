package eventmerge

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/runtime/contract"
	"github.com/docker/docker-agent/pkg/tools"
)

func TestMergeConcatenatesAgentChoiceContent(t *testing.T) {
	t.Parallel()

	chunks := []string{"Hello ", "streaming ", "world", "!"}
	events := make([]contract.Event, 0, len(chunks))
	for _, c := range chunks {
		events = append(events, &contract.AgentChoiceEvent{
			Content:      c,
			AgentContext: contract.AgentContext{AgentName: "agent-a"},
		})
	}

	merged := Merge(events)

	require.Len(t, merged, 1)
	assert.Equal(t, "Hello streaming world!", merged[0].(*contract.AgentChoiceEvent).Content)
}

func TestMergeKeepsBoundaryBetweenAgents(t *testing.T) {
	t.Parallel()

	events := []contract.Event{
		&contract.AgentChoiceEvent{Content: "a1", AgentContext: contract.AgentContext{AgentName: "agent-a"}},
		&contract.AgentChoiceEvent{Content: "a2", AgentContext: contract.AgentContext{AgentName: "agent-a"}},
		&contract.AgentChoiceEvent{Content: "b1", AgentContext: contract.AgentContext{AgentName: "agent-b"}},
		&contract.AgentChoiceEvent{Content: "a3", AgentContext: contract.AgentContext{AgentName: "agent-a"}},
	}

	merged := Merge(events)

	require.Len(t, merged, 3)
	assert.Equal(t, "a1a2", merged[0].(*contract.AgentChoiceEvent).Content)
	assert.Equal(t, "b1", merged[1].(*contract.AgentChoiceEvent).Content)
	assert.Equal(t, "a3", merged[2].(*contract.AgentChoiceEvent).Content)
}

func TestMergeConcatenatesPartialToolCallArguments(t *testing.T) {
	t.Parallel()

	def := &tools.Tool{Name: "shell"}
	events := []contract.Event{
		&contract.PartialToolCallEvent{
			ToolCall: tools.ToolCall{
				ID:         "call-1",
				ProviderID: "native-1",
				Function:   tools.FunctionCall{Arguments: `{"a"`},
			},
		},
		&contract.PartialToolCallEvent{
			ToolCall:       tools.ToolCall{ID: "call-1", Function: tools.FunctionCall{Name: "shell", Arguments: `:1`}},
			ToolDefinition: def,
		},
		&contract.PartialToolCallEvent{
			ToolCall: tools.ToolCall{ID: "call-1", Function: tools.FunctionCall{Arguments: `}`}},
		},
	}

	merged := Merge(events)

	require.Len(t, merged, 1)
	got := merged[0].(*contract.PartialToolCallEvent)
	assert.Equal(t, `{"a":1}`, got.ToolCall.Function.Arguments)
	assert.Equal(t, "shell", got.ToolCall.Function.Name)
	assert.Equal(t, "native-1", got.ToolCall.ProviderID)
	assert.Same(t, def, got.ToolDefinition)
}

func TestMergeConcatenatesToolCallOutput(t *testing.T) {
	t.Parallel()

	events := []contract.Event{
		&contract.ToolCallOutputEvent{ToolCallID: "call-1", Output: "line 1\n"},
		&contract.ToolCallOutputEvent{ToolCallID: "call-1", Output: "line 2\n"},
		&contract.ToolCallOutputEvent{ToolCallID: "call-2", Output: "other\n"},
	}

	merged := Merge(events)

	require.Len(t, merged, 2)
	assert.Equal(t, "line 1\nline 2\n", merged[0].(*contract.ToolCallOutputEvent).Output)
	assert.Equal(t, "other\n", merged[1].(*contract.ToolCallOutputEvent).Output)
}

func TestMergeNeverJoinsAcrossOtherEvents(t *testing.T) {
	t.Parallel()

	warning := contract.Warning("heads up", "root")
	lone := contract.AgentChoice("root", "s", "c")
	events := []contract.Event{
		contract.AgentChoice("root", "s", "a"),
		contract.AgentChoice("root", "s", "b"),
		warning,
		lone,
		contract.AgentChoiceReasoning("root", "s", "r"),
	}

	merged := Merge(events)

	require.Len(t, merged, 4)
	assert.Equal(t, "ab", merged[0].(*contract.AgentChoiceEvent).Content)
	assert.Same(t, warning, merged[1])
	assert.Same(t, lone, merged[2], "a delta with nothing to merge with is passed through as-is")
	assert.Equal(t, "r", merged[3].(*contract.AgentChoiceReasoningEvent).Content)
}

func TestMergePreservesContentSessions(t *testing.T) {
	t.Parallel()

	for _, reasoning := range []bool{false, true} {
		name := "text"
		if reasoning {
			name = "reasoning"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			contentEvent := contract.AgentChoice
			if reasoning {
				contentEvent = contract.AgentChoiceReasoning
			}
			events := []contract.Event{
				contentEvent("root", "child", "child "),
				contentEvent("root", "child", "reply"),
				contentEvent("root", "parent", "parent "),
				contentEvent("root", "parent", "reply"),
				contentEvent("root", "child", "child again"),
				contentEvent("root", "", "legacy "),
				contentEvent("root", "", "reply"),
			}
			merged := Merge(events)
			require.Len(t, merged, 4)
			for i, want := range []struct {
				sessionID string
				content   string
				first     int
			}{
				{"child", "child reply", 0},
				{"parent", "parent reply", 2},
				{"child", "child again", 4},
				{"", "legacy reply", 5},
			} {
				if reasoning {
					got := merged[i].(*contract.AgentChoiceReasoningEvent)
					assert.Equal(t, want.sessionID, got.SessionID)
					assert.Equal(t, want.content, got.Content)
					assert.Equal(t, events[want.first].(*contract.AgentChoiceReasoningEvent).AgentContext, got.AgentContext)
				} else {
					got := merged[i].(*contract.AgentChoiceEvent)
					assert.Equal(t, want.sessionID, got.SessionID)
					assert.Equal(t, want.content, got.Content)
					assert.Equal(t, events[want.first].(*contract.AgentChoiceEvent).AgentContext, got.AgentContext)
				}
			}
		})
	}
}

func TestMergePreservesMessageIDs(t *testing.T) {
	t.Parallel()

	for _, reasoning := range []bool{false, true} {
		makeEvent := func(id, text string) contract.Event {
			if reasoning {
				return contract.AgentChoiceReasoning("root", "session", text, id)
			}
			return contract.AgentChoice("root", "session", text, id)
		}
		events := []contract.Event{makeEvent("one", "a"), makeEvent("one", "b"), makeEvent("two", "c"), makeEvent("", "d")}
		merged := Merge(events)
		require.Len(t, merged, 3)
		if reasoning {
			assert.Equal(t, "one", merged[0].(*contract.AgentChoiceReasoningEvent).MessageID)
			assert.Equal(t, "ab", merged[0].(*contract.AgentChoiceReasoningEvent).Content)
		} else {
			assert.Equal(t, "one", merged[0].(*contract.AgentChoiceEvent).MessageID)
			assert.Equal(t, "ab", merged[0].(*contract.AgentChoiceEvent).Content)
		}
	}
}

func TestMergeAcceptsUntypedSlices(t *testing.T) {
	t.Parallel()

	type other struct{}
	events := []any{contract.AgentChoice("root", "s", "a"), contract.AgentChoice("root", "s", "b"), other{}}

	merged := Merge(events)

	require.Len(t, merged, 2)
	assert.Equal(t, "ab", merged[0].(*contract.AgentChoiceEvent).Content)
	assert.Equal(t, other{}, merged[1])
}

// BenchmarkMergeAgentChoice measures merging a typical window's worth of
// streaming chunks. Repeated `merged.Content + next.Content` would be O(N^2)
// in the chunk count; the strings.Builder version is O(N).
func BenchmarkMergeAgentChoice(b *testing.B) {
	for _, n := range []int{16, 64, 256} {
		b.Run("chunks="+strconv.Itoa(n), func(b *testing.B) {
			events := buildAgentChoiceEvents(n)

			b.ReportAllocs()
			for b.Loop() {
				_ = Merge(events)
			}
		})
	}
}

// BenchmarkMergePartialToolCall measures the same for tool-call argument
// deltas, which use a structurally similar concatenation pattern.
func BenchmarkMergePartialToolCall(b *testing.B) {
	for _, n := range []int{16, 64, 256} {
		b.Run("chunks="+strconv.Itoa(n), func(b *testing.B) {
			events := buildPartialToolCallEvents(n)

			b.ReportAllocs()
			for b.Loop() {
				_ = Merge(events)
			}
		})
	}
}

func buildAgentChoiceEvents(n int) []contract.Event {
	const chunk = "the quick brown fox jumps over the lazy dog. "
	events := make([]contract.Event, 0, n)
	for range n {
		events = append(events, &contract.AgentChoiceEvent{
			Content:      chunk,
			AgentContext: contract.AgentContext{AgentName: "agent"},
		})
	}
	return events
}

func buildPartialToolCallEvents(n int) []contract.Event {
	const chunk = `,"key":"value"`
	events := make([]contract.Event, 0, n)
	for range n {
		events = append(events, &contract.PartialToolCallEvent{
			ToolCall: tools.ToolCall{
				ID:       "call-1",
				Function: tools.FunctionCall{Arguments: chunk},
			},
		})
	}
	return events
}
