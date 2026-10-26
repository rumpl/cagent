package leantui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/leantui/ui"
	"github.com/docker/docker-agent/pkg/runtime/contract"
	"github.com/docker/docker-agent/pkg/session"
)

func TestAgentInfoContextLimitShownBeforeUsage(t *testing.T) {
	t.Parallel()
	m := bareModel(24)

	m.handleEvent(t.Context(), contract.AgentInfo("root", "test/model", "", "", 200_000))

	assert.Equal(t, int64(200_000), m.status.ContextLimit)
	assert.Contains(t, ui.RenderContext(m.status), "0% · 0/200.0k")
}

func TestTokenUsageEventAggregatesSessionCost(t *testing.T) {
	t.Parallel()
	m := bareModel(24)

	m.handleEvent(t.Context(), contract.StreamStarted("root-session", "root"))
	m.handleEvent(t.Context(), contract.NewTokenUsageEvent("root-session", "root", &contract.Usage{
		InputTokens:   2_000,
		OutputTokens:  1_000,
		ContextLength: 3_000,
		ContextLimit:  10_000,
		Cost:          0.10,
	}))
	m.handleEvent(t.Context(), contract.StreamStarted("child-session", "developer"))
	m.handleEvent(t.Context(), contract.NewTokenUsageEvent("child-session", "developer", &contract.Usage{
		InputTokens:   800,
		OutputTokens:  200,
		ContextLength: 1_000,
		ContextLimit:  20_000,
		Cost:          0.05,
	}))

	assert.Equal(t, int64(1_000), m.status.Tokens)
	assert.InDelta(t, 0.15, m.status.Cost, 0.0001)
	assert.True(t, m.status.CostKnown)
	assert.Contains(t, strings.Join(ui.RenderStatus(m.status, 80), "\n"), "$0.15")

	m.handleEvent(t.Context(), contract.StreamStopped("child-session", "developer", "normal"))

	assert.Equal(t, int64(3_000), m.status.Tokens)
	assert.InDelta(t, 0.15, m.status.Cost, 0.0001)
}

func TestTokenUsageBeforeStreamUsesFirstSessionAsRoot(t *testing.T) {
	t.Parallel()
	m := bareModel(24)

	m.handleEvent(t.Context(), contract.NewTokenUsageEvent("root-session", "root", &contract.Usage{
		InputTokens:   2_000,
		OutputTokens:  1_000,
		ContextLength: 3_000,
		ContextLimit:  10_000,
		Cost:          0.10,
	}))
	m.handleEvent(t.Context(), contract.NewTokenUsageEvent("child-session", "developer", &contract.Usage{
		InputTokens:   800,
		OutputTokens:  200,
		ContextLength: 1_000,
		ContextLimit:  20_000,
		Cost:          0.05,
	}))

	assert.Equal(t, "root-session", m.usage.RootSessionID())
	assert.Equal(t, int64(3_000), m.status.Tokens)
	assert.InDelta(t, 0.15, m.status.Cost, 0.0001)
}

func TestEmptySessionUsageDoesNotOverrideSessionScopedUsage(t *testing.T) {
	t.Parallel()
	m := bareModel(24)

	m.handleEvent(t.Context(), contract.NewTokenUsageEvent("root-session", "root", &contract.Usage{
		InputTokens:   2_000,
		OutputTokens:  1_000,
		ContextLength: 3_000,
		ContextLimit:  10_000,
		Cost:          0.10,
	}))
	m.handleEvent(t.Context(), contract.NewTokenUsageEvent("", "root", &contract.Usage{
		InputTokens:   50,
		ContextLength: 50,
		Cost:          0.99,
	}))

	assert.Equal(t, int64(3_000), m.status.Tokens)
	assert.InDelta(t, 0.10, m.status.Cost, 0.0001)
}

// leantuiCostItem returns an assistant-message item carrying the given cost.
func leantuiCostItem(agentName string, cost float64) session.Item {
	return session.Item{Message: &session.Message{
		AgentName: agentName,
		Message:   chat.Message{Role: chat.MessageRoleAssistant, Content: "done", Cost: cost},
	}}
}

