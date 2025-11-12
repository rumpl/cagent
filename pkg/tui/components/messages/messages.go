package messages

import (
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tui/components/notification"
	"github.com/docker/cagent/pkg/tui/components/scrollbar"
	"github.com/docker/cagent/pkg/tui/components/textselector"
	"github.com/docker/cagent/pkg/tui/components/tool/editfile"
	"github.com/docker/cagent/pkg/tui/components/viewport"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/types"
)

const (
	// Layout constants for proper spacing and positioning
	appStylePadding = 1 // Left padding from AppStyle
	totalRightSpace = 2 // scrollbarWidth + extra space
)

// StreamCancelledMsg notifies components that the stream has been cancelled
type StreamCancelledMsg struct {
	ShowMessage bool // Whether to show a cancellation message after cleanup
}

// Model represents a chat message list component
type Model interface {
	layout.Model
	layout.Sizeable
	layout.Focusable
	layout.Help
	layout.Positionable

	AddUserMessage(content string) tea.Cmd
	AddErrorMessage(content string) tea.Cmd
	AddAssistantMessage() tea.Cmd
	AddCancelledMessage() tea.Cmd
	AddWelcomeMessage(content string) tea.Cmd
	AddOrUpdateToolCall(agentName string, toolCall tools.ToolCall, toolDef tools.Tool, status types.ToolStatus) tea.Cmd
	AddToolResult(msg *runtime.ToolCallResponseEvent, status types.ToolStatus) tea.Cmd
	AppendToLastMessage(agentName string, messageType types.MessageType, content string) tea.Cmd
	AddShellOutputMessage(content string) tea.Cmd

	ScrollToBottom() tea.Cmd
}

// model implements Model using sub-components
type model struct {
	// Sub-components
	viewport    viewport.Model
	selector    textselector.Model
	messageList MessageList
	renderCache RenderCache
	scrollbar   *scrollbar.Model

	// Configuration
	app          *app.App
	sessionState *service.SessionState
	width        int
	height       int
	xPos, yPos   int
}

// New creates a new message list component
func New(a *app.App, sessionState *service.SessionState) Model {
	vp := viewport.New()
	ml := NewMessageList(sessionState)
	rc := NewRenderCache()

	// Connect render cache as content provider to viewport
	vp.SetContentProvider(rc)

	return &model{
		viewport:     vp,
		selector:     textselector.New(),
		messageList:  ml,
		renderCache:  rc,
		scrollbar:    scrollbar.New(),
		app:          a,
		sessionState: sessionState,
		width:        120,
		height:       24,
	}
}

// NewScrollableView creates a simple scrollable view for displaying messages in dialogs
func NewScrollableView(width, height int, sessionState *service.SessionState) Model {
	vp := viewport.New()
	ml := NewMessageList(sessionState)
	rc := NewRenderCache()

	vp.SetContentProvider(rc)

	return &model{
		viewport:     vp,
		selector:     textselector.New(),
		messageList:  ml,
		renderCache:  rc,
		scrollbar:    scrollbar.New(),
		sessionState: sessionState,
		width:        width,
		height:       height,
	}
}

// Init initializes the component
func (m *model) Init() tea.Cmd {
	return m.messageList.InitAllViews()
}

