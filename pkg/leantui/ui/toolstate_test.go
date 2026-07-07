package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/tools"
	"github.com/docker/docker-agent/pkg/tui/service"
	tuitypes "github.com/docker/docker-agent/pkg/tui/types"
)

func TestToolTrackerUpsertCreatesUpdatesAndOrders(t *testing.T) {
	t.Parallel()

	tracker := NewToolTracker()
	assert.True(t, tracker.Empty())

	tracker.Upsert("agent", testToolCall("id1", "shell", "{"), tools.Tool{}, tuitypes.ToolStatusPending)
	tracker.Upsert("agent", testToolCall("id2", "read", "{}"), tools.Tool{Name: "read"}, tuitypes.ToolStatusRunning)
	tracker.Upsert("agent2", testToolCall("id1", "shell", `"cmd":"echo"}`), tools.Tool{}, tuitypes.ToolStatusPending)

	assert.False(t, tracker.Empty())
	assert.Equal(t, 2, tracker.Len())
	assert.Equal(t, 2, tracker.ByIDLen())
	require.NotNil(t, tracker.Get("id1"))
	assert.JSONEq(t, `{"cmd":"echo"}`, tracker.Get("id1").Message().ToolCall.Function.Arguments)
	assert.Equal(t, "agent2", tracker.Get("id1").Message().Sender)
	require.NotNil(t, tracker.Get("id2").Message().StartedAt)

	var names []string
	tracker.ForEach(func(tv *ToolView) { names = append(names, tv.Message().ToolCall.Function.Name) })
	assert.Equal(t, []string{"shell", "read"}, names)
}

func TestToolTrackerUpsertRebuildsNilMessage(t *testing.T) {
	t.Parallel()

	tracker := NewToolTracker()
	tracker.byID["id"] = &ToolView{}
	tracker.order = []string{"id"}

	tracker.Upsert("agent", testToolCall("id", "shell", "{}"), tools.Tool{}, tuitypes.ToolStatusCompleted)

	require.NotNil(t, tracker.Get("id").Message())
	assert.Equal(t, "shell", tracker.Get("id").Message().ToolDefinition.Name)
}

func TestToolTrackerRemoveAndReset(t *testing.T) {
	t.Parallel()

	tracker := NewToolTracker()
	tracker.Upsert("agent", testToolCall("id", "shell", "{}"), tools.Tool{}, tuitypes.ToolStatusPending)
	tracker.Remove("")
	assert.Equal(t, 1, tracker.Len())

	tracker.Remove("id")
	assert.True(t, tracker.Empty())
	assert.Equal(t, 0, tracker.ByIDLen())

	tracker.Upsert("agent", testToolCall("id", "shell", "{}"), tools.Tool{}, tuitypes.ToolStatusPending)
	tracker.Reset()
	assert.True(t, tracker.Empty())
}

func TestToolTrackerFinishSnapshotsAndRemoves(t *testing.T) {
	t.Parallel()

	tracker := NewToolTracker()
	tracker.Upsert("agent", testToolCall("id", "shell", "{}"), tools.Tool{}, tuitypes.ToolStatusRunning)

	finished := tracker.Finish("id", ToolResult{
		Response:       "ok\tthere",
		Result:         tools.ResultSuccess("payload"),
		ToolDefinition: tools.Tool{Name: "shell"},
		Images:         []InlineImage{{Name: "img"}},
	})

	require.NotNil(t, finished)
	assert.Equal(t, tuitypes.ToolStatusCompleted, finished.Message().ToolStatus)
	assert.Equal(t, "ok    there", finished.Message().Content)
	require.NotNil(t, finished.Message().ToolResult)
	assert.Empty(t, finished.Message().ToolResult.Output)
	assert.Len(t, finished.images, 1)
	assert.True(t, tracker.Empty())
}

func TestToolTrackerFinishCreatesMissingAndHandlesErrors(t *testing.T) {
	t.Parallel()

	tracker := NewToolTracker()
	finished := tracker.Finish("missing", ToolResult{
		AgentName:      "agent",
		ToolDefinition: tools.Tool{Name: "shell"},
		Result:         tools.ResultError("boom"),
	})

	require.NotNil(t, finished)
	assert.Equal(t, "missing", finished.Message().ToolCall.ID)
	assert.Equal(t, tuitypes.ToolStatusError, finished.Message().ToolStatus)
}

func TestToolViewIDFallsBackToFunctionName(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "id", ToolViewID(testToolCall("id", "shell", "{}")))
	assert.Equal(t, "shell", ToolViewID(testToolCall("", "shell", "{}")))
}

func TestTranscriptToolLifecycle(t *testing.T) {
	t.Parallel()

	tr := NewTranscript()
	tr.UpsertTool("agent", testToolCall("id", "shell", "{}"), tools.Tool{}, tuitypes.ToolStatusPending)
	assert.Equal(t, 1, tr.ToolCount())
	assert.Equal(t, 1, tr.ToolByIDCount())
	require.NotNil(t, tr.Tool("id"))

	tr.FinishTool("id", ToolResult{Result: tools.ResultSuccess("ok"), ToolDefinition: tools.Tool{Name: "shell"}}, service.StaticSessionState{})
	assert.Equal(t, 0, tr.ToolCount())
	assert.Equal(t, 1, tr.BlockCount())
	assert.NotEmpty(t, tr.BlockLines(0, 40))
}
