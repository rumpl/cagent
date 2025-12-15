package editor

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"
	"github.com/docker/go-units"
	"github.com/mattn/go-runewidth"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/history"
	"github.com/docker/cagent/pkg/paths"
	"github.com/docker/cagent/pkg/tui/components/completion"
	"github.com/docker/cagent/pkg/tui/components/editor/completions"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/styles"
)

// ansiRegexp matches ANSI escape sequences so they can be removed when
// computing layout measurements.
var ansiRegexp = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

const (
	// maxInlinePasteLines is the maximum number of lines for inline paste.
	// Pastes exceeding this are buffered to a temp file attachment.
	maxInlinePasteLines = 5
	// maxInlinePasteChars is the character limit for inline pastes.
	// This catches very long single-line pastes that would clutter the editor.
	maxInlinePasteChars = 500
)

type attachment struct {
	path        string // Path to file (temp for pastes, real for file refs)
	placeholder string // @paste-1 or @filename
	label       string // Display label like "paste-1 (21.1 KB)"
	sizeBytes   int
	isTemp      bool // True for paste temp files that need cleanup
}

// AttachmentPreview describes an attachment and its contents for dialog display.
type AttachmentPreview struct {
	Title   string
	Content string
}

// SendMsg represents a message to send
type SendMsg struct {
	Content     string            // Full content sent to the agent (with file contents expanded)
	Attachments map[string]string // Map of filename to content for attachments
}

// Editor represents an input editor component
type Editor interface {
	layout.Model
	layout.Sizeable
	layout.Focusable
	SetWorking(working bool) tea.Cmd
	AcceptSuggestion() bool
	// Value returns the current editor content
	Value() string
	// SetValue updates the editor content
	SetValue(content string)
	Cleanup()
	GetSize() (width, height int)
	BannerHeight() int
	AttachmentAt(x int) (AttachmentPreview, bool)
}

// editor implements [Editor]
type editor struct {
	textarea textarea.Model
	hist     *history.History
	width    int
	height   int
	working  bool
	// completions are the available completions
	completions []completions.Completion

	// completionWord stores the word being completed
	completionWord    string
	currentCompletion completions.Completion

	suggestion    string
	hasSuggestion bool
	cursorHidden  bool
	// userTyped tracks whether the user has manually typed content (vs loaded from history)
	userTyped bool
	// keyboardEnhancementsSupported tracks whether the terminal supports keyboard enhancements
	keyboardEnhancementsSupported bool
	// pendingFileRef tracks the current @word being typed (for manual file ref detection).
	// Only set when cursor is in a word starting with @, cleared when cursor leaves.
	pendingFileRef string
	// attachments tracks all file attachments (pastes and file refs).
	attachments []attachment
	// pasteCounter tracks the next paste number for display purposes.
	pasteCounter int
}

// New creates a new editor component
func New(a *app.App, hist *history.History) Editor {
	ta := textarea.New()
	ta.SetStyles(styles.InputStyle)
	ta.Placeholder = "Type your message here..."
	ta.Prompt = "│ "
	ta.CharLimit = -1
	ta.SetWidth(50)
	ta.SetHeight(3) // Set minimum 3 lines for multi-line input
	ta.Focus()
	ta.ShowLineNumbers = false

	e := &editor{
		textarea:    ta,
		hist:        hist,
		completions: completions.Completions(a),
		// Default to no keyboard enhancements; ctrl+j will be used until we know otherwise
		keyboardEnhancementsSupported: false,
	}

	// Configure initial keybinding (ctrl+j for legacy terminals)
	e.configureNewlineKeybinding()

	return e
}

// Init initializes the component
func (e *editor) Init() tea.Cmd {
	return textarea.Blink
}

// stripANSI removes ANSI escape sequences from the provided string so width
// calculations can be performed on plain text.
func stripANSI(s string) string {
	return ansiRegexp.ReplaceAllString(s, "")
}

