package editor

import (
	"regexp"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/history"
	"github.com/docker/cagent/pkg/tui/components/completion"
	"github.com/docker/cagent/pkg/tui/components/editor/completions"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/styles"
)

// ansiRegexp matches ANSI escape sequences so they can be removed when
// computing layout measurements.
var ansiRegexp = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// SendMsg represents a message to send
type SendMsg struct {
	Content string
}

// Editor represents an input editor component
type Editor interface {
	layout.Model
	layout.Sizeable
	layout.Focusable
	SetWorking(working bool) tea.Cmd
	AcceptSuggestion() bool
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
	// fileReferences tracks the file paths that were selected from autocompletion
	fileReferences map[string]struct{}
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
	ta.KeyMap.InsertNewline.SetEnabled(true) // Enable newline insertion

	return &editor{
		textarea:       ta,
		hist:           hist,
		completions:    completions.Completions(a),
		fileReferences: make(map[string]struct{}),
	}
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

// Update handles messages and updates the component state
func (e *editor) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		e.textarea.SetWidth(msg.Width - 2)
		return e, nil
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
				newValue := currentValue[:lastIdx-1] + msg.Value + currentValue[lastIdx+len(e.completionWord):]
				e.textarea.SetValue(newValue)
				e.textarea.MoveToEnd()
				// Track file reference if it was a file completion (@ trigger)
				if e.currentCompletion != nil {
					e.fileReferences[msg.Value] = struct{}{}
				}
			}
			return e, nil
		}
		return e, nil
	case completion.ClosedMsg:
		e.completionWord = ""
		return e, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		// Handle send/newline keys:
		// - Enter: submit current input (if textarea inserted a newline, submit previous buffer).
		// - Shift+Enter: insert newline when supported by terminal.
		// - Ctrl+J: fallback to insert '\n' when Shift+Enter isn’t reported.

		case "enter", "shift+enter", "ctrl+j":
			if !e.textarea.Focused() {
				return e, nil
			}

			prev := e.textarea.Value()
			e.textarea, _ = e.textarea.Update(msg)

			value := e.textarea.Value()

			// If the textarea inserted a newline due to plain Enter, submit the previous value.
			if value != prev && msg.String() == "enter" {
				if prev != "" && !e.working {
					e.textarea.SetValue(prev)
					e.textarea.MoveToEnd()
					e.textarea.Reset()
					e.userTyped = false
					e.fileReferences = make(map[string]struct{})
					e.refreshSuggestion()
					return e, core.CmdHandler(SendMsg{Content: prev})
				}
				return e, nil
			}

			// Shift+Enter: insert newline when supported by terminal (textarea handles it).
			if value != prev {
				e.refreshSuggestion()
				return e, nil
			}

			// Ctrl+J fallback: append LF to the buffer (always works across TTYs).
			if msg.String() == "ctrl+j" {
				current := e.textarea.Value()
				e.textarea.SetValue(current + "\n")
				e.textarea.MoveToEnd()
				e.refreshSuggestion()
				return e, nil
			}

			// Normal Enter submit (no textarea change): send current value.
			if value != "" && !e.working {
				e.textarea.Reset()
				e.userTyped = false
				e.fileReferences = make(map[string]struct{})
				e.refreshSuggestion()
				return e, core.CmdHandler(SendMsg{Content: value})
			}

			return e, nil
		case "ctrl+c":
			return e, tea.Quit
		case "up":
			// Only navigate history if the user hasn't manually typed content
			if !e.userTyped {
				e.textarea.SetValue(e.hist.Previous())
				e.textarea.MoveToEnd()
				e.refreshSuggestion()
				return e, nil
			}
			// Otherwise, let the textarea handle cursor navigation
		case "down":
			// Only navigate history if the user hasn't manually typed content
			if !e.userTyped {
				e.textarea.SetValue(e.hist.Next())
				e.textarea.MoveToEnd()
				e.refreshSuggestion()
				return e, nil
			}
			// Otherwise, let the textarea handle cursor navigation
		default:
			for _, completion := range e.completions {
				if msg.String() == completion.Trigger() {
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

	// If the value changed due to user input (not history navigation), mark as user typed
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		// Check if content changed and it wasn't a history navigation key
		if e.textarea.Value() != prevValue && keyMsg.String() != "up" && keyMsg.String() != "down" {
			e.userTyped = true
		}

		// Also check if textarea became empty - reset userTyped flag
		if e.textarea.Value() == "" {
			e.userTyped = false
		}
		if keyMsg.String() == "space" {
			e.completionWord = ""
			e.currentCompletion = nil
			cmds = append(cmds, core.CmdHandler(completion.CloseMsg{}))
		}

		currentWord := e.textarea.Word()
		if e.currentCompletion != nil && strings.HasPrefix(currentWord, e.currentCompletion.Trigger()) {
			e.completionWord = currentWord[1:]
			cmds = append(cmds, core.CmdHandler(completion.QueryMsg{Query: e.completionWord}))
		} else {
			e.completionWord = ""
			cmds = append(cmds, core.CmdHandler(completion.CloseMsg{}))
		}
	}

	e.refreshSuggestion()

	return e, tea.Batch(cmds...)
}

