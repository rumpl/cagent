package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"
	"github.com/google/uuid"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/browser"
	"github.com/docker/cagent/pkg/evaluation"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tui/commands"
	"github.com/docker/cagent/pkg/tui/components/completion"
	"github.com/docker/cagent/pkg/tui/components/editor"
	"github.com/docker/cagent/pkg/tui/components/notification"
	"github.com/docker/cagent/pkg/tui/components/statusbar"
	"github.com/docker/cagent/pkg/tui/components/tabbar"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/dialog"
	"github.com/docker/cagent/pkg/tui/page/chat"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/styles"
)

var lastMouseEvent time.Time

// MouseEventFilter filters mouse events to prevent spam
func MouseEventFilter(_ tea.Model, msg tea.Msg) tea.Msg {
	switch msg.(type) {
	case tea.MouseWheelMsg, tea.MouseMotionMsg, tea.MouseMsg:
		now := time.Now()
		if now.Sub(lastMouseEvent) < 20*time.Millisecond {
			return nil
		}
		lastMouseEvent = now
	}
	return msg
}

// Tab represents a single tab with its own chat session
type Tab struct {
	ID           string
	Title        string
	ChatPage     chat.Page
	SessionState *service.SessionState
	Session      *session.Session
}

// appModel represents the main application model
type appModel struct {
	application     *app.App
	wWidth, wHeight int // Window dimensions
	width, height   int
	keyMap          KeyMap

	// Tab management
	tabs           []Tab
	activeTabIndex int
	tabBar         *tabbar.TabBar

	statusBar statusbar.StatusBar

	notification notification.Manager
	dialog       dialog.Manager
	completions  completion.Manager

	// State
	ready           bool
	err             error
	processingTabID string // ID of tab currently running a query
}

// KeyMap defines global key bindings
type KeyMap struct {
	Quit           key.Binding
	CommandPalette key.Binding
	NewTab         key.Binding
	CloseTab       key.Binding
	NextTab        key.Binding
	PreviousTab    key.Binding
}

// DefaultKeyMap returns the default global key bindings
func DefaultKeyMap() KeyMap {
	return KeyMap{
		CommandPalette: key.NewBinding(
			key.WithKeys("ctrl+p"),
			key.WithHelp("ctrl+p", "commands"),
		),
		NewTab: key.NewBinding(
			key.WithKeys("ctrl+t"),
			key.WithHelp("ctrl+t", "new tab"),
		),
		CloseTab: key.NewBinding(
			key.WithKeys("ctrl+w"),
			key.WithHelp("ctrl+w", "close tab"),
		),
		NextTab: key.NewBinding(
			key.WithKeys("ctrl+tab"),
			key.WithHelp("ctrl+tab", "next tab"),
		),
		PreviousTab: key.NewBinding(
			key.WithKeys("ctrl+shift+tab"),
			key.WithHelp("ctrl+shift+tab", "previous tab"),
		),
	}
}

// New creates and initializes a new TUI application model
func New(a *app.App) tea.Model {
	t := &appModel{
		keyMap:         DefaultKeyMap(),
		dialog:         dialog.New(),
		notification:   notification.New(),
		completions:    completion.New(),
		application:    a,
		tabBar:         &tabbar.TabBar{},
		tabs:           []Tab{},
		activeTabIndex: 0,
	}

	// Initialize tab bar
	*t.tabBar = tabbar.New()

	// Create initial tab
	initialTab := t.createNewTab()
	t.tabs = append(t.tabs, initialTab)
	t.updateTabBar()

	t.statusBar = statusbar.New(t.activeTab().ChatPage)

	return t
}

// Init initializes the application
func (a *appModel) Init() tea.Cmd {
	cmds := []tea.Cmd{
		a.dialog.Init(),
		a.tabBar.Init(),
		a.activeTab().ChatPage.Init(),
		a.emitStartupInfo(),
	}

	if firstMessage := a.application.FirstMessage(); firstMessage != nil {
		cmds = append(cmds, func() tea.Msg {
			return editor.SendMsg{
				Content: a.application.ResolveCommand(context.Background(), *firstMessage),
			}
		})
	}

	return tea.Batch(cmds...)
}