// lineHasContent reports whether the rendered line has user input after the
// prompt has been stripped.
func lineHasContent(line, prompt string) bool {
	plain := stripANSI(line)
	if prompt != "" && strings.HasPrefix(plain, prompt) {
		plain = strings.TrimPrefix(plain, prompt)
	}

	return strings.TrimSpace(plain) != ""
}

// lastInputLine returns the content of the final line from the textarea value,
// which is the portion eligible for suggestions.
func lastInputLine(value string) string {
	if idx := strings.LastIndex(value, "\n"); idx >= 0 {
		return value[idx+1:]
	}
	return value
}

// applySuggestionOverlay draws the inline suggestion on top of the textarea
// view using the configured ghost style.
func (e *editor) applySuggestionOverlay(view string) string {
	lines := strings.Split(view, "\n")
	targetLine := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if lineHasContent(lines[i], e.textarea.Prompt) {
			targetLine = i
			break
		}
	}

	if targetLine == -1 {
		return view
	}

	currentLine := lastInputLine(e.textarea.Value())
	promptWidth := runewidth.StringWidth(stripANSI(e.textarea.Prompt))
	textWidth := runewidth.StringWidth(currentLine)

	ghost := styles.SuggestionGhostStyle.Render(e.suggestion)

	baseLayer := lipgloss.NewLayer(view)
	overlay := lipgloss.NewLayer(ghost).
		X(promptWidth + textWidth).
		Y(targetLine)

	canvas := lipgloss.NewCanvas(baseLayer, overlay)
	return canvas.Render()
}

// refreshSuggestion updates the cached suggestion to reflect the current
// textarea value and available history entries.
func (e *editor) refreshSuggestion() {
	if e.hist == nil {
		e.clearSuggestion()
		return
	}

	current := e.textarea.Value()
	if current == "" {
		e.clearSuggestion()
		return
	}

	match := e.hist.LatestMatch(current)

	if match == "" || match == current || len(match) <= len(current) {
		e.clearSuggestion()
		return
	}

	e.suggestion = match[len(current):]
	if e.suggestion == "" {
		e.clearSuggestion()
		return
	}

	e.hasSuggestion = true
	e.setCursorHidden(true)
}

// clearSuggestion removes any pending suggestion and restores the cursor.
func (e *editor) clearSuggestion() {
	if !e.hasSuggestion && !e.cursorHidden {
		return
	}
	e.hasSuggestion = false
	e.suggestion = ""
	e.setCursorHidden(false)
}

// setCursorHidden toggles the virtual cursor so the ghost suggestion can be
// displayed without visual conflicts.
func (e *editor) setCursorHidden(hidden bool) {
	if e.cursorHidden == hidden {
		return
	}

	e.cursorHidden = hidden
	e.textarea.SetVirtualCursor(!hidden)
}

// AcceptSuggestion applies the current suggestion into the textarea value and
// returns true when a suggestion was committed.
func (e *editor) AcceptSuggestion() bool {
	if !e.hasSuggestion || e.suggestion == "" {
		return false
	}

	current := e.textarea.Value()
	e.textarea.SetValue(current + e.suggestion)
	e.textarea.MoveToEnd()

	e.clearSuggestion()

	return true
}

// configureNewlineKeybinding sets up the appropriate newline keybinding
// based on terminal keyboard enhancement support.
func (e *editor) configureNewlineKeybinding() {
	// Configure textarea's InsertNewline binding based on terminal capabilities
	if e.keyboardEnhancementsSupported {
		// Modern terminals: bind both shift+enter and ctrl+j
		e.textarea.KeyMap.InsertNewline.SetKeys("shift+enter", "ctrl+j")
		e.textarea.KeyMap.InsertNewline.SetEnabled(true)
	} else {
		// Legacy terminals: only ctrl+j works
		e.textarea.KeyMap.InsertNewline.SetKeys("ctrl+j")
		e.textarea.KeyMap.InsertNewline.SetEnabled(true)
	}
}