func (e *editor) startCompletion(c completions.Completion) tea.Cmd {
	e.currentCompletion = c
	return core.CmdHandler(completion.OpenMsg{
		Items: c.Items(),
	})
}

// applyFileReferenceHighlighting applies highlighting to file references in the view.
// It searches for known file references and renders them with the FileReferenceStyle.
func (e *editor) applyFileReferenceHighlighting(view string) string {
	if len(e.fileReferences) == 0 {
		return view
	}

	value := e.textarea.Value()
	prompt := e.textarea.Prompt
	promptWidth := runewidth.StringWidth(stripANSI(prompt))

	lines := strings.Split(view, "\n")
	valueLines := strings.Split(value, "\n")

	var layers []*lipgloss.Layer
	baseLayer := lipgloss.NewLayer(view)
	layers = append(layers, baseLayer)

	// For each line in the textarea value, find file references and create overlays
	for lineIdx, valueLine := range valueLines {
		if lineIdx >= len(lines) {
			break
		}

		for fileRef := range e.fileReferences {
			// Find all occurrences of this file reference in the line
			searchStart := 0
			for {
				idx := strings.Index(valueLine[searchStart:], fileRef)
				if idx == -1 {
					break
				}
				actualIdx := searchStart + idx

				// Calculate the x position accounting for the prompt
				xPos := promptWidth + runewidth.StringWidth(valueLine[:actualIdx])

				// Create highlighted overlay for this file reference
				highlighted := styles.FileReferenceStyle.Render(fileRef)
				overlay := lipgloss.NewLayer(highlighted).X(xPos).Y(lineIdx)
				layers = append(layers, overlay)

				searchStart = actualIdx + len(fileRef)
			}
		}
	}

	if len(layers) == 1 {
		// No overlays to apply
		return view
	}

	canvas := lipgloss.NewCanvas(layers...)
	return canvas.Render()
}

// View renders the component
func (e *editor) View() string {
	view := e.textarea.View()

	// Apply file reference highlighting
	view = e.applyFileReferenceHighlighting(view)

	if e.hasSuggestion && e.suggestion != "" {
		view = e.applySuggestionOverlay(view)
	}

	return styles.EditorStyle.Render(view)
}

// SetSize sets the dimensions of the component
func (e *editor) SetSize(width, height int) tea.Cmd {
	e.width = width
	e.height = height

	// Account for border and padding
	contentWidth := max(width, 10)
	contentHeight := max(height, 3) // Minimum 3 lines, but respect height parameter

	e.textarea.SetWidth(contentWidth)
	e.textarea.SetHeight(contentHeight)

	return nil
}

// GetSize returns the current dimensions
func (e *editor) GetSize() (width, height int) {
	return e.width, e.height
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
