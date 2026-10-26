package leantui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/leantui/ui"
	"github.com/docker/docker-agent/pkg/runtime/contract"
	"github.com/docker/docker-agent/pkg/session"
)

func leanTranscriptText(m *model) string {
	return ansi.Strip(strings.Join(m.screen.Transcript.Lines(100, 0, false, m.sessionState, nil), "\n"))
}

func TestCanonicalAssistantContentReconcilesLeanTranscript(t *testing.T) {
	t.Parallel()
	for _, partial := range []string{"", "FINAL-", "ANSWER", "FINAL-ANSWER"} {
		t.Run(partial, func(t *testing.T) {
			m := bareModel(40)
			m.handleEvent(t.Context(), contract.StreamStarted("session", "root"))
			if partial != "" {
				m.handleEvent(t.Context(), contract.AgentChoice("root", "session", partial, "answer"))
			}
			msg := session.NewAgentMessage("root", &chat.Message{Role: chat.MessageRoleAssistant, MessageID: "answer", Content: "FINAL-ANSWER"})
			for range 2 {
				m.handleEvent(t.Context(), contract.MessageAdded("session", msg, "root"))
			}
			require.Equal(t, 1, strings.Count(leanTranscriptText(m), "FINAL-ANSWER"))
		})
	}
}

func TestLeanRetryMessageIDsKeepAnswerVisible(t *testing.T) {
	t.Parallel()
	m := bareModel(40)
	m.handleEvent(t.Context(), contract.AgentChoice("root", "session", "[diagnostic](", "attempt-one"))
	m.handleEvent(t.Context(), contract.AgentChoice("root", "session", "FINAL-ANSWER)", "attempt-two"))
	require.Contains(t, leanTranscriptText(m), "FINAL-ANSWER")
}

func TestLeanInterleavedSessionsDoNotMergeMarkdown(t *testing.T) {
	t.Parallel()
	m := bareModel(40)
	m.handleEvent(t.Context(), contract.AgentChoice("root", "child", "[diagnostic](", "answer"))
	m.handleEvent(t.Context(), contract.AgentChoice("root", "parent", "FINAL-ANSWER)", "answer"))
	require.Contains(t, leanTranscriptText(m), "FINAL-ANSWER")
}

func TestRetiredLeanEventsCannotAffectReplacement(t *testing.T) {
	t.Parallel()
	m := bareModel(40)
	old := leanEvent{generation: 0, inner: contract.AgentChoice("root", "same-session", "OLD-ANSWER", "old")}
	m.eventGeneration.Add(1)
	m.handleEvent(t.Context(), old)
	m.handleEvent(t.Context(), leanEvent{generation: 1, inner: contract.AgentChoice("root", "same-session", "NEW-ANSWER", "new")})
	require.NotContains(t, leanTranscriptText(m), "OLD-ANSWER")
	require.Contains(t, leanTranscriptText(m), "NEW-ANSWER")
}

func TestLeanCanonicalContentCannotRevealSuppressedToolXML(t *testing.T) {
	t.Parallel()
	m := bareModel(40)
	msg := session.NewAgentMessage("root", &chat.Message{Role: chat.MessageRoleAssistant, MessageID: "answer", Content: `safe prefix<tool_call>{"arguments":"PRIVATE-TOOL-ARGUMENT`})
	m.handleEvent(t.Context(), contract.AgentChoice("root", "session", "safe prefix", "answer"))
	m.handleEvent(t.Context(), contract.MessageAdded("session", msg, "root"))
	require.Contains(t, leanTranscriptText(m), "safe prefix")
	require.NotContains(t, leanTranscriptText(m), "PRIVATE-TOOL-ARGUMENT")
}

func TestLeanRecoveryClearsNestedBusyWithoutDrainingQueue(t *testing.T) {
	t.Parallel()
	m := bareModel(40)
	for range 3 {
		m.handleEvent(t.Context(), contract.StreamStarted("session", "root"))
	}
	m.queue = []ui.PendingUserMessage{{Content: "must not run"}}
	m.handleEvent(t.Context(), contract.SessionRecovered(""))
	require.Zero(t, m.streamDepth)
	require.False(t, m.busy)
	require.Len(t, m.queue, 1)
}

func TestLeanRecoveryRestoresRootContextUsage(t *testing.T) {
	t.Parallel()
	m := bareModel(40)
	m.handleEvent(t.Context(), contract.StreamStarted("root", "root"))
	m.handleEvent(t.Context(), contract.NewTokenUsageEvent("root", "root", &contract.Usage{ContextLength: 100}))
	m.handleEvent(t.Context(), contract.StreamStarted("child", "child"))
	m.handleEvent(t.Context(), contract.NewTokenUsageEvent("child", "child", &contract.Usage{ContextLength: 999}))
	m.handleEvent(t.Context(), contract.SessionRecovered(""))
	m.handleEvent(t.Context(), contract.StreamStarted("root", "root"))
	m.handleEvent(t.Context(), contract.StreamStopped("root", "root", "normal"))
	require.Equal(t, int64(100), m.status.ContextLength)
}
