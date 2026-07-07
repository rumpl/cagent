package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/tools"
	"github.com/docker/docker-agent/pkg/tui/service"
)

func TestTranscriptPendingFlushAndCache(t *testing.T) {
	t.Parallel()

	tr := NewTranscript()
	tr.AppendReasoning(" thinking ")
	assert.NotEmpty(t, tr.Lines(40, 0, false, nil, nil))

	tr.AppendAssistant("hello")
	assert.Equal(t, 1, tr.BlockCount())
	assert.Nil(t, tr.BlockLines(-1, 40))
	first := tr.BlockLines(0, 40)
	second := tr.BlockLines(0, 40)
	assert.Equal(t, first, second)

	tr.FlushPending()
	assert.Equal(t, 2, tr.BlockCount())
	assert.NotEmpty(t, tr.BlockLines(1, 40))
}

func TestTranscriptLinesShowsSpinnerAndPendingUsers(t *testing.T) {
	t.Parallel()

	tr := NewTranscript()
	lines := tr.Lines(40, 1, true, service.StaticSessionState{}, []PendingUserMessage{{Display: "queued", Content: "raw", Kind: PendingUserFollowUp}})
	joined := ansi.Strip(strings.Join(lines, "\n"))

	assert.Contains(t, joined, "Working")
	assert.Contains(t, joined, PromptText+"queued")
}

func TestTranscriptClearActiveAndClear(t *testing.T) {
	t.Parallel()

	tr := NewTranscript()
	tr.AddBlock(func(width int) []string { return []string{"committed"} })
	tr.AppendAssistant("pending")
	tr.UpsertTool("agent", testToolCall("id", "shell", "{}"), testToolCallTool(), 0)

	tr.ClearActive()
	assert.Equal(t, 1, tr.BlockCount())
	assert.Equal(t, 0, tr.ToolCount())
	assert.NotContains(t, ansi.Strip(strings.Join(tr.Lines(40, 0, false, nil, nil), "\n")), "pending")

	tr.Clear()
	assert.Equal(t, 0, tr.BlockCount())
	assert.Empty(t, tr.Lines(40, 0, false, nil, nil))
}

func TestTranscriptRemoveTool(t *testing.T) {
	t.Parallel()

	tr := NewTranscript()
	tr.UpsertTool("agent", testToolCall("id", "shell", "{}"), testToolCallTool(), 0)
	require.Equal(t, 1, tr.ToolCount())

	tr.RemoveTool("id")
	assert.Equal(t, 0, tr.ToolCount())
}

func TestSpinnerLineWrapsFrames(t *testing.T) {
	t.Parallel()

	line := ansi.Strip(spinnerLine(len(spinnerFrames) + 2))

	assert.Contains(t, line, "Working")
	assert.Contains(t, line, spinnerFrames[2])
}

func testToolCallTool() tools.Tool { return tools.Tool{Name: "shell"} }
