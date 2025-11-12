package textselector

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	ts := New()
	require.NotNil(t, ts)
	assert.False(t, ts.IsActive())
}

func TestHandleMouseDown(t *testing.T) {
	ts := New().(*model)

	ts.HandleMouseDown(10, 5)

	assert.True(t, ts.selection.active)
	assert.True(t, ts.selection.mouseButtonDown)
	assert.Equal(t, 5, ts.selection.startLine)
	assert.Equal(t, 10, ts.selection.startCol)
	assert.Equal(t, 5, ts.selection.endLine)
	assert.Equal(t, 10, ts.selection.endCol)
	assert.Equal(t, 5, ts.selection.mouseY)
}

func TestHandleMouseMove(t *testing.T) {
	ts := New().(*model)
	ts.HandleMouseDown(10, 5)

	ts.HandleMouseMove(20, 10)

	assert.Equal(t, 5, ts.selection.startLine)
	assert.Equal(t, 10, ts.selection.startCol)
	assert.Equal(t, 10, ts.selection.endLine)
	assert.Equal(t, 20, ts.selection.endCol)
	assert.Equal(t, 10, ts.selection.mouseY)
}

func TestHandleMouseMove_NoEffect_WhenNotActive(t *testing.T) {
	ts := New().(*model)

	ts.HandleMouseMove(20, 10)

	assert.False(t, ts.selection.active)
	assert.Equal(t, 0, ts.selection.endLine)
	assert.Equal(t, 0, ts.selection.endCol)
}

func TestHandleMouseUp(t *testing.T) {
	ts := New().(*model)
	ts.HandleMouseDown(10, 5)
	ts.HandleMouseMove(20, 10)

	ts.HandleMouseUp(25, 12)

	assert.True(t, ts.selection.active)
	assert.False(t, ts.selection.mouseButtonDown)
	assert.Equal(t, 12, ts.selection.endLine)
	assert.Equal(t, 25, ts.selection.endCol)
}

func TestIsActive(t *testing.T) {
	ts := New()

	assert.False(t, ts.IsActive())

	ts.HandleMouseDown(10, 5)
	assert.True(t, ts.IsActive())

	ts.Clear()
	assert.False(t, ts.IsActive())
}

func TestIsMouseButtonDown(t *testing.T) {
	ts := New()

	assert.False(t, ts.IsMouseButtonDown())

	ts.HandleMouseDown(10, 5)
	assert.True(t, ts.IsMouseButtonDown())

	ts.HandleMouseUp(15, 8)
	assert.False(t, ts.IsMouseButtonDown())
}

func TestClear(t *testing.T) {
	ts := New().(*model)
	ts.HandleMouseDown(10, 5)
	ts.HandleMouseMove(20, 10)

	ts.Clear()

	assert.False(t, ts.selection.active)
	assert.False(t, ts.selection.mouseButtonDown)
	assert.Equal(t, 0, ts.selection.startLine)
	assert.Equal(t, 0, ts.selection.startCol)
	assert.Equal(t, 0, ts.selection.endLine)
	assert.Equal(t, 0, ts.selection.endCol)
}

func TestGetMouseY(t *testing.T) {
	ts := New()
	ts.HandleMouseDown(10, 5)

	assert.Equal(t, 5, ts.GetMouseY())

	ts.HandleMouseMove(20, 15)
	assert.Equal(t, 15, ts.GetMouseY())
}

func TestGetSelectedText_SingleLine(t *testing.T) {
	ts := New().(*model)
	content := "Hello World\nSecond Line\nThird Line"

	// Select "World" (cols 6-11 on line 0)
	ts.selection.active = true
	ts.selection.startLine = 0
	ts.selection.startCol = 6
	ts.selection.endLine = 0
	ts.selection.endCol = 11

	result := ts.GetSelectedText(content)
	assert.Contains(t, result, "World")
}

func TestGetSelectedText_MultiLine(t *testing.T) {
	ts := New().(*model)
	content := "Line One\nLine Two\nLine Three"

	// Select from middle of line 0 to middle of line 2
	ts.selection.active = true
	ts.selection.startLine = 0
	ts.selection.startCol = 5
	ts.selection.endLine = 2
	ts.selection.endCol = 9

	result := ts.GetSelectedText(content)
	assert.Contains(t, result, "One")
	assert.Contains(t, result, "Line Two")
	assert.Contains(t, result, "Line Thre")
}

func TestGetSelectedText_ReverseDirection(t *testing.T) {
	ts := New().(*model)
	content := "Hello World\nSecond Line"

	// Select backwards (end before start)
	ts.selection.active = true
	ts.selection.startLine = 1
	ts.selection.startCol = 10
	ts.selection.endLine = 0
	ts.selection.endCol = 5

	result := ts.GetSelectedText(content)
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "World")
}

func TestGetSelectedText_NotActive(t *testing.T) {
	ts := New().(*model)
	content := "Hello World"

	result := ts.GetSelectedText(content)
	assert.Empty(t, result)
}

func TestApplyHighlight_NoSelection(t *testing.T) {
	ts := New()
	lines := []string{"Line 1", "Line 2", "Line 3"}

	result := ts.ApplyHighlight(lines, 0)
	assert.Equal(t, lines, result)
}

func TestApplyHighlight_SingleLine(t *testing.T) {
	ts := New().(*model)
	lines := []string{"Hello World", "Line 2", "Line 3"}

	ts.selection.active = true
	ts.selection.startLine = 0
	ts.selection.startCol = 0
	ts.selection.endLine = 0
	ts.selection.endCol = 5

	result := ts.ApplyHighlight(lines, 0)

	// First line should be highlighted
	assert.NotEqual(t, lines[0], result[0])
	// Other lines unchanged
	assert.Equal(t, lines[1], result[1])
	assert.Equal(t, lines[2], result[2])
}