// findAttachmentAtCursor returns the attachment placeholder that the cursor is inside of or at the boundary of.
// Returns the start and end byte positions, and whether one was found.
func (e *editor) findAttachmentAtCursor() (int, int, bool) {
	value := e.textarea.Value()
	cursorPos := e.getCursorBytePos()

	for i := range e.attachments {
		att := &e.attachments[i]
		// Find all occurrences of this placeholder
		searchStart := 0
		for {
			idx := strings.Index(value[searchStart:], att.placeholder)
			if idx == -1 {
				break
			}
			start := searchStart + idx
			end := start + len(att.placeholder)

			// Check if cursor is at start, inside, or at end of this placeholder
			if cursorPos >= start && cursorPos <= end {
				return start, end, true
			}
			searchStart = end
		}
	}
	return 0, 0, false
}

// getCursorBytePos returns the cursor position in bytes.
func (e *editor) getCursorBytePos() int {
	info := e.textarea.LineInfo()
	value := e.textarea.Value()
	lines := strings.Split(value, "\n")

	pos := 0
	for i := 0; i < e.textarea.Line() && i < len(lines); i++ {
		pos += len(lines[i]) + 1 // +1 for newline
	}

	// Add column offset (in runes, need to convert to bytes)
	if e.textarea.Line() < len(lines) {
		line := lines[e.textarea.Line()]
		runes := []rune(line)
		for i := 0; i < info.ColumnOffset && i < len(runes); i++ {
			pos += len(string(runes[i]))
		}
	}

	return pos
}

// handleAttachmentNavigation handles left/right arrow keys to skip over attachments atomically.
// Returns true if the key was handled, false otherwise.
func (e *editor) handleAttachmentNavigation(keyStr string) bool {
	start, end, found := e.findAttachmentAtCursor()
	if !found {
		return false
	}

	cursorPos := e.getCursorBytePos()

	switch keyStr {
	case "left":
		// If cursor is at start, inside, or at end of attachment, jump to before it
		// We need to move one position before start
		if cursorPos >= start && cursorPos <= end {
			if start > 0 {
				// Move to position before the attachment
				e.setCursorToBytePos(start - 1)
			} else {
				// Attachment is at the beginning, just go to start
				e.setCursorToBytePos(0)
			}
			return true
		}
	case "right":
		// If cursor is at start or inside attachment, jump to after it
		if cursorPos >= start && cursorPos < end {
			e.setCursorToBytePos(end)
			return true
		}
	}

	return false
}

// handleAttachmentDeletion handles backspace/delete to remove attachments atomically.
// Returns true if the key was handled, false otherwise.
func (e *editor) handleAttachmentDeletion(keyStr string) bool {
	value := e.textarea.Value()
	cursorPos := e.getCursorBytePos()

	isBackspace := keyStr == "backspace" || keyStr == "ctrl+h"
	isDelete := keyStr == "delete" || keyStr == "ctrl+d"

	if isBackspace {
		// Check if cursor is inside or right after an attachment
		for i := range e.attachments {
			att := &e.attachments[i]
			idx := strings.Index(value, att.placeholder)
			if idx == -1 {
				continue
			}
			end := idx + len(att.placeholder)
			// If cursor is inside or right after the attachment
			if cursorPos > idx && cursorPos <= end {
				newValue := value[:idx] + value[end:]
				e.textarea.SetValue(newValue)
				e.setCursorToBytePos(idx)
				e.removeAttachment(i)
				return true
			}
		}
	}

	if isDelete {
		// Check if cursor is at or inside an attachment
		for i := range e.attachments {
			att := &e.attachments[i]
			idx := strings.Index(value, att.placeholder)
			if idx == -1 {
				continue
			}
			end := idx + len(att.placeholder)
			// If cursor is at start or inside the attachment
			if cursorPos >= idx && cursorPos < end {
				newValue := value[:idx] + value[end:]
				e.textarea.SetValue(newValue)
				e.setCursorToBytePos(idx)
				e.removeAttachment(i)
				return true
			}
		}
	}

	return false
}

