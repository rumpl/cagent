package messages

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/types"
)

// renderedItem represents a cached rendered message with position information
type renderedItem struct {
	view   string // Cached rendered content
	height int    // Height in lines
}

// RenderCache manages caching of rendered message views
type RenderCache interface {
	// ContentProvider interface for viewport
	GetContent() string
	GetTotalHeight() int

	// Cache management
	Invalidate(index int)
	InvalidateAll()
	ShouldCache(msg *types.Message) bool

	// Rendering
	RenderAll(messages []*types.Message, views []layout.Model) string
	RenderItem(index int, msg *types.Message, view layout.Model) renderedItem
}

// renderCache implements RenderCache
type renderCache struct {
	renderedItems map[int]renderedItem
	rendered      string // Complete rendered content string
	totalHeight   int    // Total height of all content in lines
}

// NewRenderCache creates a new render cache
func NewRenderCache() RenderCache {
	return &renderCache{
		renderedItems: make(map[int]renderedItem),
	}
}

// GetContent returns the complete rendered content
func (rc *renderCache) GetContent() string {
	return rc.rendered
}

// GetTotalHeight returns the total height of all content in lines
func (rc *renderCache) GetTotalHeight() int {
	return rc.totalHeight
}

// Invalidate removes an item from cache, forcing re-render
func (rc *renderCache) Invalidate(index int) {
	delete(rc.renderedItems, index)
}

// InvalidateAll clears the entire cache
func (rc *renderCache) InvalidateAll() {
	rc.renderedItems = make(map[int]renderedItem)
	rc.rendered = ""
	rc.totalHeight = 0
}

// ShouldCache determines if a message should be cached based on its type and content
func (rc *renderCache) ShouldCache(msg *types.Message) bool {
	if msg == nil {
		return false
	}

	switch msg.Type {
	case types.MessageTypeToolCall:
		return msg.ToolStatus == types.ToolStatusCompleted ||
			msg.ToolStatus == types.ToolStatusError ||
			msg.ToolStatus == types.ToolStatusConfirmation
	case types.MessageTypeToolResult:
		return true
	case types.MessageTypeAssistant, types.MessageTypeAssistantReasoning:
		// Only cache assistant messages that have content (completed streaming)
		// Empty assistant messages have spinners and need constant re-rendering
		return strings.Trim(msg.Content, "\r\n\t ") != ""
	case types.MessageTypeUser, types.MessageTypeWelcome, types.MessageTypeCancelled,
		types.MessageTypeError, types.MessageTypeShellOutput:
		// Always cache static content
		return true
	default:
		// Unknown types - don't cache to be safe
		return false
	}
}

// RenderItem creates a renderedItem for a specific view with selective caching
func (rc *renderCache) RenderItem(index int, msg *types.Message, view layout.Model) renderedItem {
	// Only check cache for messages that should be cached
	if rc.ShouldCache(msg) {
		if cached, exists := rc.renderedItems[index]; exists {
			return cached
		}
	}

	// Render the item (always for dynamic content, or when not cached)
	rendered := view.View()
	height := lipgloss.Height(rendered)
	if rendered == "" {
		height = 0
	}

	item := renderedItem{
		view:   rendered,
		height: height,
	}

	// Only store in cache for messages that should be cached
	if rc.ShouldCache(msg) {
		rc.renderedItems[index] = item
	}

	return item
}

// RenderAll renders all message views and builds the complete content
func (rc *renderCache) RenderAll(messages []*types.Message, views []layout.Model) string {
	if len(views) == 0 {
		rc.rendered = ""
		rc.totalHeight = 0
		return ""
	}

	// Render all items and build the full content
	var allLines []string

	for i, view := range views {
		var msg *types.Message
		if i < len(messages) {
			msg = messages[i]
		}

		item := rc.RenderItem(i, msg, view)

		// Add content to complete rendered string
		if item.view != "" {
			lines := strings.Split(item.view, "\n")
			allLines = append(allLines, lines...)
		}

		// Add separator between messages (but not after last message)
		if i < len(views)-1 && item.view != "" {
			allLines = append(allLines, "")
		}
	}

	rc.rendered = strings.Join(allLines, "\n")
	rc.totalHeight = len(allLines)

	return rc.rendered
}