// emitStartupInfo creates a command that emits startup events for immediate sidebar display
func (a *appModel) emitStartupInfo() tea.Cmd {
	return func() tea.Msg {
		// a buffered channel to collect startup events
		events := make(chan runtime.Event, 10)

		go func() {
			defer close(events)
			a.application.EmitStartupInfo(context.Background(), events)
		}()

		var collectedEvents []runtime.Event
		for event := range events {
			collectedEvents = append(collectedEvents, event)
		}

		return StartupEventsMsg{Events: collectedEvents}
	}
}

// StartupEventsMsg carries startup events to be processed by the UI
type StartupEventsMsg struct {
	Events []runtime.Event
}

// Help returns help information
func (a *appModel) Help() help.KeyMap {
	return core.NewSimpleHelp(a.Bindings())
}

func (a *appModel) Bindings() []key.Binding {
	return append([]key.Binding{
		a.keyMap.Quit,
		a.keyMap.CommandPalette,
		a.keyMap.NewTab,
		a.keyMap.CloseTab,
		a.keyMap.NextTab,
		a.keyMap.PreviousTab,
	}, a.activeTab().ChatPage.Bindings()...)
}

// Tab management helper methods

// activeTab returns the currently active tab
func (a *appModel) activeTab() *Tab {
	if len(a.tabs) == 0 || a.activeTabIndex < 0 || a.activeTabIndex >= len(a.tabs) {
		return nil
	}
	return &a.tabs[a.activeTabIndex]
}

// createNewTab creates a new tab with a fresh session
func (a *appModel) createNewTab() Tab {
	sessionState := service.NewSessionState()

	// Create new session in app
	a.application.NewSession()
	sess := a.application.Session()

	chatPage := chat.New(a.application, sessionState, sess)

	return Tab{
		ID:           uuid.New().String(),
		Title:        "New Session",
		ChatPage:     chatPage,
		SessionState: sessionState,
		Session:      sess,
	}
}

// createTabFromSession creates a tab from an existing session
func (a *appModel) createTabFromSession(sess *session.Session) Tab {
	sessionState := service.NewSessionState()
	chatPage := chat.New(a.application, sessionState, sess)

	// Generate title from session
	title := a.generateTabTitle(sess)

	return Tab{
		ID:           uuid.New().String(),
		Title:        title,
		ChatPage:     chatPage,
		SessionState: sessionState,
		Session:      sess,
	}
}

// generateTabTitle generates a title for a tab from session content
func (a *appModel) generateTabTitle(sess *session.Session) string {
	// Use session title if available
	if sess.Title != "" {
		return sess.Title
	}

	// Find first user message
	for _, item := range sess.Messages {
		if item.IsMessage() && item.Message.Message.Content != "" {
			content := strings.TrimSpace(item.Message.Message.Content)
			if idx := strings.Index(content, "\n"); idx > 0 {
				content = content[:idx]
			}
			if len(content) > 20 {
				content = content[:17] + "..."
			}
			if content != "" {
				return content
			}
		}
	}

	// Fallback to session ID
	if sess.ID != "" {
		return "Session " + sess.ID[:8]
	}

	return "New Session"
}

// updateTabBar updates the tab bar with current tab information
func (a *appModel) updateTabBar() {
	tabInfos := make([]tabbar.TabInfo, len(a.tabs))
	for i, tab := range a.tabs {
		tabInfos[i] = tabbar.TabInfo{
			ID:    tab.ID,
			Title: tab.Title,
		}
	}
	a.tabBar.SetTabs(tabInfos)
	a.tabBar.SetActive(a.activeTabIndex)
}

// switchToTab switches to the specified tab index
func (a *appModel) switchToTab(index int) tea.Cmd {
	if index < 0 || index >= len(a.tabs) {
		return nil
	}

	a.activeTabIndex = index
	a.updateTabBar()

	// Switch app session to the new tab's session - ignore error as tab might not be saved yet
	_ = a.application.LoadSession(context.Background(), a.tabs[index].Session.ID)

	// Update status bar
	a.statusBar = statusbar.New(a.activeTab().ChatPage)

	return tea.Batch(
		a.activeTab().ChatPage.SetSize(a.width, a.height-a.tabBar.GetHeight()),
		a.handleWindowResize(a.wWidth, a.wHeight),
	)
}