// Update handles messages and updates the component state
func (m *model) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case StreamCancelledMsg:
		m.messageList.RemoveSpinner()
		m.messageList.RemovePendingToolCalls()
		m.renderCache.InvalidateAll()
		m.ensureAllItemsRendered()
		return m, nil

	case tea.WindowSizeMsg:
		cmds = append(cmds, m.SetSize(msg.Width, msg.Height))

	case tea.MouseClickMsg:
		if m.isMouseOnScrollbar(msg.X, msg.Y) {
			return m.handleScrollbarUpdate(msg)
		}

		if msg.Button == tea.MouseLeft {
			line, col := m.mouseToLineCol(msg.X, msg.Y)
			m.selector.HandleMouseDown(col, line)
		}
		return m, nil

	case tea.MouseMotionMsg:
		if m.scrollbar.IsDragging() {
			return m.handleScrollbarUpdate(msg)
		}

		if m.selector.IsMouseButtonDown() && m.selector.IsActive() {
			line, col := m.mouseToLineCol(msg.X, msg.Y)
			m.selector.HandleMouseMove(col, line)
			cmd := m.autoScroll()
			return m, cmd
		}
		return m, nil

	case tea.MouseReleaseMsg:
		if updated, cmd := m.handleScrollbarUpdate(msg); cmd != nil {
			return updated, cmd
		}

		if msg.Button == tea.MouseLeft && m.selector.IsMouseButtonDown() {
			if m.selector.IsActive() {
				line, col := m.mouseToLineCol(msg.X, msg.Y)
				m.selector.HandleMouseUp(col, line)
				cmd := m.copySelectionToClipboard()
				return m, cmd
			}
			line, col := m.mouseToLineCol(msg.X, msg.Y)
			m.selector.HandleMouseUp(col, line)
		}
		return m, nil

	case tea.MouseWheelMsg:
		// Forward to viewport
		updated, cmd := m.viewport.Update(msg)
		m.viewport = updated.(viewport.Model)
		return m, cmd

	case textselector.AutoScrollTickMsg:
		if m.selector.IsMouseButtonDown() && m.selector.IsActive() {
			cmd := m.autoScroll()
			return m, cmd
		}
		return m, nil

	case editfile.ToggleDiffViewMsg:
		m.sessionState.ToggleSplitDiffView()
		m.renderCache.InvalidateAll()
		m.ensureAllItemsRendered()
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.selector.Clear()
			return m, nil
		case "up", "k", "down", "j", "pgup", "pgdown", "home", "end":
			// Forward to viewport
			updated, cmd := m.viewport.Update(msg)
			m.viewport = updated.(viewport.Model)
			return m, cmd
		}
	}

	// Forward updates to all message views
	cmd := m.messageList.UpdateAllViews(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// View renders the component
func (m *model) View() string {
	if m.messageList.GetMessageCount() == 0 {
		return ""
	}

	m.ensureAllItemsRendered()

	if m.renderCache.GetTotalHeight() == 0 {
		return ""
	}

	visibleLines := m.viewport.GetVisibleContent()
	if len(visibleLines) == 0 {
		return ""
	}

	if m.selector.IsActive() {
		startLine := m.viewport.GetScrollOffset()
		visibleLines = m.selector.ApplyHighlight(visibleLines, startLine)
	}

	contentView := strings.Join(visibleLines, "\n")
	scrollbarView := m.scrollbar.View()

	if scrollbarView != "" {
		return lipgloss.JoinHorizontal(lipgloss.Top, contentView, scrollbarView)
	}

	return contentView
}

// SetSize sets the dimensions of the component
func (m *model) SetSize(width, height int) tea.Cmd {
	// Reserve space for scrollbar
	m.width = width - totalRightSpace
	m.height = height

	// Update viewport size
	m.viewport.SetSize(m.width, m.height)

	// Update all message view sizes
	m.messageList.SetAllViewSizes(m.width)

	// Position scrollbar accounting for AppStyle padding
	scrollbarX := appStylePadding + m.xPos + m.width
	m.scrollbar.SetPosition(scrollbarX, m.yPos)
	m.scrollbar.SetDimensions(m.height, m.renderCache.GetTotalHeight())

	// Size changes may affect item rendering, invalidate all items
	m.renderCache.InvalidateAll()
	return nil
}

// SetPosition sets the position of the component
func (m *model) SetPosition(x, y int) tea.Cmd {
	m.xPos = x
	m.yPos = y
	m.viewport.SetPosition(x, y)
	return nil
}

// GetSize returns the current dimensions
func (m *model) GetSize() (width, height int) {
	return m.width, m.height
}

// Focus gives focus to the component
func (m *model) Focus() tea.Cmd {
	return nil
}

// Blur removes focus from the component
func (m *model) Blur() tea.Cmd {
	return nil
}

// Bindings returns key bindings for the component
func (m *model) Bindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("↑", "up"),
		),
		key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("↓", "down"),
		),
	}
}

// Help returns the help information
func (m *model) Help() help.KeyMap {
	return core.NewSimpleHelp(m.Bindings())
}

// AddUserMessage adds a user message to the chat
func (m *model) AddUserMessage(content string) tea.Cmd {
	return m.addMessage(types.User(content))
}

// AddErrorMessage adds an error message to the chat
func (m *model) AddErrorMessage(content string) tea.Cmd {
	return m.addMessage(types.Error(content))
}

// AddShellOutputMessage adds a shell output message to the chat
func (m *model) AddShellOutputMessage(content string) tea.Cmd {
	return m.addMessage(types.ShellOutput(content))
}

// AddAssistantMessage adds an assistant message to the chat
func (m *model) AddAssistantMessage() tea.Cmd {
	return m.addMessage(types.Spinner())
}

