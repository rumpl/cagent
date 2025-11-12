package textselector

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/docker/cagent/pkg/tui/styles"
)

// AutoScrollTickMsg triggers auto-scroll during selection
type AutoScrollTickMsg struct {
	Direction int // -1 for up, 1 for down
}

// Model represents a text selection component
type Model interface {
	// Mouse event handling
	HandleMouseDown(x, y int)
	HandleMouseMove(x, y int)
	HandleMouseUp(x, y int)

	// Selection state
	IsActive() bool
	IsMouseButtonDown() bool
	Clear()

	// Extract selected text
	GetSelectedText(content string) string

	// Apply highlighting to visible lines
	ApplyHighlight(lines []string, viewportStartLine int) []string

	// Auto-scroll support
	GetAutoScrollDirection(mouseY, viewportHeight int) int
	UpdateSelectionForScroll(direction int)

	// Get mouse Y coordinate for auto-scroll
	GetMouseY() int
}

// selectionState encapsulates all state related to text selection
type selectionState struct {
	active          bool
	startLine       int
	startCol        int
	endLine         int
	endCol          int
	mouseButtonDown bool
	mouseY          int // Screen Y coordinate for autoscroll
}

// model implements Model
type model struct {
	selection selectionState
}

// New creates a new text selector component
func New() Model {
	return &model{}
}

// HandleMouseDown starts a new selection
func (m *model) HandleMouseDown(x, y int) {
	m.selection.active = true
	m.selection.mouseButtonDown = true
	m.selection.startLine = y
	m.selection.startCol = x
	m.selection.endLine = y
	m.selection.endCol = x
	m.selection.mouseY = y
}

// HandleMouseMove updates the selection end position
func (m *model) HandleMouseMove(x, y int) {
	if m.selection.mouseButtonDown {
		m.selection.endLine = y
		m.selection.endCol = x
		m.selection.mouseY = y
	}
}

// HandleMouseUp finalizes the selection
func (m *model) HandleMouseUp(x, y int) {
	if m.selection.mouseButtonDown && m.selection.active {
		m.selection.endLine = y
		m.selection.endCol = x
		m.selection.mouseButtonDown = false
	}
}

// IsActive returns true if there is an active selection
func (m *model) IsActive() bool {
	return m.selection.active
}

// IsMouseButtonDown returns true if the mouse button is currently down
func (m *model) IsMouseButtonDown() bool {
	return m.selection.mouseButtonDown
}

// Clear clears the selection
func (m *model) Clear() {
	m.selection = selectionState{}
}

// GetMouseY returns the current mouse Y coordinate
func (m *model) GetMouseY() int {
	return m.selection.mouseY
}

// GetSelectedText extracts the selected text from the content
func (m *model) GetSelectedText(content string) string {
	if !m.selection.active {
		return ""
	}

	lines := strings.Split(content, "\n")

	// Normalize selection direction
	startLine, startCol := m.selection.startLine, m.selection.startCol
	endLine, endCol := m.selection.endLine, m.selection.endCol

	if startLine > endLine || (startLine == endLine && startCol > endCol) {
		startLine, endLine = endLine, startLine
		startCol, endCol = endCol, startCol
	}

	if startLine < 0 || startLine >= len(lines) {
		return ""
	}
	if endLine >= len(lines) {
		endLine = len(lines) - 1
	}

	var result strings.Builder
	for i := startLine; i <= endLine && i < len(lines); i++ {
		line := ansi.Strip(lines[i])
		runes := []rune(line)

		var lineText string
		switch i {
		case startLine:
			if startLine == endLine {
				startIdx := displayWidthToRuneIndex(line, startCol)
				endIdx := min(displayWidthToRuneIndex(line, endCol), len(runes))
				if startIdx < len(runes) && startIdx < endIdx {
					lineText = strings.TrimSpace(string(runes[startIdx:endIdx]))
				}
				break
			}
			// First line: from startCol to end
			startIdx := displayWidthToRuneIndex(line, startCol)
			if startIdx < len(runes) {
				lineText = strings.TrimSpace(string(runes[startIdx:]))
			}
		case endLine:
			// Last line: from start to endCol
			endIdx := min(displayWidthToRuneIndex(line, endCol), len(runes))
			lineText = strings.TrimSpace(string(runes[:endIdx]))
		default:
			// Middle lines: entire line
			lineText = strings.TrimSpace(line)
		}

		if lineText != "" {
			result.WriteString(lineText)
		}

		result.WriteString("\n")
	}

	return result.String()
}