// closeTab closes the tab at the specified index
func (a *appModel) closeTab(index int) tea.Cmd {
	if index < 0 || index >= len(a.tabs) {
		return nil
	}

	// Cleanup the tab
	a.tabs[index].ChatPage.Cleanup()

	// Remove tab from slice
	a.tabs = append(a.tabs[:index], a.tabs[index+1:]...)

	// If we closed the last tab, create a new one
	if len(a.tabs) == 0 {
		newTab := a.createNewTab()
		a.tabs = append(a.tabs, newTab)
		a.activeTabIndex = 0
		a.updateTabBar()
		return tea.Batch(
			newTab.ChatPage.Init(),
			a.handleWindowResize(a.wWidth, a.wHeight),
		)
	}

	// Adjust active tab index if needed
	if a.activeTabIndex >= len(a.tabs) {
		a.activeTabIndex = len(a.tabs) - 1
	} else if index < a.activeTabIndex {
		a.activeTabIndex--
	}

	a.updateTabBar()

	// Switch to the new active tab
	return a.switchToTab(a.activeTabIndex)
}

// Update handles incoming messages and updates the application state
func (a *appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	// Handle dialog-specific messages first
	case dialog.OpenDialogMsg, dialog.CloseDialogMsg:
		u, dialogCmd := a.dialog.Update(msg)
		a.dialog = u.(dialog.Manager)
		return a, dialogCmd

	case StartupEventsMsg:
		var cmds []tea.Cmd
		for _, event := range msg.Events {
			updated, cmd := a.activeTab().ChatPage.Update(event)
			a.tabs[a.activeTabIndex].ChatPage = updated.(chat.Page)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return a, tea.Batch(cmds...)

	case tea.WindowSizeMsg:
		a.wWidth, a.wHeight = msg.Width, msg.Height
		cmd := a.handleWindowResize(msg.Width, msg.Height)
		a.completions.Update(msg)
		// Update tab bar size
		u, tabBarCmd := a.tabBar.Update(msg)
		a.tabBar = u.(*tabbar.TabBar)
		cmd = tea.Batch(cmd, tabBarCmd)
		return a, cmd

	case notification.ShowMsg, notification.HideMsg:
		updated, cmd := a.notification.Update(msg)
		a.notification = updated
		return a, cmd

	case tea.KeyPressMsg:
		cmd := a.handleKeyPressMsg(msg)
		return a, cmd

	case tea.MouseWheelMsg:
		// If dialogs are active, they get priority for mouse events
		if a.dialog.Open() {
			u, dialogCmd := a.dialog.Update(msg)
			a.dialog = u.(dialog.Manager)
			return a, dialogCmd
		}
		// Otherwise forward to chat page
		updated, cmd := a.activeTab().ChatPage.Update(msg)
		a.tabs[a.activeTabIndex].ChatPage = updated.(chat.Page)
		return a, cmd

	case tea.MouseMsg:
		// Forward to tab bar for potential tab clicks
		u, tabBarCmd := a.tabBar.Update(msg)
		a.tabBar = u.(*tabbar.TabBar)
		if tabBarCmd != nil {
			return a, tabBarCmd
		}
		// Otherwise forward to active chat page
		updated, cmd := a.activeTab().ChatPage.Update(msg)
		a.tabs[a.activeTabIndex].ChatPage = updated.(chat.Page)
		return a, cmd

	// Tab management messages
	case tabbar.TabClickMsg:
		cmd := a.switchToTab(msg.TabIndex)
		return a, cmd

	case tabbar.TabCloseMsg:
		cmd := a.closeTab(msg.TabIndex)
		return a, cmd

	case tabbar.NewTabClickMsg, commands.CreateNewTabMsg:
		newTab := a.createNewTab()
		a.tabs = append(a.tabs, newTab)
		a.activeTabIndex = len(a.tabs) - 1
		a.updateTabBar()
		return a, tea.Batch(
			newTab.ChatPage.Init(),
			a.handleWindowResize(a.wWidth, a.wHeight),
		)

	case commands.SwitchTabMsg:
		cmd := a.switchToTab(msg.TabIndex)
		return a, cmd

	case commands.CloseTabMsg:
		cmd := a.closeTab(msg.TabIndex)
		return a, cmd

	case commands.NextTabMsg:
		nextIndex := (a.activeTabIndex + 1) % len(a.tabs)
		cmd := a.switchToTab(nextIndex)
		return a, cmd

	case commands.PreviousTabMsg:
		prevIndex := a.activeTabIndex - 1
		if prevIndex < 0 {
			prevIndex = len(a.tabs) - 1
		}
		cmd := a.switchToTab(prevIndex)
		return a, cmd

	case commands.UpdateTabTitleMsg:
		if msg.TabIndex >= 0 && msg.TabIndex < len(a.tabs) {
			a.tabs[msg.TabIndex].Title = msg.Title
			a.updateTabBar()
		}
		return a, nil

	case commands.NewSessionMsg:
		// Create new tab with fresh session
		newTab := a.createNewTab()
		a.tabs = append(a.tabs, newTab)
		a.activeTabIndex = len(a.tabs) - 1
		a.updateTabBar()
		a.statusBar = statusbar.New(newTab.ChatPage)

		return a, tea.Batch(
			newTab.ChatPage.Init(),
			a.handleWindowResize(a.wWidth, a.wHeight),
		)

	case commands.EvalSessionMsg:
		evalFile, _ := evaluation.Save(a.activeTab().Session)
		return a, core.CmdHandler(notification.ShowMsg{Text: fmt.Sprintf("Eval saved to file %s", evalFile)})

	case commands.CompactSessionMsg:
		return a, a.activeTab().ChatPage.CompactSession()

	case commands.CopySessionToClipboardMsg:
		transcript := a.application.PlainTextTranscript()
		if transcript == "" {
			return a, core.CmdHandler(notification.ShowMsg{Text: "Conversation is empty; nothing copied."})
		}

		if err := clipboard.WriteAll(transcript); err != nil {
			return a, core.CmdHandler(notification.ShowMsg{Text: "Failed to copy conversation: " + err.Error(), Type: notification.TypeError})
		}

		return a, core.CmdHandler(notification.ShowMsg{Text: "Conversation copied to clipboard."})

	case commands.ToggleYoloMsg:
		sess := a.activeTab().Session
		sess.ToolsApproved = !sess.ToolsApproved
		var statusText string
		if sess.ToolsApproved {
			statusText = "Yolo mode enabled: tools will be auto-approved"
		} else {
			statusText = "Yolo mode disabled: tools will require confirmation"
		}
		return a, core.CmdHandler(notification.ShowMsg{Text: statusText})
	case commands.LoadSessionMsg:
		// Load session from store
		if err := a.application.LoadSession(context.Background(), msg.SessionID); err != nil {
			return a, core.CmdHandler(notification.ShowMsg{
				Text: fmt.Sprintf("Failed to load session: %v", err),
				Type: notification.TypeError,
			})
		}

		// Get the loaded session
		sess := a.application.Session()

		// Create new tab with loaded session
		newTab := a.createTabFromSession(sess)
		a.tabs = append(a.tabs, newTab)
		a.activeTabIndex = len(a.tabs) - 1
		a.updateTabBar()
		a.statusBar = statusbar.New(newTab.ChatPage)

		// Convert session messages to TUI messages for display
		tuiMessages := ConvertSessionToTUIMessages(sess)

		// Create token usage event from loaded session to update sidebar
		tokenUsageEvent := runtime.TokenUsage(
			sess.InputTokens,
			sess.OutputTokens,
			0, // ContextLength - not stored in session
			0, // ContextLimit - not stored in session
			sess.Cost,
		)

		return a, tea.Sequence(
			newTab.ChatPage.Init(),
			newTab.ChatPage.LoadMessages(tuiMessages),
			a.handleWindowResize(a.wWidth, a.wHeight),
			core.CmdHandler(tokenUsageEvent),
			core.CmdHandler(notification.ShowMsg{Text: "Session loaded successfully"}),
		)

	case commands.AgentCommandMsg:
		resolvedCommand := a.application.ResolveCommand(context.Background(), msg.Command)
		return a, core.CmdHandler(editor.SendMsg{Content: resolvedCommand})

	case commands.OpenURLMsg:
		_ = browser.Open(context.Background(), msg.URL)
		return a, nil

	case dialog.RuntimeResumeMsg:
		a.application.Resume(msg.Response)
		return a, nil

	// Handle SessionEvent - route to the correct tab based on session ID
	case app.SessionEvent:
		// Find the tab with matching session ID
		for i := range a.tabs {
			if a.tabs[i].Session.ID == msg.SessionID {
				// Route the unwrapped event to the correct tab
				updated, cmd := a.tabs[i].ChatPage.Update(msg.Event)
				a.tabs[i].ChatPage = updated.(chat.Page)
				return a, cmd
			}
		}
		// If we can't find the tab, just ignore the event
		return a, nil

	case error:
		a.err = msg
		return a, nil

	case editor.SendMsg:
		// Set the processing tab ID to track which tab initiated this request
		a.processingTabID = a.activeTab().ID

		// Forward the message to the active chat page
		updated, cmd := a.activeTab().ChatPage.Update(msg)
		a.tabs[a.activeTabIndex].ChatPage = updated.(chat.Page)
		return a, cmd

	case *runtime.StreamStoppedEvent:
		// Clear the processing tab ID when stream stops
		if a.processingTabID == a.activeTab().ID {
			a.processingTabID = ""
		}
		// Forward to active chat page
		updated, cmd := a.activeTab().ChatPage.Update(msg)
		a.tabs[a.activeTabIndex].ChatPage = updated.(chat.Page)
		return a, cmd

	default:
		if _, isRuntimeEvent := msg.(runtime.Event); isRuntimeEvent {
			// Only forward runtime events to the tab that initiated the request
			// If no tab is processing (processingTabID is empty), or if the active tab
			// is the one processing, forward the event
			if a.processingTabID == "" || a.activeTab().ID == a.processingTabID {
				updated, cmd := a.activeTab().ChatPage.Update(msg)
				a.tabs[a.activeTabIndex].ChatPage = updated.(chat.Page)
				return a, cmd
			}
			// Ignore events from non-active tabs during processing
			return a, nil
		}

		// For other messages, check if dialogs should handle them first
		// If dialogs are active, they get priority for input
		if a.dialog.Open() {
			u, dialogCmd := a.dialog.Update(msg)
			a.dialog = u.(dialog.Manager)
			return a, dialogCmd
		}

		var cmds []tea.Cmd
		var cmd tea.Cmd

		updated, cmd := a.completions.Update(msg)
		cmds = append(cmds, cmd)
		a.completions = updated.(completion.Manager)

		updated, cmd = a.activeTab().ChatPage.Update(msg)
		cmds = append(cmds, cmd)
		a.tabs[a.activeTabIndex].ChatPage = updated.(chat.Page)

		return a, tea.Batch(cmds...)
	}
}

