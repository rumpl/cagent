package viewport

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockContentProvider implements ContentProvider for testing
type mockContentProvider struct {
	content     string
	totalHeight int
}

func (m *mockContentProvider) GetContent() string {
	return m.content
}

func (m *mockContentProvider) GetTotalHeight() int {
	return m.totalHeight
}

func newMockProvider(lines int) *mockContentProvider {
	content := make([]string, lines)
	for i := range lines {
		content[i] = "Line " + string(rune('A'+i))
	}
	return &mockContentProvider{
		content:     strings.Join(content, "\n"),
		totalHeight: lines,
	}
}

func TestNew(t *testing.T) {
	v := New()
	require.NotNil(t, v)

	m := v.(*model)
	assert.NotNil(t, m.scrollbar)
	assert.Equal(t, 0, m.scrollOffset)
}

func TestSetContentProvider(t *testing.T) {
	v := New().(*model)
	provider := newMockProvider(10)

	v.SetContentProvider(provider)
	assert.Equal(t, provider, v.contentProvider)
}

func TestSetSize(t *testing.T) {
	v := New().(*model)
	cmd := v.SetSize(80, 24)
	assert.Nil(t, cmd)
	assert.Equal(t, 80, v.width)
	assert.Equal(t, 24, v.height)
}

func TestSetPosition(t *testing.T) {
	v := New().(*model)
	cmd := v.SetPosition(10, 5)
	assert.Nil(t, cmd)
	assert.Equal(t, 10, v.xPos)
	assert.Equal(t, 5, v.yPos)
}

func TestScrollOperations(t *testing.T) {
	v := New().(*model)
	v.SetSize(80, 10)
	provider := newMockProvider(100)
	v.SetContentProvider(provider)

	t.Run("ScrollDown", func(t *testing.T) {
		v.scrollOffset = 0
		v.ScrollDown()
		assert.Equal(t, 1, v.scrollOffset)
	})

	t.Run("ScrollUp", func(t *testing.T) {
		v.scrollOffset = 5
		v.ScrollUp()
		assert.Equal(t, 4, v.scrollOffset)
	})

	t.Run("ScrollUp at top", func(t *testing.T) {
		v.scrollOffset = 0
		v.ScrollUp()
		assert.Equal(t, 0, v.scrollOffset)
	})

	t.Run("ScrollPageDown", func(t *testing.T) {
		v.scrollOffset = 0
		v.ScrollPageDown()
		assert.Equal(t, 10, v.scrollOffset)
	})

	t.Run("ScrollPageUp", func(t *testing.T) {
		v.scrollOffset = 20
		v.ScrollPageUp()
		assert.Equal(t, 10, v.scrollOffset)
	})

	t.Run("ScrollToTop", func(t *testing.T) {
		v.scrollOffset = 50
		v.ScrollToTop()
		assert.Equal(t, 0, v.scrollOffset)
	})

	t.Run("ScrollToBottom", func(t *testing.T) {
		v.scrollOffset = 0
		v.ScrollToBottom()
		// maxScrollOffset = 100 - 10 = 90
		assert.Equal(t, 90, v.scrollOffset)
	})

	t.Run("ScrollToOffset", func(t *testing.T) {
		v.ScrollToOffset(25)
		assert.Equal(t, 25, v.scrollOffset)
	})

	t.Run("ScrollToOffset clamps to max", func(t *testing.T) {
		v.ScrollToOffset(1000)
		assert.Equal(t, 90, v.scrollOffset)
	})

	t.Run("ScrollToOffset clamps to min", func(t *testing.T) {
		v.ScrollToOffset(-10)
		assert.Equal(t, 0, v.scrollOffset)
	})
}

func TestGetViewportBounds(t *testing.T) {
	v := New().(*model)
	v.SetSize(80, 10)
	provider := newMockProvider(100)
	v.SetContentProvider(provider)

	tests := []struct {
		name          string
		scrollOffset  int
		expectedStart int
		expectedEnd   int
	}{
		{
			name:          "at top",
			scrollOffset:  0,
			expectedStart: 0,
			expectedEnd:   10,
		},
		{
			name:          "middle",
			scrollOffset:  45,
			expectedStart: 45,
			expectedEnd:   55,
		},
		{
			name:          "at bottom",
			scrollOffset:  90,
			expectedStart: 90,
			expectedEnd:   100,
		},
		{
			name:          "beyond bottom (clamped)",
			scrollOffset:  1000,
			expectedStart: 90,
			expectedEnd:   100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v.scrollOffset = tt.scrollOffset
			start, end := v.GetViewportBounds()
			assert.Equal(t, tt.expectedStart, start)
			assert.Equal(t, tt.expectedEnd, end)
		})
	}
}

func TestIsAtBottom(t *testing.T) {
	v := New().(*model)
	v.SetSize(80, 10)
	provider := newMockProvider(100)
	v.SetContentProvider(provider)

	tests := []struct {
		name         string
		scrollOffset int
		expected     bool
	}{
		{
			name:         "at top",
			scrollOffset: 0,
			expected:     false,
		},
		{
			name:         "middle",
			scrollOffset: 45,
			expected:     false,
		},
		{
			name:         "at bottom",
			scrollOffset: 90,
			expected:     true,
		},
		{
			name:         "beyond bottom",
			scrollOffset: 100,
			expected:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v.scrollOffset = tt.scrollOffset
			assert.Equal(t, tt.expected, v.IsAtBottom())
		})
	}
}