// setCursorToBytePos sets the cursor to the given byte position.
func (e *editor) setCursorToBytePos(bytePos int) {
	value := e.textarea.Value()
	lines := strings.Split(value, "\n")

	targetLine := 0
	targetCol := 0
	currentPos := 0

	for i, line := range lines {
		lineLen := len(line)
		if currentPos+lineLen >= bytePos {
			targetLine = i
			// Convert byte offset within line to rune offset
			lineBytes := bytePos - currentPos
			targetCol = len([]rune(line[:lineBytes]))
			break
		}
		currentPos += lineLen + 1 // +1 for newline
		targetLine = i + 1
	}

	// Reset cursor position
	e.textarea.SetValue(value)
	e.textarea.MoveToBegin()
	for range targetLine {
		e.textarea.CursorDown()
	}
	e.textarea.CursorStart()
	e.textarea.SetCursorColumn(targetCol)
}

// removeAttachment removes an attachment by index and cleans up temp files.
func (e *editor) removeAttachment(idx int) {
	if idx < 0 || idx >= len(e.attachments) {
		return
	}
	att := e.attachments[idx]
	if att.isTemp {
		_ = os.Remove(att.path)
	}
	e.attachments = append(e.attachments[:idx], e.attachments[idx+1:]...)
}

// Update handles messages and updates the component state
func (e *editor) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.PasteMsg:
		if e.handlePaste(msg.Content) {
			return e, nil
		}
	case tea.KeyboardEnhancementsMsg:
		// Track keyboard enhancement support and configure newline keybinding accordingly
		e.keyboardEnhancementsSupported = msg.Flags != 0
		e.configureNewlineKeybinding()
		return e, nil
	case tea.WindowSizeMsg:
		e.textarea.SetWidth(msg.Width - 2)
		return e, nil

	// Handle mouse events
	case tea.MouseWheelMsg:
		// Forward mouse wheel as cursor movements to textarea for scrolling
		// This bypasses history navigation and allows viewport scrolling
		switch msg.Button.String() {
		case "wheelup":
			e.textarea.CursorUp()
		case "wheeldown":
			e.textarea.CursorDown()
		}
		return e, nil

	case tea.MouseClickMsg, tea.MouseMotionMsg, tea.MouseReleaseMsg:
		var cmd tea.Cmd
		e.textarea, cmd = e.textarea.Update(msg)
		if _, ok := msg.(tea.MouseClickMsg); ok {
			return e, tea.Batch(cmd, e.Focus())
		}
		return e, cmd

	case completion.SelectedMsg:
		currentValue := e.textarea.Value()
		lastIdx := strings.LastIndex(currentValue, e.completionWord)
		if e.currentCompletion.AutoSubmit() {
			if lastIdx >= 0 {
				newValue := currentValue[:lastIdx-1]
				e.textarea.SetValue(newValue)
				e.textarea.MoveToEnd()
			}
			if msg.Execute != nil {
				return e, msg.Execute()
			}
		} else {
			if lastIdx >= 0 {
				// Remove the @ and completion word, insert the selected value
				beforeTrigger := currentValue[:lastIdx-1]
				afterWord := currentValue[lastIdx+len(e.completionWord):]

				newValue := beforeTrigger + msg.Value + afterWord
				e.textarea.SetValue(newValue)
				e.textarea.MoveToEnd()

				// Add as attachment if it's a file reference
				if e.currentCompletion != nil && e.currentCompletion.Trigger() == "@" {
					e.addFileAttachment(msg.Value)
				}
			}
			e.clearSuggestion()
			return e, nil
		}
		return e, nil
	case completion.ClosedMsg:
		e.completionWord = ""
		return e, nil
	case tea.KeyPressMsg:
		if key.Matches(msg, e.textarea.KeyMap.Paste) {
			return e.handleClipboardPaste()
		}

		keyStr := msg.String()

		// Handle attachment-aware navigation
		if keyStr == "left" || keyStr == "right" {
			if e.handleAttachmentNavigation(keyStr) {
				return e, nil
			}
		}

		// Handle attachment-aware deletion
		if keyStr == "backspace" || keyStr == "ctrl+h" || keyStr == "delete" || keyStr == "ctrl+d" {
			if e.handleAttachmentDeletion(keyStr) {
				e.refreshSuggestion()
				return e, nil
			}
		}

		switch keyStr {
		case "enter", "shift+enter", "ctrl+j":
			if !e.textarea.Focused() {
				return e, nil
			}

			prev := e.textarea.Value()
			e.textarea, _ = e.textarea.Update(msg)
			value := e.textarea.Value()

			if value != prev && keyStr != "enter" {
				e.refreshSuggestion()
				return e, nil
			}

			if value != prev && keyStr == "enter" {
				if prev != "" && !e.working {
					e.tryAddFileRef(e.pendingFileRef)
					e.pendingFileRef = ""
					attachments := e.collectAttachments(prev)
					e.textarea.SetValue(prev)
					e.textarea.MoveToEnd()
					e.textarea.Reset()
					e.userTyped = false
					e.refreshSuggestion()
					return e, core.CmdHandler(SendMsg{Content: prev, Attachments: attachments})
				}
				return e, nil
			}

			if value != "" && !e.working {
				slog.Debug(value)
				e.tryAddFileRef(e.pendingFileRef)
				e.pendingFileRef = ""
				attachments := e.collectAttachments(value)
				e.textarea.Reset()
				e.userTyped = false
				e.refreshSuggestion()
				return e, core.CmdHandler(SendMsg{Content: value, Attachments: attachments})
			}

			return e, nil
		case "ctrl+c":
			return e, tea.Quit
		case "up":
			if !e.userTyped {
				e.textarea.SetValue(e.hist.Previous())
				e.textarea.MoveToEnd()
				e.refreshSuggestion()
				return e, nil
			}
		case "down":
			if !e.userTyped {
				e.textarea.SetValue(e.hist.Next())
				e.textarea.MoveToEnd()
				e.refreshSuggestion()
				return e, nil
			}
		default:
			for _, completion := range e.completions {
				if keyStr == completion.Trigger() {
					if completion.RequiresEmptyEditor() && e.textarea.Value() != "" {
						continue
					}
					cmds = append(cmds, e.startCompletion(completion))
				}
			}
		}
	}

	prevValue := e.textarea.Value()
	var cmd tea.Cmd
	e.textarea, cmd = e.textarea.Update(msg)
	cmds = append(cmds, cmd)

	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		if e.textarea.Value() != prevValue && keyMsg.String() != "up" && keyMsg.String() != "down" {
			e.userTyped = true
		}

		if e.textarea.Value() == "" {
			e.userTyped = false
		}

		currentWord := e.textarea.Word()

		if e.pendingFileRef != "" && currentWord != e.pendingFileRef {
			e.tryAddFileRef(e.pendingFileRef)
			e.pendingFileRef = ""
		}
		if e.pendingFileRef == "" && strings.HasPrefix(currentWord, "@") && len(currentWord) > 1 {
			e.pendingFileRef = currentWord
		} else if e.pendingFileRef != "" && strings.HasPrefix(currentWord, "@") {
			e.pendingFileRef = currentWord
		}

		if keyMsg.String() == "space" {
			e.completionWord = ""
			e.currentCompletion = nil
			cmds = append(cmds, core.CmdHandler(completion.CloseMsg{}))
		}

		if e.currentCompletion != nil && strings.HasPrefix(currentWord, e.currentCompletion.Trigger()) {
			e.completionWord = strings.TrimPrefix(currentWord, e.currentCompletion.Trigger())
			cmds = append(cmds, core.CmdHandler(completion.QueryMsg{Query: e.completionWord}))
		} else {
			e.completionWord = ""
			cmds = append(cmds, core.CmdHandler(completion.CloseMsg{}))
		}
	}

	e.refreshSuggestion()

	return e, tea.Batch(cmds...)
}