// handleWindowResize processes window resize events
func (a *appModel) handleWindowResize(width, height int) tea.Cmd {
	var cmds []tea.Cmd

	// Update dimensions - account for status bar and tab bar
	tabBarHeight := a.tabBar.GetHeight()
	a.width, a.height = width, height-1-tabBarHeight // Account for status bar and tab bar

	if !a.ready {
		a.ready = true
	}

	// Update dialog system
	u, cmd := a.dialog.Update(tea.WindowSizeMsg{Width: width, Height: height})
	a.dialog = u.(dialog.Manager)
	cmds = append(cmds, cmd)

	// Update tab bar
	cmd = a.tabBar.SetSize(width, tabBarHeight)
	cmds = append(cmds, cmd)

	// Update active chat page
	if activeTab := a.activeTab(); activeTab != nil {
		cmd = activeTab.ChatPage.SetSize(a.width, a.height)
		cmds = append(cmds, cmd)
	}

	// Update status bar width
	a.statusBar.SetWidth(a.width)

	// Update notification size
	a.notification.SetSize(a.width, a.height)

	return tea.Batch(cmds...)
}

func (a *appModel) handleKeyPressMsg(msg tea.KeyPressMsg) tea.Cmd {
	if a.dialog.Open() {
		u, dialogCmd := a.dialog.Update(msg)
		a.dialog = u.(dialog.Manager)
		return dialogCmd
	}

	if a.completions.Open() {
		// Check if this is a navigation key that the completion manager should handle
		switch msg.String() {
		case "up", "down", "enter", "esc":
			// Let completion manager handle navigation keys
			u, completionCmd := a.completions.Update(msg)
			a.completions = u.(completion.Manager)
			return completionCmd
		default:
			// For all other keys (typing), send to both completion (for filtering) and editor
			var cmds []tea.Cmd
			u, completionCmd := a.completions.Update(msg)
			a.completions = u.(completion.Manager)
			cmds = append(cmds, completionCmd)

			// Also send to chat page/editor so user can continue typing
			updated, cmd := a.activeTab().ChatPage.Update(msg)
			a.tabs[a.activeTabIndex].ChatPage = updated.(chat.Page)
			cmds = append(cmds, cmd)

			return tea.Batch(cmds...)
		}
	}

	switch {
	case key.Matches(msg, a.keyMap.Quit):
		// Cleanup all tabs
		for _, tab := range a.tabs {
			tab.ChatPage.Cleanup()
		}
		return tea.Quit
	case key.Matches(msg, a.keyMap.NewTab):
		return core.CmdHandler(commands.CreateNewTabMsg{})
	case key.Matches(msg, a.keyMap.CloseTab):
		return core.CmdHandler(commands.CloseTabMsg{TabIndex: a.activeTabIndex})
	case key.Matches(msg, a.keyMap.NextTab):
		return core.CmdHandler(commands.NextTabMsg{})
	case key.Matches(msg, a.keyMap.PreviousTab):
		return core.CmdHandler(commands.PreviousTabMsg{})
	case key.Matches(msg, a.keyMap.CommandPalette):
		categories := commands.BuildCommandCategories(context.Background(), a.application)
		return core.CmdHandler(dialog.OpenDialogMsg{
			Model: dialog.NewCommandPaletteDialog(categories),
		})
	default:
		updated, cmd := a.activeTab().ChatPage.Update(msg)
		a.tabs[a.activeTabIndex].ChatPage = updated.(chat.Page)
		return cmd
	}
}

