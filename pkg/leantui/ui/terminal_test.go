package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeTerminalSizeDefaultsInvalidValues(t *testing.T) {
	t.Parallel()

	w, h := normalizeTerminalSize(0, -1)

	assert.Equal(t, 80, w)
	assert.Equal(t, 24, h)

	w, h = normalizeTerminalSize(120, 40)
	assert.Equal(t, 120, w)
	assert.Equal(t, 40, h)
}

func TestSendLatestResizeKeepsNewestValue(t *testing.T) {
	t.Parallel()

	ch := make(chan [2]int, 1)
	sendLatestResize(ch, [2]int{80, 24})
	sendLatestResize(ch, [2]int{100, 30})

	assert.Equal(t, [2]int{100, 30}, <-ch)
}

func TestTerminalAccessorsAndWriteFlush(t *testing.T) {
	t.Parallel()

	tty := &Terminal{width: 90, height: 31}
	w, h := tty.Size()
	assert.Equal(t, 90, w)
	assert.Equal(t, 31, h)
	assert.Nil(t, tty.Reader())
	assert.Nil(t, tty.Writer())
}