func (e *editor) handleClipboardPaste() (layout.Model, tea.Cmd) {
	content, err := clipboard.ReadAll()
	if err != nil {
		slog.Warn("failed to read clipboard", "error", err)
		return e, nil
	}

	if !e.handlePaste(content) {
		e.textarea.InsertString(content)
	}
	return e, textarea.Blink
}

func (e *editor) startCompletion(c completions.Completion) tea.Cmd {
	e.currentCompletion = c
	items := c.Items()

	if c.Trigger() == "@" {
		pasteItems := e.getPasteCompletionItems()
		if len(pasteItems) > 0 {
			items = append(pasteItems, items...)
		}
	}

	return core.CmdHandler(completion.OpenMsg{
		Items: items,
	})
}

func (e *editor) getPasteCompletionItems() []completion.Item {
	var items []completion.Item
	for _, att := range e.attachments {
		if !att.isTemp {
			continue
		}
		name := strings.TrimPrefix(att.placeholder, "@")
		items = append(items, completion.Item{
			Label:       name,
			Description: units.HumanSize(float64(att.sizeBytes)),
			Value:       att.placeholder,
			Pinned:      true,
		})
	}
	return items
}

// View renders the component with highlighted attachments
func (e *editor) View() string {
	view := e.textarea.View()

	// Apply attachment highlighting
	view = e.applyAttachmentHighlighting(view)

	if e.hasSuggestion && e.suggestion != "" {
		view = e.applySuggestionOverlay(view)
	}

	return styles.EditorStyle.Render(view)
}

