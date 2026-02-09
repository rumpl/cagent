package subsession

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/tools/builtin"
	"github.com/docker/cagent/pkg/tui/components/spinner"
	"github.com/docker/cagent/pkg/tui/components/toolcommon"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/styles"
	"github.com/docker/cagent/pkg/tui/types"
)

// New creates a new sub-session tool component.
func New(msg *types.Message, sessionState service.SessionStateReader) layout.Model {
	return toolcommon.NewBase(msg, sessionState, render)
}

func render(msg *types.Message, s spinner.Spinner, sessionState service.SessionStateReader, width, _ int) string {
	var params builtin.TransferTaskArgs
	if err := json.Unmarshal([]byte(msg.ToolCall.Function.Arguments), &params); err != nil {
		return ""
	}

	// Header: [caller] → [callee]
	header := styles.AgentBadgeStyle.MarginLeft(2).Render(msg.Sender) +
		" → " +
		styles.AgentBadgeStyle.Render(params.Agent)

	// Tool count
	toolCount := len(msg.SubSessionToolCalls)

	// Status icon
	var statusIcon string
	if msg.SubSessionActive {
		statusIcon = styles.NoStyle.MarginLeft(2).Render(s.View())
	} else {
		statusIcon = styles.ToolCompletedIcon.Render("✓")
	}

	// Tool count summary
	var countLabel string
	if toolCount == 1 {
		countLabel = "1 tool call"
	} else {
		countLabel = fmt.Sprintf("%d tool calls", toolCount)
	}
	summary := statusIcon + " " + styles.ToolMessageStyle.Render(countLabel)

	// When hideToolResults is false (expanded mode / ctrl+O toggled), show just the header.
	// The flat InSubSession messages below handle showing the full expanded content.
	if !sessionState.HideToolResults() {
		return renderExpanded(msg, s, header, summary, width)
	}

	// Collapsed: show header + summary + compact tool list
	return renderCollapsed(msg, header, summary, width)
}

// renderCollapsed shows the header, tool count, and a compact list of tool names.
func renderCollapsed(msg *types.Message, header, summary string, width int) string {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString(summary)

	if len(msg.SubSessionToolCalls) > 0 {
		b.WriteString("\n")
		for _, tc := range msg.SubSessionToolCalls {
			icon := toolStatusIcon(tc.Status)
			name := tc.ToolDefinition.DisplayName()
			if name == "" {
				name = tc.ToolCall.Function.Name
			}

			line := icon + " " + styles.ToolMessageStyle.Render(name)

			// Add a brief description from args if available
			desc := extractBriefArg(tc.ToolCall.Function.Arguments, tc.ToolCall.Function.Name)
			if desc != "" {
				available := width - lipgloss.Width(line) - 1
				if available > 10 {
					desc = toolcommon.TruncateText(desc, available)
					line += " " + styles.MutedStyle.Render(desc)
				}
			}

			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	content := strings.TrimRight(b.String(), "\n")
	return styles.RenderComposite(styles.ToolMessageStyle.Width(width), content)
}

// renderExpanded shows just the header for the sub-session.
// In expanded mode, the flat InSubSession messages below handle showing the full content.
func renderExpanded(msg *types.Message, s spinner.Spinner, header, summary string, width int) string {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString(summary)

	// Show spinner if still active
	if msg.SubSessionActive {
		b.WriteString("\n")
		b.WriteString(styles.NoStyle.MarginLeft(2).Render(s.View()) + " " + styles.MutedStyle.Render("running..."))
	}

	content := strings.TrimRight(b.String(), "\n")
	return styles.RenderComposite(styles.ToolMessageStyle.Width(width), content)
}

// toolStatusIcon returns the appropriate icon for a tool call status.
func toolStatusIcon(status types.ToolStatus) string {
	switch status {
	case types.ToolStatusCompleted:
		return styles.ToolCompletedIcon.Render("✓")
	case types.ToolStatusError:
		return styles.ToolErrorIcon.Render("✗")
	case types.ToolStatusRunning, types.ToolStatusPending:
		return styles.NoStyle.MarginLeft(2).Render("⋯")
	case types.ToolStatusConfirmation:
		return styles.ToolPendingIcon.Render("?")
	default:
		return styles.WarningStyle.Render("?")
	}
}

// extractBriefArg attempts to extract a meaningful brief description from tool arguments.
func extractBriefArg(args, _ string) string {
	if args == "" {
		return ""
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(args), &parsed); err != nil {
		return ""
	}

	// Try common field names in priority order
	for _, key := range []string{"path", "file_path", "filePath", "command", "cmd", "query", "url", "description"} {
		if v, ok := parsed[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}

	return ""
}
