package commands

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/docker/docker-agent/pkg/cloudbridge"
	"github.com/docker/docker-agent/pkg/tui/core"
	"github.com/docker/docker-agent/pkg/tui/messages"
)

// remoteControlCommand returns the /remote-control slash command.
//
// Usage:
//
//	/remote-control       — start mirroring this session to AP and accept
//	                        prompts pushed back from the web UI.
//	/remote-control off   — stop mirroring. AP retains whatever was already
//	                        mirrored; future updates stay local.
//
// The command only takes effect when the user is signed in to AP (i.e.
// /cloud login has been run). Without credentials, the bridge is inert and
// activation is a no-op.
func remoteControlCommand() Item {
	return Item{
		ID:           "settings.remote-control",
		Label:        "Remote Control",
		SlashCommand: "/remote-control",
		Description:  "Mirror this session to Agentic Platform (use /remote-control off to stop)",
		Category:     "Settings",
		Immediate:    true,
		Execute: func(arg string) tea.Cmd {
			if !cloudbridge.Enabled() {
				return tea.Printf("Sign in first with /cloud, then re-run /remote-control.\n")
			}
			enable := true
			switch strings.TrimSpace(arg) {
			case "":
				enable = true
			case "on", "start":
				enable = true
			case "off", "stop":
				enable = false
			default:
				return tea.Printf("Unknown /remote-control argument %q (expected 'on' or 'off')\n", arg)
			}
			return core.CmdHandler(messages.RemoteControlMsg{Enable: enable})
		},
	}
}
