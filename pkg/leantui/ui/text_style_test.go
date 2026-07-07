package ui

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
)

func TestTextHelpersHandleANSIRunesAndBounds(t *testing.T) {
	t.Parallel()

	styled := StAccent().Render("hello")
	assert.Equal(t, 5, DisplayWidth(styled))
	assert.Equal(t, 1, RuneWidth('\t'))
	assert.Equal(t, 2, RuneWidth('界'))

	assert.Empty(t, Truncate("hello", 0))
	assert.Equal(t, "hello", Truncate("hello", 10))
	assert.LessOrEqual(t, DisplayWidth(Truncate("hello", 3)), 3)

	assert.Equal(t, "hi   ", PadRight("hi", 5))
	assert.Equal(t, "hello", PadRight("hello", 2))
}

func TestWrapANSIExpandsTabsAndPreservesStyleWidth(t *testing.T) {
	t.Parallel()

	lines := WrapANSI(StAccent().Render("a\tbcd"), 3)

	assert.NotEmpty(t, lines)
	for _, line := range lines {
		assert.LessOrEqual(t, DisplayWidth(line), 3)
		assert.NotContains(t, ansi.Strip(line), "\t")
	}
}

func TestStyleHelpersReturnStyles(t *testing.T) {
	t.Parallel()

	styles := []string{
		StPrimary().Render("x"),
		StBold().Render("x"),
		StError().Render("x"),
		StWarning().Render("x"),
		StSuccess().Render("x"),
		StReasoning().Render("x"),
		StToolBox(0).Render("x"),
		StToolBox(3).Render("x"),
	}

	for _, rendered := range styles {
		assert.Contains(t, ansi.Strip(rendered), "x")
	}
}