func TestApplyHighlight_MultiLine(t *testing.T) {
	ts := New().(*model)
	lines := []string{"Line 0", "Line 1", "Line 2", "Line 3"}

	ts.selection.active = true
	ts.selection.startLine = 1
	ts.selection.startCol = 0
	ts.selection.endLine = 2
	ts.selection.endCol = 4

	result := ts.ApplyHighlight(lines, 0)

	// Line 0 unchanged
	assert.Equal(t, lines[0], result[0])
	// Lines 1 and 2 should be highlighted
	assert.NotEqual(t, lines[1], result[1])
	assert.NotEqual(t, lines[2], result[2])
	// Line 3 unchanged
	assert.Equal(t, lines[3], result[3])
}

func TestApplyHighlight_WithViewportOffset(t *testing.T) {
	ts := New().(*model)
	lines := []string{"Line 10", "Line 11", "Line 12"}

	// Selection on absolute line 11 (second visible line)
	ts.selection.active = true
	ts.selection.startLine = 11
	ts.selection.startCol = 0
	ts.selection.endLine = 11
	ts.selection.endCol = 4

	// Viewport starts at line 10
	result := ts.ApplyHighlight(lines, 10)

	// Only the second line should be highlighted
	assert.Equal(t, lines[0], result[0])
	assert.NotEqual(t, lines[1], result[1])
	assert.Equal(t, lines[2], result[2])
}

func TestGetAutoScrollDirection(t *testing.T) {
	ts := New()

	tests := []struct {
		name           string
		mouseY         int
		viewportHeight int
		expected       int
	}{
		{
			name:           "top threshold",
			mouseY:         1,
			viewportHeight: 20,
			expected:       -1,
		},
		{
			name:           "middle (no scroll)",
			mouseY:         10,
			viewportHeight: 20,
			expected:       0,
		},
		{
			name:           "bottom threshold",
			mouseY:         18,
			viewportHeight: 20,
			expected:       1,
		},
		{
			name:           "exactly at bottom threshold",
			mouseY:         18,
			viewportHeight: 20,
			expected:       1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ts.GetAutoScrollDirection(tt.mouseY, tt.viewportHeight)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestUpdateSelectionForScroll(t *testing.T) {
	ts := New().(*model)
	ts.selection.endLine = 10

	t.Run("scroll up", func(t *testing.T) {
		ts.selection.endLine = 10
		ts.UpdateSelectionForScroll(-1)
		assert.Equal(t, 9, ts.selection.endLine)
	})

	t.Run("scroll down", func(t *testing.T) {
		ts.selection.endLine = 10
		ts.UpdateSelectionForScroll(1)
		assert.Equal(t, 11, ts.selection.endLine)
	})

	t.Run("no scroll", func(t *testing.T) {
		ts.selection.endLine = 10
		ts.UpdateSelectionForScroll(0)
		assert.Equal(t, 10, ts.selection.endLine)
	})

	t.Run("scroll up at boundary", func(t *testing.T) {
		ts.selection.endLine = 0
		ts.UpdateSelectionForScroll(-1)
		assert.Equal(t, 0, ts.selection.endLine) // clamped to 0
	})
}

func TestHighlightLine(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		startCol int
		endCol   int
	}{
		{
			name:     "basic highlight",
			line:     "Hello World",
			startCol: 0,
			endCol:   5,
		},
		{
			name:     "middle portion",
			line:     "Hello World",
			startCol: 6,
			endCol:   11,
		},
		{
			name:     "invalid range",
			line:     "Hello",
			startCol: 10,
			endCol:   15,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := highlightLine(tt.line, tt.startCol, tt.endCol)
			// Just verify it doesn't panic and returns something
			assert.NotEmpty(t, result)
		})
	}
}

func TestDisplayWidthToRuneIndex(t *testing.T) {
	tests := []struct {
		name        string
		text        string
		targetWidth int
		expected    int
	}{
		{
			name:        "ASCII text",
			text:        "Hello",
			targetWidth: 3,
			expected:    3,
		},
		{
			name:        "zero width",
			text:        "Hello",
			targetWidth: 0,
			expected:    0,
		},
		{
			name:        "beyond end",
			text:        "Hi",
			targetWidth: 10,
			expected:    2,
		},
		{
			name:        "negative width",
			text:        "Hello",
			targetWidth: -5,
			expected:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := displayWidthToRuneIndex(tt.text, tt.targetWidth)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAutoScrollTick(t *testing.T) {
	t.Run("no scroll", func(t *testing.T) {
		cmd := AutoScrollTick(0)
		assert.Nil(t, cmd)
	})

	t.Run("scroll up", func(t *testing.T) {
		cmd := AutoScrollTick(-1)
		assert.NotNil(t, cmd)
	})

	t.Run("scroll down", func(t *testing.T) {
		cmd := AutoScrollTick(1)
		assert.NotNil(t, cmd)
	})
}

func TestGetSelectedText_WithANSICodes(t *testing.T) {
	ts := New().(*model)
	// Content with ANSI color codes
	content := "\x1b[31mRed Text\x1b[0m\n\x1b[32mGreen Text\x1b[0m"

	ts.selection.active = true
	ts.selection.startLine = 0
	ts.selection.startCol = 0
	ts.selection.endLine = 0
	ts.selection.endCol = 8

	result := ts.GetSelectedText(content)
	// ANSI codes should be stripped
	assert.Contains(t, strings.TrimSpace(result), "Red Text")
	assert.NotContains(t, result, "\x1b[31m")
}
