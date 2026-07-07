package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderUserLinesWrapsWithPromptAndContinuation(t *testing.T) {
	t.Parallel()

	lines := RenderUserLines("abcd efgh\n", PromptWidth+4)

	require.Len(t, lines, 2)
	assert.Contains(t, ansi.Strip(lines[0]), PromptText)
	assert.Contains(t, ansi.Strip(lines[0]), "abcd")
	assert.Equal(t, Continuation+"efgh", ansi.Strip(lines[1]))
	for _, line := range lines {
		assert.LessOrEqual(t, DisplayWidth(line), PromptWidth+4)
	}
}

func TestRenderPendingUserLinesUsesUserLayout(t *testing.T) {
	t.Parallel()

	lines := RenderPendingUserLines("queued", 20)

	require.Len(t, lines, 1)
	assert.Contains(t, ansi.Strip(lines[0]), PromptText+"queued")
	assert.LessOrEqual(t, DisplayWidth(lines[0]), 20)
}

func TestRenderReasoningLinesTrimsEmptyAndWraps(t *testing.T) {
	t.Parallel()

	assert.Nil(t, RenderReasoningLines(" \n\t", 20))
	lines := RenderReasoningLines("  thinking hard  ", 10)

	assert.NotEmpty(t, lines)
	for _, line := range lines {
		assert.True(t, strings.HasPrefix(ansi.Strip(line), "  "))
		assert.LessOrEqual(t, DisplayWidth(line), 10)
	}
}

func TestRenderAssistantLinesMarkdownAndFallbackWidth(t *testing.T) {
	t.Parallel()

	lines := RenderAssistantLines("# Title\n\nhello world", 12)

	assert.NotEmpty(t, lines)
	for _, line := range lines {
		assert.LessOrEqual(t, DisplayWidth(line), 12)
	}
	assert.Nil(t, RenderAssistantLines(" \n\t", 12))
}

func TestRenderNoticeLinesWrapsAndIndentsContinuations(t *testing.T) {
	t.Parallel()

	lines := RenderNoticeLines("! ", "abcdef", 6, StWarning())

	require.Len(t, lines, 2)
	assert.Equal(t, "! abcd", ansi.Strip(lines[0]))
	assert.Equal(t, "  ef", ansi.Strip(lines[1]))
	for _, line := range lines {
		assert.LessOrEqual(t, DisplayWidth(line), 6)
	}
}
