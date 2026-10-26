// Package contracttest checks the common runtime surface for all backends.
package contracttest

import (
	"context"
	"errors"
	"testing"

	"github.com/docker/docker-agent/pkg/runtime/contract"
	"github.com/docker/docker-agent/pkg/runtime/toolexec"
	"github.com/docker/docker-agent/pkg/tools"
)

// Run exercises every non-streaming method on the contract.Runtime
// interface. It runs against any factory so that LocalRuntime and (in a
// future commit) RemoteRuntime can be checked against the same expectations.
//
// Streaming methods (RunStream, Run, Summarize) are exercised by the
// type-specific test files because they need real models or providers.
func Run(t *testing.T, newRT func(t *testing.T) contract.Runtime) {
	t.Helper()

	t.Run("CurrentAgentName not empty", func(t *testing.T) {
		rt := newRT(t)
		t.Cleanup(func() { _ = rt.Close() })
		if rt.CurrentAgentName(t.Context()) == "" {
			t.Error("empty current agent")
		}
	})

	t.Run("CurrentAgentInfo carries the current name", func(t *testing.T) {
		rt := newRT(t)
		t.Cleanup(func() { _ = rt.Close() })
		info := rt.CurrentAgentInfo(t.Context())
		if rt.CurrentAgentName(t.Context()) != info.Name {
			t.Error("current agent info mismatch")
		}
	})

	t.Run("SetCurrentAgent rejects unknown agents", func(t *testing.T) {
		rt := newRT(t)
		t.Cleanup(func() { _ = rt.Close() })
		err := rt.SetCurrentAgent(t.Context(), "nonexistent-agent")
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("CurrentAgentTools returns slice or ErrUnsupported", func(t *testing.T) {
		rt := newRT(t)
		t.Cleanup(func() { _ = rt.Close() })
		_, err := rt.CurrentAgentTools(t.Context())
		if err != nil {
			if !errors.Is(err, contract.ErrUnsupported) {
				t.Errorf("unexpected error: %v", err)
			}
		}
	})

	t.Run("CurrentAgentToolsetStatuses does not panic", func(t *testing.T) {
		rt := newRT(t)
		t.Cleanup(func() { _ = rt.Close() })
		_ = rt.CurrentAgentToolsetStatuses()
	})

	t.Run("RestartToolset on unknown name returns error", func(t *testing.T) {
		rt := newRT(t)
		t.Cleanup(func() { _ = rt.Close() })
		err := rt.RestartToolset(t.Context(), "nonexistent-toolset")
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("CurrentMCPPrompts returns a non-nil map", func(t *testing.T) {
		rt := newRT(t)
		t.Cleanup(func() { _ = rt.Close() })
		prompts := rt.CurrentMCPPrompts(t.Context())
		if prompts == nil {
			t.Error("nil prompts map")
		}
	})

	t.Run("ExecuteMCPPrompt for unknown prompt returns error", func(t *testing.T) {
		rt := newRT(t)
		t.Cleanup(func() { _ = rt.Close() })
		_, err := rt.ExecuteMCPPrompt(t.Context(), "nonexistent-prompt", nil)
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("Steer accepts a message", func(t *testing.T) {
		rt := newRT(t)
		t.Cleanup(func() { _ = rt.Close() })
		err := rt.Steer(t.Context(), contract.QueuedMessage{Content: "hello"})
		if err != nil {
			if !errors.Is(err, contract.ErrUnsupported) {
				t.Errorf("unexpected error: %v", err)
			}
		}
	})

	t.Run("FollowUp accepts a message", func(t *testing.T) {
		rt := newRT(t)
		t.Cleanup(func() { _ = rt.Close() })
		err := rt.FollowUp(t.Context(), contract.QueuedMessage{Content: "hello"})
		if err != nil {
			if !errors.Is(err, contract.ErrUnsupported) {
				t.Errorf("unexpected error: %v", err)
			}
		}
	})

	t.Run("ResetStartupInfo is idempotent", func(t *testing.T) {
		rt := newRT(t)
		t.Cleanup(func() { _ = rt.Close() })
		rt.ResetStartupInfo()
		rt.ResetStartupInfo()
	})

	t.Run("Close is idempotent", func(t *testing.T) {
		rt := newRT(t)
		if err := rt.Close(); err != nil {
			t.Fatal(err)
		}
		// A second Close may error or not; both are acceptable as long as it
		// doesn't panic.
		_ = rt.Close()
	})

	t.Run("Resume on a fresh runtime is non-blocking", func(t *testing.T) {
		rt := newRT(t)
		t.Cleanup(func() { _ = rt.Close() })
		// No tool call is in flight; Resume must not deadlock.
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		rt.Resume(ctx, toolexec.ResumeRequest{Type: toolexec.ResumeTypeApprove})
	})

	t.Run("ResumeElicitation outside an elicitation does not panic", func(t *testing.T) {
		rt := newRT(t)
		t.Cleanup(func() { _ = rt.Close() })
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_ = rt.ResumeElicitation(ctx, tools.ElicitationActionDecline, nil, "")
	})
}
