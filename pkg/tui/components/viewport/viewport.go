package viewport

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/docker/cagent/pkg/tui/components/scrollbar"
	"github.com/docker/cagent/pkg/tui/core/layout"
)

// ContentProvider provides content for the viewport to display
type ContentProvider interface {
	// GetContent returns the complete rendered content
	GetContent() string

	// GetTotalHeight returns total content height in lines
	GetTotalHeight() int
}

// Model represents a scrollable viewport component
type Model interface {
	layout.Model
	layout.Sizeable
	layout.Positionable

	// SetContentProvider sets the content provider
	SetContentProvider(provider ContentProvider)

	// Scroll operations
	ScrollUp()
	ScrollDown()
	ScrollPageUp()
	ScrollPageDown()
	ScrollToTop()
	ScrollToBottom()
	ScrollToOffset(offset int)

	// Query viewport state
	GetScrollOffset() int
	GetViewportBounds() (start, end int)
	IsAtBottom() bool

	// Get visible content (extracted from full content)
	GetVisibleContent() []string
}

// model implements Model
type model struct {
	contentProvider ContentProvider
	scrollbar       *scrollbar.Model

	width        int
	height       int
	xPos         int
	yPos         int
	scrollOffset int
}

// New creates a new viewport component
func New() Model {
	return &model{
		scrollbar: scrollbar.New(),
	}
}

// Init initializes the viewport
func (m *model) Init() tea.Cmd {
	return nil
}

// Update handles messages and updates the viewport state
func (m *model) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		const mouseScrollAmount = 3
		buttonStr := msg.Button.String()

		switch buttonStr {
		case "wheelup":
			for range mouseScrollAmount {
				m.ScrollUp()
			}
		case "wheeldown":
			for range mouseScrollAmount {
				m.ScrollDown()
			}
		default:
			if msg.Y < 0 {
				for range min(-msg.Y, mouseScrollAmount) {
					m.ScrollUp()
				}
			} else if msg.Y > 0 {
				for range min(msg.Y, mouseScrollAmount) {
					m.ScrollDown()
				}
			}
		}
		// Sync scrollbar with new scroll offset
		m.scrollbar.SetScrollOffset(m.scrollOffset)
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "up", "k":
			m.ScrollUp()
			return m, nil
		case "down", "j":
			m.ScrollDown()
			return m, nil
		case "pgup":
			m.ScrollPageUp()
			return m, nil
		case "pgdown":
			m.ScrollPageDown()
			return m, nil
		case "home":
			m.ScrollToTop()
			return m, nil
		case "end":
			m.ScrollToBottom()
			return m, nil
		}
	}

	return m, nil
}

// View renders the visible portion of the content
func (m *model) View() string {
	if m.contentProvider == nil {
		return ""
	}

	totalHeight := m.contentProvider.GetTotalHeight()
	if totalHeight == 0 {
		return ""
	}

	// Update scrollbar state
	m.scrollbar.SetDimensions(m.height, totalHeight)
	m.scrollbar.SetScrollOffset(m.scrollOffset)

	// Get visible lines
	visibleLines := m.GetVisibleContent()
	return strings.Join(visibleLines, "\n")
}

// SetSize sets the dimensions of the viewport
func (m *model) SetSize(width, height int) tea.Cmd {
	m.width = width
	m.height = height
	return nil
}

// SetPosition sets the position of the viewport
func (m *model) SetPosition(x, y int) tea.Cmd {
	m.xPos = x
	m.yPos = y
	return nil
}

// SetContentProvider sets the content provider
func (m *model) SetContentProvider(provider ContentProvider) {
	m.contentProvider = provider
}

// ScrollUp scrolls up by one line
func (m *model) ScrollUp() {
	if m.scrollOffset > 0 {
		m.scrollOffset = max(0, m.scrollOffset-1)
		m.scrollbar.SetScrollOffset(m.scrollOffset)
	}
}

// ScrollDown scrolls down by one line
func (m *model) ScrollDown() {
	if m.contentProvider == nil {
		return
	}
	maxOffset := m.getMaxScrollOffset()
	m.scrollOffset = min(m.scrollOffset+1, maxOffset)
	m.scrollbar.SetScrollOffset(m.scrollOffset)
}

// ScrollPageUp scrolls up by one page
func (m *model) ScrollPageUp() {
	m.scrollOffset = max(0, m.scrollOffset-m.height)
	m.scrollbar.SetScrollOffset(m.scrollOffset)
}

// ScrollPageDown scrolls down by one page
func (m *model) ScrollPageDown() {
	if m.contentProvider == nil {
		return
	}
	maxOffset := m.getMaxScrollOffset()
	m.scrollOffset = min(m.scrollOffset+m.height, maxOffset)
	m.scrollbar.SetScrollOffset(m.scrollOffset)
}

// ScrollToTop scrolls to the top
func (m *model) ScrollToTop() {
	m.scrollOffset = 0
	m.scrollbar.SetScrollOffset(m.scrollOffset)
}

// ScrollToBottom scrolls to the bottom
func (m *model) ScrollToBottom() {
	if m.contentProvider == nil {
		return
	}
	m.scrollOffset = m.getMaxScrollOffset()
	m.scrollbar.SetScrollOffset(m.scrollOffset)
}

// ScrollToOffset sets the scroll offset to a specific value
func (m *model) ScrollToOffset(offset int) {
	if m.contentProvider == nil {
		return
	}
	maxOffset := m.getMaxScrollOffset()
	m.scrollOffset = max(0, min(offset, maxOffset))
	m.scrollbar.SetScrollOffset(m.scrollOffset)
}

// GetScrollOffset returns the current scroll offset
func (m *model) GetScrollOffset() int {
	return m.scrollOffset
}

// GetViewportBounds returns the start and end line indices visible in the viewport
func (m *model) GetViewportBounds() (start, end int) {
	if m.contentProvider == nil {
		return 0, 0
	}

	totalHeight := m.contentProvider.GetTotalHeight()
	if totalHeight == 0 {
		return 0, 0
	}

	// Clamp scroll offset
	maxOffset := m.getMaxScrollOffset()
	m.scrollOffset = max(0, min(m.scrollOffset, maxOffset))

	start = m.scrollOffset
	end = min(start+m.height, totalHeight)

	return start, end
}

// IsAtBottom returns true if the viewport is scrolled to the bottom
func (m *model) IsAtBottom() bool {
	if m.contentProvider == nil {
		return true
	}

	totalHeight := m.contentProvider.GetTotalHeight()
	if totalHeight == 0 {
		return true
	}

	maxOffset := m.getMaxScrollOffset()
	return m.scrollOffset >= maxOffset
}

// GetVisibleContent returns the visible lines from the content provider
func (m *model) GetVisibleContent() []string {
	if m.contentProvider == nil {
		return nil
	}

	content := m.contentProvider.GetContent()
	if content == "" {
		return nil
	}

	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return nil
	}

	start, end := m.GetViewportBounds()
	if start >= end || start >= len(lines) {
		return nil
	}

	end = min(end, len(lines))
	return lines[start:end]
}

// GetScrollbar returns the scrollbar model for external rendering/interaction
func (m *model) GetScrollbar() *scrollbar.Model {
	return m.scrollbar
}

// getMaxScrollOffset calculates the maximum scroll offset
func (m *model) getMaxScrollOffset() int {
	if m.contentProvider == nil {
		return 0
	}
	totalHeight := m.contentProvider.GetTotalHeight()
	return max(0, totalHeight-m.height)
}