// applyAttachmentHighlighting replaces attachment placeholders with styled versions in the view.
// This handles the case where the cursor might be inside an attachment by working on plain text.
func (e *editor) applyAttachmentHighlighting(view string) string {
	if len(e.attachments) == 0 {
		return view
	}

	// The view contains ANSI codes for cursor positioning etc.
	// We need to replace attachments carefully.
	// The textarea renders the value with the cursor embedded in it.
	// We'll replace each attachment placeholder with a styled version,
	// but we need to handle the case where cursor codes might be embedded.

	for _, att := range e.attachments {
		// Simple replacement works when cursor is not inside the attachment
		styled := styles.HighlightStyle.Render(att.placeholder)
		view = strings.ReplaceAll(view, att.placeholder, styled)
	}

	return view
}

// SetSize sets the dimensions of the component
func (e *editor) SetSize(width, height int) tea.Cmd {
	e.width = width
	e.height = max(height, 1)

	e.textarea.SetWidth(max(width, 10))
	e.textarea.SetHeight(e.height)

	return nil
}

// BannerHeight returns 0 since we no longer use the banner.
func (e *editor) BannerHeight() int {
	return 0
}

// GetSize returns the rendered dimensions including EditorStyle padding.
func (e *editor) GetSize() (width, height int) {
	return e.width + styles.EditorStyle.GetHorizontalFrameSize(),
		e.height + styles.EditorStyle.GetVerticalFrameSize()
}

// AttachmentAt is no longer used since we don't have a banner.
func (e *editor) AttachmentAt(x int) (AttachmentPreview, bool) {
	return AttachmentPreview{}, false
}

// Focus gives focus to the component
func (e *editor) Focus() tea.Cmd {
	return e.textarea.Focus()
}

// Blur removes focus from the component
func (e *editor) Blur() tea.Cmd {
	e.textarea.Blur()
	return nil
}

func (e *editor) SetWorking(working bool) tea.Cmd {
	e.working = working
	return nil
}