// ApplyHighlight applies selection highlighting to visible lines
func (m *model) ApplyHighlight(lines []string, viewportStartLine int) []string {
	if !m.selection.active {
		return lines
	}

	// Normalize selection bounds
	startLine, startCol := m.selection.startLine, m.selection.startCol
	endLine, endCol := m.selection.endLine, m.selection.endCol

	if startLine > endLine || (startLine == endLine && startCol > endCol) {
		startLine, endLine = endLine, startLine
		startCol, endCol = endCol, startCol
	}

	highlighted := make([]string, len(lines))

	for i, line := range lines {
		absoluteLine := viewportStartLine + i

		if absoluteLine < startLine || absoluteLine > endLine {
			highlighted[i] = line
			continue
		}

		switch {
		case startLine == endLine && absoluteLine == startLine:
			// Single line selection
			highlighted[i] = highlightLine(line, startCol, endCol)
		case absoluteLine == startLine:
			// Start of multi-line selection
			plainLine := ansi.Strip(line)
			trimmedLine := strings.TrimRight(plainLine, " \t")
			lineWidth := runewidth.StringWidth(trimmedLine)
			highlighted[i] = highlightLine(line, startCol, lineWidth)
		case absoluteLine == endLine:
			// End of multi-line selection
			highlighted[i] = highlightLine(line, 0, endCol)
		default:
			// Middle of multi-line selection
			plainLine := ansi.Strip(line)
			trimmedLine := strings.TrimRight(plainLine, " \t")
			lineWidth := runewidth.StringWidth(trimmedLine)
			highlighted[i] = highlightLine(line, 0, lineWidth)
		}
	}

	return highlighted
}

// GetAutoScrollDirection returns the auto-scroll direction based on mouse position
// Returns -1 for up, 1 for down, 0 for no scroll
func (m *model) GetAutoScrollDirection(mouseY, viewportHeight int) int {
	const scrollThreshold = 2

	if mouseY < scrollThreshold {
		return -1
	} else if mouseY >= viewportHeight-scrollThreshold && mouseY < viewportHeight {
		return 1
	}

	return 0
}

// UpdateSelectionForScroll updates the selection end line when scrolling
func (m *model) UpdateSelectionForScroll(direction int) {
	if direction == -1 {
		// Scrolling up
		m.selection.endLine = max(0, m.selection.endLine-1)
	} else if direction == 1 {
		// Scrolling down
		m.selection.endLine++
	}
}

// AutoScrollTick creates a tick message for auto-scrolling
func AutoScrollTick(direction int) tea.Cmd {
	if direction == 0 {
		return nil
	}

	return tea.Tick(20*time.Millisecond, func(time.Time) tea.Msg {
		return AutoScrollTickMsg{Direction: direction}
	})
}

// highlightLine highlights a portion of a line from startCol to endCol
func highlightLine(line string, startCol, endCol int) string {
	// Get plain text for boundary checks
	plainLine := ansi.Strip(line)
	plainWidth := runewidth.StringWidth(plainLine)

	// Validate and normalize boundaries
	if startCol >= plainWidth {
		return line
	}
	if startCol >= endCol {
		return line
	}
	if endCol > plainWidth {
		endCol = plainWidth
	}

	// Extract the three parts while preserving ANSI codes
	// before: from start to startCol (preserves original styling)
	before := ansi.Cut(line, 0, startCol)

	// selected: from startCol to endCol (strip styling, apply selection style)
	selectedText := ansi.Cut(line, startCol, endCol)
	selectedPlain := ansi.Strip(selectedText)
	selected := styles.SelectionStyle.Render(selectedPlain)

	// after: from endCol to end (preserves original styling)
	after := ansi.Cut(line, endCol, plainWidth)

	return before + selected + after
}

// displayWidthToRuneIndex converts a display width to a rune index
func displayWidthToRuneIndex(s string, targetWidth int) int {
	if targetWidth <= 0 {
		return 0
	}

	runes := []rune(s)
	currentWidth := 0

	for i, r := range runes {
		if currentWidth >= targetWidth {
			return i
		}
		currentWidth += runewidth.RuneWidth(r)
	}

	return len(runes)
}
