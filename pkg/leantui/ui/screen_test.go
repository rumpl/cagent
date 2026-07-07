package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/tui/service"
)

func TestNewScreenInitializesModels(t *testing.T) {
	t.Parallel()

	s := NewScreen("/work", "main", "ask")

	require.NotNil(t, s.Transcript)
	require.NotNil(t, s.Editor)
	require.NotNil(t, s.Autocomplete)
	assert.Equal(t, "/work", s.Status.WorkingDir)
	assert.Equal(t, "main", s.Status.Branch)
	assert.Equal(t, "ask", s.Editor.placeholder)
}

func TestScreenFrameWithEditor(t *testing.T) {
	t.Parallel()

	s := NewScreen("/work", "main", "ask")
	s.Transcript.AddBlock(func(width int) []string { return []string{"hello"} })
	s.Editor.SetText("input")

	lines, cursorLine, cursorCol := s.Frame(60, 20, 0, false, service.StaticSessionState{}, nil)
	joined := ansi.Strip(strings.Join(lines, "\n"))

	assert.Contains(t, joined, "hello")
	assert.Contains(t, joined, PromptText+"input")
	assert.Contains(t, joined, "/work")
	assert.Equal(t, 2, cursorLine)
	assert.Equal(t, PromptWidth+len("input"), cursorCol)
}

func TestScreenFrameWithConfirm(t *testing.T) {
	t.Parallel()

	s := NewScreen("/work", "main", "ask")
	s.Confirm = &ConfirmModel{Tool: "shell", View: *NewToolView("agent", testToolCall("id", "shell", "{}"), testToolCallTool(), 0)}

	lines, cursorLine, cursorCol := s.Frame(50, 20, 0, false, service.StaticSessionState{}, nil)
	joined := ansi.Strip(strings.Join(lines, "\n"))

	assert.Contains(t, joined, "Approve tool call")
	assert.Contains(t, joined, "[y] yes")
	assert.GreaterOrEqual(t, cursorLine, 0)
	assert.Positive(t, cursorCol)
	for _, line := range lines {
		assert.LessOrEqual(t, DisplayWidth(line), 50)
	}
}

func TestConfirmModelRenderIncludesToolAndActions(t *testing.T) {
	t.Parallel()

	confirm := ConfirmModel{Tool: "shell", View: *NewToolView("agent", testToolCall("id", "shell", "{}"), testToolCallTool(), 0)}
	lines := confirm.Render(40)

	joined := ansi.Strip(strings.Join(lines, "\n"))
	assert.Contains(t, joined, "Approve tool call")
	assert.Contains(t, joined, "[s]")
	for _, line := range lines {
		assert.LessOrEqual(t, DisplayWidth(line), 40)
	}
}
