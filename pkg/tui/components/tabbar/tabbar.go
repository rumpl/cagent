package tabbar

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/styles"
)

const (
	maxTabTitleLength = 20
	tabBarHeight      = 3
	closeButtonWidth  = 3
)

// Messages
type (
	TabClickMsg struct {
		TabIndex int
	}

	TabCloseMsg struct {
		TabIndex int
	}

	NewTabClickMsg struct{}
)

// TabInfo represents information about a single tab
type TabInfo struct {
	ID    string
	Title string
}

// TabBar is a component that displays tabs at the top of the screen
type TabBar struct {
	tabs        []TabInfo
	activeIndex int
	width       int
	height      int
}

// New creates a new TabBar
func New() TabBar {
	return TabBar{
		tabs:        []TabInfo{},
		activeIndex: 0,
		height:      tabBarHeight,
	}
}

// Init initializes the TabBar
func (t *TabBar) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (t *TabBar) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		t.width = msg.Width
		return t, nil

	case tea.MouseClickMsg:
		return t.handleMouseClickMsg(msg)
	}

	return t, nil
}

// handleMouseClickMsg processes mouse click events for tab interaction
func (t *TabBar) handleMouseClickMsg(msg tea.MouseClickMsg) (layout.Model, tea.Cmd) {
	// Check if click is within tab bar area
	if msg.Y >= tabBarHeight {
		return t, nil
	}

	// Calculate tab positions and check for clicks
	x := 0
	for i, tab := range t.tabs {
		tabWidth := t.calculateTabWidth(tab.Title)

		// Check if click is within this tab's bounds
		if msg.X >= x && msg.X < x+tabWidth {
			// Check if click is on close button
			closeX := x + tabWidth - closeButtonWidth - 1
			if msg.X >= closeX && msg.X < closeX+closeButtonWidth {
				// Click on close button
				return t, func() tea.Msg {
					return TabCloseMsg{TabIndex: i}
				}
			}
			// Click on tab itself
			return t, func() tea.Msg {
				return TabClickMsg{TabIndex: i}
			}
		}

		x += tabWidth + 1 // Add spacing
	}

	// Check for click on "+ New" button
	newBtnX := x
	newBtnWidth := 5
	if msg.X >= newBtnX && msg.X < newBtnX+newBtnWidth {
		return t, func() tea.Msg {
			return NewTabClickMsg{}
		}
	}

	return t, nil
}

// View renders the TabBar
func (t *TabBar) View() string {
	if len(t.tabs) == 0 {
		return ""
	}

	var tabs []string
	for i, tab := range t.tabs {
		tabs = append(tabs, t.renderTab(tab, i))
	}

	// Add "+ New" button
	newBtn := t.renderNewButton()
	tabs = append(tabs, newBtn)

	tabsLine := lipgloss.JoinHorizontal(lipgloss.Top, tabs...)

	// Add bottom border line
	borderLine := strings.Repeat("─", t.width)
	borderStyle := lipgloss.NewStyle().Foreground(styles.BorderSecondary)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		tabsLine,
		borderStyle.Render(borderLine),
	)
}

// renderTab renders a single tab
func (t *TabBar) renderTab(tab TabInfo, index int) string {
	title := t.truncateTitle(tab.Title)
	isActive := index == t.activeIndex

	// Build tab content with close button
	closeBtn := " ×"
	content := title + closeBtn

	var tabStyle lipgloss.Style
	if isActive {
		// Active tab style
		tabStyle = lipgloss.NewStyle().
			Foreground(styles.TextPrimary).
			Background(styles.Background).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderTop(true).
			BorderLeft(true).
			BorderRight(true).
			BorderForeground(styles.BorderPrimary).
			Padding(0, 1).
			Bold(true)
	} else {
		// Inactive tab style
		tabStyle = lipgloss.NewStyle().
			Foreground(styles.TextSecondary).
			Background(styles.BackgroundAlt).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderTop(true).
			BorderLeft(true).
			BorderRight(true).
			BorderForeground(styles.BorderSecondary).
			Padding(0, 1)
	}

	return tabStyle.Render(content)
}

// renderNewButton renders the "+ New" button
func (t *TabBar) renderNewButton() string {
	btnStyle := lipgloss.NewStyle().
		Foreground(styles.Accent).
		Background(styles.BackgroundAlt).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderTop(true).
		BorderLeft(true).
		BorderRight(true).
		BorderForeground(styles.BorderSecondary).
		Padding(0, 1)

	return btnStyle.Render("+")
}

// truncateTitle truncates a title if it's too long
func (t *TabBar) truncateTitle(title string) string {
	if len(title) <= maxTabTitleLength {
		return title
	}
	return title[:maxTabTitleLength-3] + "..."
}

// calculateTabWidth calculates the width of a tab including borders and padding
func (t *TabBar) calculateTabWidth(title string) int {
	truncated := t.truncateTitle(title)
	// Width = padding(2) + title + close button (2) + border(2)
	return len(truncated) + closeButtonWidth + 4
}

// SetTabs sets the tabs to display
func (t *TabBar) SetTabs(tabs []TabInfo) {
	t.tabs = tabs
}

// SetActive sets the active tab index
func (t *TabBar) SetActive(index int) {
	if index >= 0 && index < len(t.tabs) {
		t.activeIndex = index
	}
}

// SetSize sets the size of the tab bar
func (t *TabBar) SetSize(width, height int) tea.Cmd {
	t.width = width
	t.height = height
	return nil
}

// GetSize returns the current size
func (t *TabBar) GetSize() (width, height int) {
	return t.width, t.height
}

// GetHeight returns the height of the tab bar
func (t *TabBar) GetHeight() int {
	return tabBarHeight
}
