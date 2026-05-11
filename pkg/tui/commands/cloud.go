package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/docker/docker-agent/pkg/browser"
	"github.com/docker/docker-agent/pkg/cloudauth"
	"github.com/docker/docker-agent/pkg/cloudbridge"
	"github.com/docker/docker-agent/pkg/userconfig"
)

// cloudCommand returns the /cloud slash command.
//
// Usage:
//
//	/cloud         — start the device-flow login (no-op if already signed in)
//	/cloud logout  — remove the local credentials file
//
// Login output (user_code + verification_uri) and final status are surfaced
// via tea.Printf so they survive the TUI redraw cycle.
func cloudCommand() Item {
	return Item{
		ID:           "settings.cloud",
		Label:        "Cloud",
		SlashCommand: "/cloud",
		Description:  "Sign in to Agentic Platform (use /cloud logout to sign out)",
		Category:     "Settings",
		Immediate:    true,
		Execute: func(arg string) tea.Cmd {
			arg = strings.TrimSpace(arg)
			switch arg {
			case "logout":
				return cloudLogoutCmd()
			case "", "login":
				return cloudLoginCmd()
			default:
				return tea.Printf("Unknown /cloud subcommand %q (expected login or logout)\n", arg)
			}
		},
	}
}

func cloudLogoutCmd() tea.Cmd {
	return func() tea.Msg {
		if err := cloudauth.Logout(context.Background()); err != nil {
			return tea.Printf("✗ Failed to sign out: %v\n", err)()
		}
		return tea.Printf("✓ Signed out of Agentic Platform.\n")()
	}
}

func cloudLoginCmd() tea.Cmd {
	if cloudbridge.Enabled() {
		return tea.Printf("Already signed in. Use /cloud logout to sign out.\n")
	}

	endpoint := resolveCloudEndpoint()

	// Run the device-flow on a background goroutine so the TUI stays
	// responsive. We surface user_code via tea.Printf from a PromptFunc.
	return func() tea.Msg {
		ctx := context.Background()
		_, err := cloudauth.Login(ctx, endpoint, cloudauth.WithPrompt(func(userCode, verificationURI, verificationURIComplete string) {
			line := fmt.Sprintf("Sign in at %s — code: %s", verificationURI, userCode)
			if verificationURIComplete != "" {
				line = fmt.Sprintf("Sign in at %s (or %s) — code: %s",
					verificationURI, verificationURIComplete, userCode)
			}
			// Write directly to stderr so the message appears even before
			// tea has a chance to flush. tea.Printf from inside the
			// callback would not flush until the cmd returns.
			fmt.Fprintln(os.Stderr, line)

			// Pre-fill the user_code if the server gave us a complete URI;
			// fall back to the plain verification URI otherwise. The user
			// can still copy/paste manually if browser launch fails.
			openURL := verificationURIComplete
			if openURL == "" {
				openURL = verificationURI
			}
			if openURL != "" {
				if berr := browser.Open(context.Background(), openURL); berr != nil {
					fmt.Fprintf(os.Stderr, "(couldn't open browser automatically: %v)\n", berr)
				}
			}
		}))
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return tea.Printf("Login cancelled.\n")()
			}
			return tea.Printf("✗ Sign-in failed: %v\n", err)()
		}
		return tea.Printf("✓ Signed in to Agentic Platform.\n")()
	}
}

// resolveCloudEndpoint returns the AP endpoint configured in user config,
// falling back to cloudauth.DefaultEndpoint.
func resolveCloudEndpoint() string {
	cfg, err := userconfig.Load()
	if err == nil && cfg.Cloud != nil && cfg.Cloud.Endpoint != "" {
		return cfg.Cloud.Endpoint
	}
	return cloudauth.DefaultEndpoint
}