// addMessage is the internal method to add a message
func (m *model) addMessage(msg *types.Message) tea.Cmd {
	m.selector.Clear()

	wasAtBottom := m.viewport.IsAtBottom()

	m.messageList.AddMessage(msg)
	view := m.messageList.CreateMessageView(msg, m.width)
	m.messageList.AddView(view)

	var cmds []tea.Cmd
	if initCmd := view.Init(); initCmd != nil {
		cmds = append(cmds, initCmd)
	}

	// Invalidate cache since we added a message
	m.renderCache.InvalidateAll()

	if wasAtBottom {
		// Ensure items are rendered before scrolling
		m.ensureAllItemsRendered()
		cmds = append(cmds, func() tea.Msg {
			m.viewport.ScrollToBottom()
			return nil
		})
	}

	return tea.Batch(cmds...)
}

// AddCancelledMessage adds a cancellation indicator to the chat
func (m *model) AddCancelledMessage() tea.Cmd {
	msg := types.Cancelled()
	m.messageList.AddMessage(msg)

	view := m.messageList.CreateMessageView(msg, m.width)
	m.messageList.AddView(view)

	m.renderCache.InvalidateAll()
	return view.Init()
}

// AddWelcomeMessage adds a welcome message to the chat
func (m *model) AddWelcomeMessage(content string) tea.Cmd {
	if content == "" {
		return nil
	}
	msg := types.Welcome(content)
	m.messageList.AddMessage(msg)

	view := m.messageList.CreateMessageView(msg, m.width)
	m.messageList.AddView(view)

	m.renderCache.InvalidateAll()
	return view.Init()
}

// AddOrUpdateToolCall adds a tool call or updates existing one with the given status
func (m *model) AddOrUpdateToolCall(agentName string, toolCall tools.ToolCall, toolDef tools.Tool, status types.ToolStatus) tea.Cmd {
	messages := m.messageList.GetMessages()

	// First try to update existing tool by ID
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.ToolCall.ID == toolCall.ID {
			msg.ToolStatus = status
			if toolCall.Function.Arguments != "" {
				msg.ToolCall.Function.Arguments = toolCall.Function.Arguments
			}
			m.renderCache.Invalidate(i)

			view := m.messageList.CreateToolCallView(msg, m.width)
			m.messageList.(*messageList).views[i] = view
			return view.Init()
		}
	}

	// If not found by ID, remove last empty assistant message
	m.messageList.RemoveSpinner()

	// Create new tool call
	msg := types.ToolCallMessage(agentName, toolCall, toolDef, status)
	m.messageList.AddMessage(msg)

	view := m.messageList.CreateToolCallView(msg, m.width)
	m.messageList.AddView(view)

	m.renderCache.InvalidateAll()
	return view.Init()
}

// AddToolResult adds tool result to the most recent matching tool call
func (m *model) AddToolResult(msg *runtime.ToolCallResponseEvent, status types.ToolStatus) tea.Cmd {
	messages := m.messageList.GetMessages()

	for i := len(messages) - 1; i >= 0; i-- {
		toolMessage := messages[i]
		if toolMessage.ToolCall.ID == msg.ToolCall.ID {
			toolMessage.Content = strings.ReplaceAll(msg.Response, "\t", "    ")
			toolMessage.ToolStatus = status
			m.renderCache.Invalidate(i)

			view := m.messageList.CreateToolCallView(toolMessage, m.width)
			m.messageList.(*messageList).views[i] = view
			return view.Init()
		}
	}
	return nil
}