// TestRestoredSessionCostDoesNotDecreaseAfterCompaction is a regression test
// for the footer total dropping after a session restore: the startup usage
// event carries the restored total (own + embedded sub-session costs), and
// the next root event — compaction emits one right away — used to replace it
// with an own-cost-only snapshot, shrinking the total.
func TestRestoredSessionCostDoesNotDecreaseAfterCompaction(t *testing.T) {
	t.Parallel()
	m := bareModel(24)

	// Restored session: $0.10 direct + $0.05 embedded sub-session.
	sess := session.New()
	sess.InputTokens = 800
	sess.OutputTokens = 200
	sess.Messages = append(sess.Messages, leantuiCostItem("root", 0.10))
	sub := session.New(session.WithParentID(sess.ID))
	sub.Messages = append(sub.Messages, leantuiCostItem("developer", 0.05))
	sess.Messages = append(sess.Messages, session.Item{SubSession: sub})

	// Session restore: EmitStartupInfo emits the restored session's usage.
	m.handleEvent(t.Context(), contract.NewTokenUsageEvent(sess.ID, "root", contract.SessionUsage(sess, 10_000)))
	assert.InDelta(t, 0.15, m.status.Cost, 0.0001)

	// Compaction appends a $0.01 summary and immediately emits the session's
	// usage (see compactWithReason).
	sess.ApplyCompaction(100, 0, session.Item{Summary: "summary", Cost: 0.01})
	m.handleEvent(t.Context(), contract.NewTokenUsageEvent(sess.ID, "root", contract.SessionUsage(sess, 10_000)))
	assert.InDelta(t, 0.16, m.status.Cost, 0.0001,
		"the total must keep the embedded sub-session cost and gain the summary cost")
}

// TestLiveSubSessionCostNotDoubleCounted drives the live delegation flow
// through the runtime's own usage snapshots: the child reports its own cost
// while it runs, and once it completes and attaches to the root the root's
// next event must not fold the child in again.
func TestLiveSubSessionCostNotDoubleCounted(t *testing.T) {
	t.Parallel()
	m := bareModel(24)

	root := session.New()
	root.Messages = append(root.Messages, leantuiCostItem("root", 0.10))
	m.handleEvent(t.Context(), contract.StreamStarted(root.ID, "root"))
	m.handleEvent(t.Context(), contract.NewTokenUsageEvent(root.ID, "root", contract.SessionUsage(root, 10_000)))

	child := session.New(session.WithParentID(root.ID))
	child.Messages = append(child.Messages, leantuiCostItem("developer", 0.05))
	m.handleEvent(t.Context(), contract.StreamStarted(child.ID, "developer"))
	m.handleEvent(t.Context(), contract.NewTokenUsageEvent(child.ID, "developer", contract.SessionUsage(child, 10_000)))
	assert.InDelta(t, 0.15, m.status.Cost, 0.0001)

	// Child completes: its stream stops and it is attached to the root.
	m.handleEvent(t.Context(), contract.StreamStopped(child.ID, "developer", "normal"))
	root.AddLiveSubSession(child)

	m.handleEvent(t.Context(), contract.NewTokenUsageEvent(root.ID, "root", contract.SessionUsage(root, 10_000)))
	assert.InDelta(t, 0.15, m.status.Cost, 0.0001, "root own + child own, not double-counted")
}

// TestSessionCompactionDrivesGaugeState verifies the status line adopts the
// usage event's compaction threshold and flips to "compacting…" for the
// started→completed window of a SessionCompactionEvent.
func TestSessionCompactionDrivesGaugeState(t *testing.T) {
	t.Parallel()
	m := bareModel(24)

	m.handleEvent(t.Context(), contract.StreamStarted("root-session", "root"))
	m.handleEvent(t.Context(), contract.NewTokenUsageEvent("root-session", "root", &contract.Usage{
		InputTokens:         6_000,
		OutputTokens:        3_000,
		ContextLength:       9_000,
		ContextLimit:        10_000,
		CompactionThreshold: 0.95,
	}))
	assert.InDelta(t, 0.95, m.status.CompactionThreshold, 0.0001)
	assert.Contains(t, ui.RenderContext(m.status), "90%")

	m.handleEvent(t.Context(), contract.SessionCompaction("root-session", "started", "root"))
	assert.True(t, m.status.Compacting)
	out := ui.RenderContext(m.status)
	assert.Contains(t, out, "compacting…")
	assert.NotContains(t, out, "90%")

	m.handleEvent(t.Context(), contract.SessionCompactionCompleted("root-session", contract.CompactionOutcomeApplied, "root"))
	assert.False(t, m.status.Compacting)
	out = ui.RenderContext(m.status)
	assert.NotContains(t, out, "compacting…")
	assert.Contains(t, out, "90%")
}