// Value returns the current editor content.
func (e *editor) Value() string {
	return e.textarea.Value()
}

// SetValue updates the editor content and moves cursor to end
func (e *editor) SetValue(content string) {
	e.textarea.SetValue(content)
	e.textarea.MoveToEnd()
	e.userTyped = content != ""
	e.refreshSuggestion()
}

// tryAddFileRef checks if word is a valid @filepath and adds it as attachment.
func (e *editor) tryAddFileRef(word string) {
	if !strings.HasPrefix(word, "@") || len(word) < 2 {
		return
	}

	if strings.HasPrefix(word, "@paste-") {
		return
	}

	path := word[1:]
	if !strings.ContainsAny(path, "/.") {
		return
	}

	e.addFileAttachment(word)
}

// addFileAttachment adds a file reference as an attachment if valid.
func (e *editor) addFileAttachment(placeholder string) {
	path := strings.TrimPrefix(placeholder, "@")

	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return
	}

	// Avoid duplicates
	for _, att := range e.attachments {
		if att.placeholder == placeholder {
			return
		}
	}

	e.attachments = append(e.attachments, attachment{
		path:        path,
		placeholder: placeholder,
		label:       fmt.Sprintf("%s (%s)", filepath.Base(path), units.HumanSize(float64(info.Size()))),
		sizeBytes:   int(info.Size()),
		isTemp:      false,
	})
}

// collectAttachments returns a map of placeholder to file content for all attachments.
func (e *editor) collectAttachments(content string) map[string]string {
	if len(e.attachments) == 0 {
		return nil
	}

	attachments := make(map[string]string)
	for _, att := range e.attachments {
		if !strings.Contains(content, att.placeholder) {
			if att.isTemp {
				_ = os.Remove(att.path)
			}
			continue
		}

		data, err := os.ReadFile(att.path)
		if err != nil {
			slog.Warn("failed to read attachment", "path", att.path, "error", err)
			if att.isTemp {
				_ = os.Remove(att.path)
			}
			continue
		}

		attachments[att.placeholder] = string(data)

		if att.isTemp {
			_ = os.Remove(att.path)
		}
	}
	e.attachments = nil

	return attachments
}

// Cleanup removes any temporary paste files that haven't been sent yet.
func (e *editor) Cleanup() {
	for _, att := range e.attachments {
		if att.isTemp {
			_ = os.Remove(att.path)
		}
	}
	e.attachments = nil
}

func (e *editor) handlePaste(content string) bool {
	lines := strings.Count(content, "\n") + 1
	if strings.HasSuffix(content, "\n") {
		lines--
	}

	if lines <= maxInlinePasteLines && len(content) <= maxInlinePasteChars {
		return false
	}

	e.pasteCounter++
	att, err := createPasteAttachment(content, e.pasteCounter)
	if err != nil {
		slog.Warn("failed to buffer paste", "error", err)
		return true
	}

	e.textarea.InsertString(att.placeholder)
	e.attachments = append(e.attachments, att)

	return true
}

func createPasteAttachment(content string, num int) (attachment, error) {
	pasteDir := filepath.Join(paths.GetDataDir(), "pastes")
	if err := os.MkdirAll(pasteDir, 0o700); err != nil {
		return attachment{}, fmt.Errorf("create paste dir: %w", err)
	}

	file, err := os.CreateTemp(pasteDir, "paste-*.txt")
	if err != nil {
		return attachment{}, fmt.Errorf("create paste file: %w", err)
	}
	defer file.Close()

	if _, err := file.WriteString(content); err != nil {
		return attachment{}, fmt.Errorf("write paste file: %w", err)
	}

	displayName := fmt.Sprintf("paste-%d", num)
	return attachment{
		path:        file.Name(),
		placeholder: "@" + displayName,
		label:       fmt.Sprintf("%s (%s)", displayName, units.HumanSize(float64(len(content)))),
		sizeBytes:   len(content),
		isTemp:      true,
	}, nil
}