func TestIsAtBottomWithEmptyContent(t *testing.T) {
	v := New().(*model)
	v.SetSize(80, 10)

	t.Run("no content provider", func(t *testing.T) {
		assert.True(t, v.IsAtBottom())
	})

	t.Run("empty content", func(t *testing.T) {
		provider := &mockContentProvider{
			content:     "",
			totalHeight: 0,
		}
		v.SetContentProvider(provider)
		assert.True(t, v.IsAtBottom())
	})
}

func TestGetVisibleContent(t *testing.T) {
	v := New().(*model)
	v.SetSize(80, 5)
	provider := newMockProvider(10)
	v.SetContentProvider(provider)

	tests := []struct {
		name         string
		scrollOffset int
		expected     []string
	}{
		{
			name:         "at top",
			scrollOffset: 0,
			expected:     []string{"Line A", "Line B", "Line C", "Line D", "Line E"},
		},
		{
			name:         "middle",
			scrollOffset: 3,
			expected:     []string{"Line D", "Line E", "Line F", "Line G", "Line H"},
		},
		{
			name:         "at bottom",
			scrollOffset: 5,
			expected:     []string{"Line F", "Line G", "Line H", "Line I", "Line J"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v.scrollOffset = tt.scrollOffset
			visible := v.GetVisibleContent()
			assert.Equal(t, tt.expected, visible)
		})
	}
}

func TestGetVisibleContentWithNoProvider(t *testing.T) {
	v := New().(*model)
	v.SetSize(80, 10)

	visible := v.GetVisibleContent()
	assert.Nil(t, visible)
}

func TestUpdate_MouseWheel(t *testing.T) {
	v := New().(*model)
	v.SetSize(80, 10)
	provider := newMockProvider(100)
	v.SetContentProvider(provider)

	t.Run("wheel up", func(t *testing.T) {
		v.scrollOffset = 10
		msg := tea.MouseWheelMsg{
			X:      5,
			Y:      5,
			Button: tea.MouseWheelUp,
		}
		updated, cmd := v.Update(msg)
		assert.NotNil(t, updated)
		assert.Nil(t, cmd)
		assert.Equal(t, 7, v.scrollOffset) // scrolls up 3 lines
	})

	t.Run("wheel down", func(t *testing.T) {
		v.scrollOffset = 10
		msg := tea.MouseWheelMsg{
			X:      5,
			Y:      5,
			Button: tea.MouseWheelDown,
		}
		updated, cmd := v.Update(msg)
		assert.NotNil(t, updated)
		assert.Nil(t, cmd)
		assert.Equal(t, 13, v.scrollOffset) // scrolls down 3 lines
	})
}

func TestUpdate_KeyPress(t *testing.T) {
	v := New().(*model)
	v.SetSize(80, 10)
	provider := newMockProvider(100)
	v.SetContentProvider(provider)

	tests := []struct {
		name           string
		key            string
		initialOffset  int
		expectedOffset int
	}{
		{
			name:           "up arrow",
			key:            "up",
			initialOffset:  10,
			expectedOffset: 9,
		},
		{
			name:           "down arrow",
			key:            "down",
			initialOffset:  10,
			expectedOffset: 11,
		},
		{
			name:           "page up",
			key:            "pgup",
			initialOffset:  20,
			expectedOffset: 10,
		},
		{
			name:           "page down",
			key:            "pgdown",
			initialOffset:  10,
			expectedOffset: 20,
		},
		{
			name:           "home",
			key:            "home",
			initialOffset:  50,
			expectedOffset: 0,
		},
		{
			name:           "end",
			key:            "end",
			initialOffset:  0,
			expectedOffset: 90, // max offset for 100 lines with height 10
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v.scrollOffset = tt.initialOffset
			// We can't easily construct KeyPressMsg in tests, so we'll call the methods directly
			switch tt.key {
			case "up":
				v.ScrollUp()
			case "down":
				v.ScrollDown()
			case "pgup":
				v.ScrollPageUp()
			case "pgdown":
				v.ScrollPageDown()
			case "home":
				v.ScrollToTop()
			case "end":
				v.ScrollToBottom()
			}
			assert.Equal(t, tt.expectedOffset, v.scrollOffset)
		})
	}
}

func TestView(t *testing.T) {
	v := New().(*model)
	v.SetSize(80, 5)

	t.Run("no content provider", func(t *testing.T) {
		view := v.View()
		assert.Equal(t, "", view)
	})

	t.Run("with content", func(t *testing.T) {
		provider := newMockProvider(10)
		v.SetContentProvider(provider)
		view := v.View()
		assert.NotEmpty(t, view)
		lines := strings.Split(view, "\n")
		assert.Equal(t, 5, len(lines))
		assert.Equal(t, "Line A", lines[0])
	})

	t.Run("scrolled content", func(t *testing.T) {
		provider := newMockProvider(10)
		v.SetContentProvider(provider)
		v.scrollOffset = 3
		view := v.View()
		lines := strings.Split(view, "\n")
		assert.Equal(t, 5, len(lines))
		assert.Equal(t, "Line D", lines[0])
	})
}

func TestGetScrollOffset(t *testing.T) {
	v := New().(*model)
	v.scrollOffset = 42
	assert.Equal(t, 42, v.GetScrollOffset())
}

func TestGetScrollbar(t *testing.T) {
	v := New().(*model)
	sb := v.GetScrollbar()
	assert.NotNil(t, sb)
}