// AppendToLastMessage appends content to the last message (for streaming)
func (m *model) AppendToLastMessage(agentName string, messageType types.MessageType, content string) tea.Cmd {
	m.messageList.RemoveSpinner()

	if m.messageList.GetMessageCount() == 0 {
		return nil
	}

	messages := m.messageList.GetMessages()
	lastIdx := len(messages) - 1
	lastMsg := messages[lastIdx]

	if lastMsg.Type == messageType {
		// Check if we were at the bottom before updating
		wasAtBottom := m.viewport.IsAtBottom()

		lastMsg.Content += content
		views := m.messageList.GetViews()
		if lastIdx < len(views) {
			if msgView, ok := views[lastIdx].(interface{ SetMessage(*types.Message) }); ok {
				msgView.SetMessage(lastMsg)
			}
		}
		m.renderCache.Invalidate(lastIdx)

		// If we were at the bottom, scroll to the new bottom to show incoming tokens
		if wasAtBottom {
			// Ensure items are rendered before scrolling
			m.ensureAllItemsRendered()
			return func() tea.Msg {
				m.viewport.ScrollToBottom()
				return nil
			}
		}

		return nil
	} else {
		wasAtBottom := m.viewport.IsAtBottom()

		msg := types.Agent(messageType, agentName, content)
		m.messageList.AddMessage(msg)

		view := m.messageList.CreateMessageView(msg, m.width)
		m.messageList.AddView(view)

		m.renderCache.InvalidateAll()

		var cmds []tea.Cmd
		if initCmd := view.Init(); initCmd != nil {
			cmds = append(cmds, initCmd)
		}

		if wasAtBottom {
			// Ensure items are rendered before scrolling
			m.ensureAllItemsRendered()
			cmds = append(cmds, func() tea.Msg {
				m.viewport.ScrollToBottom()
				return nil
			})
		}

		return tea.Batch(cmds...)
	}
}

// ScrollToBottom scrolls to the bottom of the chat
func (m *model) ScrollToBottom() tea.Cmd {
	return func() tea.Msg {
		m.viewport.ScrollToBottom()
		return nil
	}
}

// ensureAllItemsRendered ensures all message items are rendered
func (m *model) ensureAllItemsRendered() {
	messages := m.messageList.GetMessages()
	views := m.messageList.GetViews()
	m.renderCache.RenderAll(messages, views)

	// Update scrollbar dimensions
	m.scrollbar.SetDimensions(m.height, m.renderCache.GetTotalHeight())
	m.scrollbar.SetScrollOffset(m.viewport.GetScrollOffset())
}

// mouseToLineCol converts mouse position to line/column in rendered content
func (m *model) mouseToLineCol(x, y int) (line, col int) {
	// Adjust for AppStyle left padding
	adjustedX := max(0, x-appStylePadding-m.xPos)
	col = adjustedX

	adjustedY := max(0, y-m.yPos)
	line = m.viewport.GetScrollOffset() + adjustedY

	return line, col
}

// copySelectionToClipboard copies the selected text to the clipboard
func (m *model) copySelectionToClipboard() tea.Cmd {
	if !m.selector.IsActive() {
		return nil
	}

	selectedText := strings.TrimSpace(m.selector.GetSelectedText(m.renderCache.GetContent()))
	if selectedText == "" {
		return nil
	}

	if err := clipboard.WriteAll(selectedText); err != nil {
		return core.CmdHandler(notification.ShowMsg{Text: "Failed to copy: " + err.Error(), Type: notification.TypeError})
	}

	return core.CmdHandler(notification.ShowMsg{Text: "Text copied to clipboard"})
}

// autoScroll handles auto-scrolling during text selection
func (m *model) autoScroll() tea.Cmd {
	// Use stored screen Y coordinate
	viewportY := max(m.selector.GetMouseY()-m.yPos, 0)

	direction := m.selector.GetAutoScrollDirection(viewportY, m.height)
	if direction == 0 {
		return nil
	}

	if direction == -1 && m.viewport.GetScrollOffset() > 0 {
		m.viewport.ScrollUp()
		m.selector.UpdateSelectionForScroll(-1)
	} else if direction == 1 {
		maxScrollOffset := max(0, m.renderCache.GetTotalHeight()-m.height)
		if m.viewport.GetScrollOffset() < maxScrollOffset {
			m.viewport.ScrollDown()
			m.selector.UpdateSelectionForScroll(1)
		}
	}

	return textselector.AutoScrollTick(direction)
}

// isMouseOnScrollbar checks if the mouse is on the scrollbar
func (m *model) isMouseOnScrollbar(x, y int) bool {
	if m.renderCache.GetTotalHeight() <= m.height {
		return false
	}

	// Calculate scrollbar X position accounting for AppStyle padding
	scrollbarX := appStylePadding + m.xPos + m.width
	return x == scrollbarX && y >= m.yPos && y < m.yPos+m.height
}

// handleScrollbarUpdate handles scrollbar interactions
func (m *model) handleScrollbarUpdate(msg tea.Msg) (layout.Model, tea.Cmd) {
	sb, cmd := m.scrollbar.Update(msg)
	m.scrollbar = sb
	// Sync viewport with scrollbar offset
	m.viewport.ScrollToOffset(m.scrollbar.GetScrollOffset())
	return m, cmd
}
