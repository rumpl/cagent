package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/tools"
)

func testToolCall(id, name, args string) tools.ToolCall {
	return tools.ToolCall{
		ID:   id,
		Type: "function",
		Function: tools.FunctionCall{
			Name:      name,
			Arguments: args,
		},
	}
}

func TestEnsureToolDefinitionUsesCallNameWhenMissing(t *testing.T) {
	t.Parallel()

	toolDef := EnsureToolDefinition(testToolCall("id", "shell", "{}"), tools.Tool{})

	assert.Equal(t, "shell", toolDef.Name)

	existing := EnsureToolDefinition(testToolCall("id", "shell", "{}"), tools.Tool{Name: "custom"})
	assert.Equal(t, "custom", existing.Name)
}

func TestToolViewMessageAndImages(t *testing.T) {
	t.Parallel()

	view := NewToolView("agent", testToolCall("id", "shell", "{}"), tools.Tool{}, 0)
	view.SetImages([]InlineImage{{Name: "img", PNGData: []byte("png"), Width: 1, Height: 1}})

	require.NotNil(t, view.Message())
	assert.Equal(t, "agent", view.Message().Sender)
	assert.Equal(t, "shell", view.Message().ToolDefinition.Name)
	assert.Len(t, view.images, 1)

	var nilView *ToolView
	assert.Nil(t, nilView.Message())
	nilView.SetImages([]InlineImage{{Name: "ignored"}})
}

func TestRenderToolHandlesNilAndTinyWidth(t *testing.T) {
	t.Parallel()

	assert.Nil(t, RenderToolWithState(nil, 0, 0, nil))
	assert.Nil(t, RenderToolWithState(&ToolView{}, 0, 0, nil))

	lines := RenderTool(*NewToolView("agent", testToolCall("id", "shell", `{"cmd":"echo hi"}`), tools.Tool{}, 20), 20)
	assert.NotEmpty(t, lines)
	for _, line := range lines {
		assert.LessOrEqual(t, DisplayWidth(line), 20)
	}
}

func TestRenderToolWithImagesAppendsImageLines(t *testing.T) {
	t.Parallel()

	view := NewToolView("agent", testToolCall("id", "shell", "{}"), tools.Tool{}, 0)
	view.SetImages([]InlineImage{{Name: "sample.png", PNGData: testPNGData(t), Width: 2, Height: 1}})

	lines := RenderToolWithState(view, 50, 0, nil)

	assert.Contains(t, ansi.Strip(strings.Join(lines, "\n")), "sample.png")
}

func TestPendingToolKeepsLastLinesWhenNextRenderShrinks(t *testing.T) {
	t.Parallel()

	view := NewToolView("agent", testToolCall("id", "shell", "{}"), tools.Tool{}, 0)
	view.lastWidth = 20
	view.lastLines = []string{"previous wider line"}

	assert.True(t, view.shouldKeepLastPendingLines(20, nil))
	assert.Equal(t, []string{"previous wider line"}, cloneLines(view.lastLines))
}

func TestSplitRenderedToolWrapsLongLinesAndIgnoresEmpty(t *testing.T) {
	t.Parallel()

	assert.Nil(t, splitRenderedTool("\n", 10))
	lines := splitRenderedTool("abcdef", 3)

	assert.Equal(t, []string{"abc", "def"}, lines)
}

func TestRenderToolOutputLimitsAndWraps(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	for range MaxToolOutputLines + 2 {
		b.WriteString("line")
		b.WriteString("\n")
	}

	lines := RenderToolOutput(b.String(), 8)

	assert.Contains(t, ansi.Strip(lines[0]), "2 earlier lines")
	assert.Len(t, lines, MaxToolOutputLines+1)
	for _, line := range lines[1:] {
		assert.LessOrEqual(t, DisplayWidth(line), 8)
	}
}

func TestRenderToolBoxAndContentWidthHelpers(t *testing.T) {
	t.Parallel()

	assert.Empty(t, renderToolBox("\n", 20))
	boxed := renderToolBox("content", 20)
	assert.NotEmpty(t, boxed)
	assert.Positive(t, totalContentWidth([]string{boxed}))
	assert.Equal(t, []string{"a", "b"}, cloneLines([]string{"a", "b"}))
}