// View renders the complete application interface
func (a *appModel) View() tea.View {
	// Show error if present
	if a.err != nil {
		return toFullscreenView(styles.ErrorStyle.Render(a.err.Error()))
	}

	// Show loading if not ready
	if !a.ready {
		return toFullscreenView(
			styles.CenterStyle.
				Width(a.wWidth).
				Height(a.wHeight).
				Render(styles.MutedStyle.Render("Loading...")),
		)
	}

	// Render tab bar
	tabBarView := a.tabBar.View()

	// Render active chat page
	var pageView string
	if activeTab := a.activeTab(); activeTab != nil {
		pageView = activeTab.ChatPage.View()
	}

	// Create status bar
	statusBar := a.statusBar.View()

	// Combine all components
	var components []string
	if tabBarView != "" {
		components = append(components, tabBarView)
	}
	components = append(components, pageView)
	if statusBar != "" {
		components = append(components, statusBar)
	}

	baseView := lipgloss.JoinVertical(lipgloss.Top, components...)

	hasOverlays := a.dialog.Open() || a.notification.Open() || a.completions.Open()

	if hasOverlays {
		baseLayer := lipgloss.NewLayer(baseView)
		var allLayers []*lipgloss.Layer
		allLayers = append(allLayers, baseLayer)

		// Add dialog layers
		if a.dialog.Open() {
			dialogLayers := a.dialog.GetLayers()
			allLayers = append(allLayers, dialogLayers...)
		}

		if a.notification.Open() {
			allLayers = append(allLayers, a.notification.GetLayer())
		}

		if a.completions.Open() {
			layers := a.completions.GetLayers()
			allLayers = append(allLayers, layers...)
		}

		canvas := lipgloss.NewCanvas(allLayers...)
		return toFullscreenView(canvas.Render())
	}

	return toFullscreenView(baseView)
}

func toFullscreenView(content string) tea.View {
	view := tea.NewView(content)
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	view.BackgroundColor = styles.Background

	return view
}
