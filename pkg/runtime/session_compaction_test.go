package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/tools"
)

func userItem(content string) session.Item {
	return session.Item{
		Message: &session.Message{
			Message: chat.Message{
				Role:    chat.MessageRoleUser,
				Content: content,
			},
		},
	}
}

func assistantItem(content string) session.Item {
	return session.Item{
		Message: &session.Message{
			Message: chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: content,
			},
		},
	}
}

func toolResultItem(content, toolCallID string) session.Item {
	return session.Item{
		Message: &session.Message{
			Message: chat.Message{
				Role:       chat.MessageRoleTool,
				Content:    content,
				ToolCallID: toolCallID,
			},
		},
	}
}

func assistantWithToolCalls(content string, toolCalls ...tools.ToolCall) session.Item {
	return session.Item{
		Message: &session.Message{
			Message: chat.Message{
				Role:      chat.MessageRoleAssistant,
				Content:   content,
				ToolCalls: toolCalls,
			},
		},
	}
}

func summaryItem(summary string) session.Item {
	return session.Item{Summary: summary}
}

// padContent creates a string that will estimate to approximately n tokens (4 chars per token).
func padContent(n int) string {
	b := make([]byte, n*4)
	for i := range b {
		b[i] = 'x'
	}
	return string(b)
}

func TestEstimateItemTokens(t *testing.T) {
	tests := []struct {
		name     string
		item     session.Item
		expected int
	}{
		{
			name:     "empty message",
			item:     userItem(""),
			expected: 0,
		},
		{
			name:     "simple user message",
			item:     userItem("hello world"), // 11 chars / 4 = 2
			expected: 2,
		},
		{
			name:     "summary item",
			item:     summaryItem("this is a summary text"), // 22 chars / 4 = 5
			expected: 5,
		},
		{
			name: "assistant with reasoning",
			item: session.Item{
				Message: &session.Message{
					Message: chat.Message{
						Role:             chat.MessageRoleAssistant,
						Content:          "response",     // 8 / 4 = 2
						ReasoningContent: "let me think", // 12 / 4 = 3
					},
				},
			},
			expected: 5,
		},
		{
			name: "assistant with tool calls",
			item: assistantWithToolCalls("", tools.ToolCall{
				Function: tools.FunctionCall{
					Name:      "read_file",         // 9 / 4 = 2
					Arguments: `{"path":"foo.go"}`, // 18 / 4 = 4
				},
			}),
			expected: 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateItemTokens(tt.item)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestShouldCompact(t *testing.T) {
	tests := []struct {
		name          string
		contextTokens int64
		contextWindow int64
		reserveTokens int
		expected      bool
	}{
		{
			name:          "below threshold",
			contextTokens: 80000,
			contextWindow: 100000,
			reserveTokens: 16384,
			expected:      false,
		},
		{
			name:          "above threshold",
			contextTokens: 90000,
			contextWindow: 100000,
			reserveTokens: 16384,
			expected:      true,
		},
		{
			name:          "at threshold",
			contextTokens: 83617, // 100000 - 16384 + 1
			contextWindow: 100000,
			reserveTokens: 16384,
			expected:      true,
		},
		{
			name:          "exactly at boundary",
			contextTokens: 83616, // 100000 - 16384
			contextWindow: 100000,
			reserveTokens: 16384,
			expected:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldCompact(tt.contextTokens, tt.contextWindow, tt.reserveTokens)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestPrepareCompaction_EmptySession(t *testing.T) {
	c := newSessionCompactor(session.NewInMemorySessionStore())
	sess := session.New()
	prep := c.prepareCompaction(sess)
	assert.Nil(t, prep)
}

func TestPrepareCompaction_AllFitsInBudget(t *testing.T) {
	c := newSessionCompactor(session.NewInMemorySessionStore())
	c.settings.KeepRecentTokens = 10000

	sess := session.New()
	sess.Messages = []session.Item{
		userItem("hello"),
		assistantItem("hi there"),
	}

	prep := c.prepareCompaction(sess)
	assert.Nil(t, prep, "should not compact when everything fits in the keep budget")
}

func TestPrepareCompaction_BasicCompaction(t *testing.T) {
	c := newSessionCompactor(session.NewInMemorySessionStore())
	c.settings.KeepRecentTokens = 50 // Very small budget

	sess := session.New()
	// Create items where the first ones exceed the budget
	sess.Messages = []session.Item{
		userItem(padContent(100)),      // 100 tokens - turn 1
		assistantItem(padContent(100)), // 100 tokens
		userItem(padContent(10)),       // 10 tokens - turn 2 (kept)
		assistantItem(padContent(10)),  // 10 tokens (kept)
	}

	prep := c.prepareCompaction(sess)
	require.NotNil(t, prep)
	assert.Equal(t, 2, prep.FirstKeptIndex)
	assert.Len(t, prep.MessagesToSummarize, 2)
	assert.False(t, prep.IsSplitTurn)
}

func TestPrepareCompaction_WithPreviousSummary(t *testing.T) {
	c := newSessionCompactor(session.NewInMemorySessionStore())
	c.settings.KeepRecentTokens = 50

	sess := session.New()
	sess.Messages = []session.Item{
		summaryItem("previous summary of conversation"),
		userItem(padContent(100)),      // 100 tokens
		assistantItem(padContent(100)), // 100 tokens
		userItem(padContent(10)),       // 10 tokens (kept)
		assistantItem(padContent(10)),  // 10 tokens (kept)
	}

	prep := c.prepareCompaction(sess)
	require.NotNil(t, prep)
	assert.Equal(t, "previous summary of conversation", prep.PreviousSummary)
	// Should only summarize items after the previous summary
	assert.Len(t, prep.MessagesToSummarize, 2)
	assert.Equal(t, 3, prep.FirstKeptIndex)
}

func TestPrepareCompaction_SplitTurn(t *testing.T) {
	c := newSessionCompactor(session.NewInMemorySessionStore())
	c.settings.KeepRecentTokens = 50

	sess := session.New()
	// One giant turn that exceeds the budget
	sess.Messages = []session.Item{
		userItem(padContent(100)),      // Start of turn
		assistantItem(padContent(100)), // Still in same turn
		toolResultItem(padContent(100), "call_1"),
		assistantItem(padContent(10)), // This fits in budget
	}

	prep := c.prepareCompaction(sess)
	require.NotNil(t, prep)
	assert.True(t, prep.IsSplitTurn)
	// No complete turns before the split, so MessagesToSummarize is empty
	assert.Empty(t, prep.MessagesToSummarize)
	assert.NotEmpty(t, prep.TurnPrefixMessages)
}

func TestPrepareCompaction_CutPointSnapping(t *testing.T) {
	c := newSessionCompactor(session.NewInMemorySessionStore())
	c.settings.KeepRecentTokens = 30

	sess := session.New()
	sess.Messages = []session.Item{
		userItem(padContent(100)),
		assistantWithToolCalls("", tools.ToolCall{
			Function: tools.FunctionCall{Name: "read", Arguments: `{"path":"a.go"}`},
		}),
		toolResultItem(padContent(100), "call_1"), // tool result - NOT a valid cut point
		userItem(padContent(10)),                  // valid cut point
		assistantItem(padContent(10)),
	}

	prep := c.prepareCompaction(sess)
	require.NotNil(t, prep)
	// Should snap to the user message (index 3), not the tool result (index 2)
	assert.Equal(t, 3, prep.FirstKeptIndex)
}

func TestIsValidCutPoint(t *testing.T) {
	tests := []struct {
		name     string
		item     session.Item
		expected bool
	}{
		{"user message", userItem("hello"), true},
		{"assistant message", assistantItem("hi"), true},
		{"tool result", toolResultItem("output", "call_1"), false},
		{"summary", summaryItem("summary text"), true},
		{"empty item", session.Item{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isValidCutPoint(tt.item))
		})
	}
}

func TestFindTurnStart(t *testing.T) {
	items := []session.Item{
		userItem("turn 1"),          // 0
		assistantItem("response 1"), // 1
		userItem("turn 2"),          // 2
		assistantItem("response 2"), // 3
		toolResultItem("out", "c1"), // 4
		assistantItem("response 3"), // 5
	}

	assert.Equal(t, 2, findTurnStart(items, 0, 5))
	assert.Equal(t, 2, findTurnStart(items, 0, 4))
	assert.Equal(t, 2, findTurnStart(items, 0, 3))
	assert.Equal(t, 0, findTurnStart(items, 0, 2))
	assert.Equal(t, 0, findTurnStart(items, 0, 1))
}

func TestIsAtTurnBoundary(t *testing.T) {
	items := []session.Item{
		userItem("turn 1"),          // 0
		assistantItem("response 1"), // 1
		userItem("turn 2"),          // 2
		assistantItem("response 2"), // 3
	}

	assert.True(t, isAtTurnBoundary(items, 0, 0))
	assert.False(t, isAtTurnBoundary(items, 0, 1))
	assert.True(t, isAtTurnBoundary(items, 0, 2))
	assert.False(t, isAtTurnBoundary(items, 0, 3))
}

func TestSerializeItems(t *testing.T) {
	items := []session.Item{
		userItem("What is Go?"),
		assistantItem("Go is a programming language."),
		assistantWithToolCalls("Let me check.", tools.ToolCall{
			Function: tools.FunctionCall{Name: "read_file", Arguments: `{"path":"main.go"}`},
		}),
		toolResultItem("package main\n\nfunc main() {}", "call_1"),
	}

	result := serializeItems(items)

	assert.Contains(t, result, "[User]: What is Go?")
	assert.Contains(t, result, "[Assistant]: Go is a programming language.")
	assert.Contains(t, result, "[Assistant]: Let me check.")
	assert.Contains(t, result, "[Assistant tool calls]: read_file")
	assert.Contains(t, result, `{"path":"main.go"}`)
	assert.Contains(t, result, "[Tool result]: package main")
}

func TestSerializeItems_WithSummary(t *testing.T) {
	items := []session.Item{
		summaryItem("previous context summary"),
		userItem("continue work"),
	}

	result := serializeItems(items)
	assert.Contains(t, result, "[Previous Summary]: previous context summary")
	assert.Contains(t, result, "[User]: continue work")
}

func TestSerializeItems_TruncatesLongToolResults(t *testing.T) {
	longContent := padContent(1000) // 4000 chars
	items := []session.Item{
		toolResultItem(longContent, "call_1"),
	}

	result := serializeItems(items)
	assert.Contains(t, result, "... [truncated]")
	assert.Less(t, len(result), len(longContent))
}

func TestSerializeItems_WithReasoning(t *testing.T) {
	items := []session.Item{
		{
			Message: &session.Message{
				Message: chat.Message{
					Role:             chat.MessageRoleAssistant,
					Content:          "The answer is 42.",
					ReasoningContent: "I need to think about the meaning of life.",
				},
			},
		},
	}

	result := serializeItems(items)
	assert.Contains(t, result, "[Assistant thinking]: I need to think about the meaning of life.")
	assert.Contains(t, result, "[Assistant]: The answer is 42.")
}

func TestSnapToValidCutPoint(t *testing.T) {
	items := []session.Item{
		userItem("turn 1"), // 0 - valid
		assistantWithToolCalls("", tools.ToolCall{ // 1 - valid (assistant)
			Function: tools.FunctionCall{Name: "read", Arguments: "{}"},
		}),
		toolResultItem("output", "call_1"),  // 2 - invalid (tool result)
		toolResultItem("output2", "call_2"), // 3 - invalid (tool result)
		userItem("turn 2"),                  // 4 - valid
		assistantItem("response"),           // 5 - valid
	}

	// If cut lands on tool result, should snap forward to the next user message
	assert.Equal(t, 4, snapToValidCutPoint(items, 0, 2))
	assert.Equal(t, 4, snapToValidCutPoint(items, 0, 3))

	// If cut is already at a valid point, stay there
	assert.Equal(t, 4, snapToValidCutPoint(items, 0, 4))
	assert.Equal(t, 5, snapToValidCutPoint(items, 0, 5))
}

func TestPrepareCompaction_MultipleTurns(t *testing.T) {
	c := newSessionCompactor(session.NewInMemorySessionStore())
	c.settings.KeepRecentTokens = 100

	sess := session.New()
	sess.Messages = []session.Item{
		userItem(padContent(50)),      // turn 1 - 50 tokens
		assistantItem(padContent(50)), // 50 tokens
		userItem(padContent(50)),      // turn 2 - 50 tokens
		assistantItem(padContent(50)), // 50 tokens
		userItem(padContent(20)),      // turn 3 - 20 tokens (kept)
		assistantItem(padContent(20)), // 20 tokens (kept)
	}

	prep := c.prepareCompaction(sess)
	require.NotNil(t, prep)
	// Walking backwards from end with budget 100:
	// i=5: 20, i=4: 40, i=3: 90, i=2: would exceed 100 → cutIdx=3
	// cutIdx=3 is an assistant message mid-turn (turn 2 started at index 2)
	// This is a split turn: turn 1 is complete, turn 2 is split
	assert.True(t, prep.IsSplitTurn)
	assert.Equal(t, 3, prep.FirstKeptIndex)
	assert.Len(t, prep.MessagesToSummarize, 2, "turn 1 should be summarized")
	assert.Len(t, prep.TurnPrefixMessages, 1, "first message of turn 2 is the prefix")
}

func TestPrepareCompaction_NothingAfterSummary(t *testing.T) {
	c := newSessionCompactor(session.NewInMemorySessionStore())
	c.settings.KeepRecentTokens = 10000

	sess := session.New()
	sess.Messages = []session.Item{
		userItem("hello"),
		assistantItem("world"),
		summaryItem("a summary"),
	}

	prep := c.prepareCompaction(sess)
	assert.Nil(t, prep, "should return nil when no items after last summary")
}

func TestSerializeMessage_SystemSkipped(t *testing.T) {
	items := []session.Item{
		{
			Message: &session.Message{
				Message: chat.Message{
					Role:    chat.MessageRoleSystem,
					Content: "You are a helpful assistant.",
				},
			},
		},
		userItem("hello"),
	}

	result := serializeItems(items)
	assert.NotContains(t, result, "You are a helpful assistant.")
	assert.Contains(t, result, "[User]: hello")
}

func TestPrepareCompaction_WithToolCallsNeverCutsAtToolResult(t *testing.T) {
	c := newSessionCompactor(session.NewInMemorySessionStore())
	c.settings.KeepRecentTokens = 60

	sess := session.New()
	sess.Messages = []session.Item{
		userItem(padContent(100)),
		assistantWithToolCalls(padContent(50), tools.ToolCall{
			Function: tools.FunctionCall{Name: "read", Arguments: `{"path":"a.go"}`},
		}),
		toolResultItem(padContent(50), "call_1"),
		userItem(padContent(10)),
		assistantItem(padContent(10)),
	}

	prep := c.prepareCompaction(sess)
	require.NotNil(t, prep)
	// The first kept index should be at the user message, never at the tool result
	keptItem := sess.Messages[prep.FirstKeptIndex]
	assert.NotEqual(t, chat.MessageRoleTool, keptItem.Message.Message.Role,
		"cut point should never land on a tool result message")
}

// TestSummaryInsertionPosition verifies that the summary is inserted right
// before the kept messages so that GetMessages (which reads from
// lastSummaryIndex+1 onward) includes the kept messages in the LLM context.
func TestSummaryInsertionPosition(t *testing.T) {
	c := newSessionCompactor(session.NewInMemorySessionStore())
	c.settings.KeepRecentTokens = 50

	sess := session.New()
	sess.Messages = []session.Item{
		userItem(padContent(100)),      // 0: summarized
		assistantItem(padContent(100)), // 1: summarized
		userItem("kept question"),      // 2: kept
		assistantItem("kept answer"),   // 3: kept
	}

	prep := c.prepareCompaction(sess)
	require.NotNil(t, prep)
	assert.Equal(t, 2, prep.FirstKeptIndex)

	// Simulate what Compact does: insert summary at FirstKeptIndex
	summaryText := "this is the compaction summary"
	insertIdx := prep.FirstKeptIndex
	sess.Messages = append(sess.Messages, session.Item{})
	copy(sess.Messages[insertIdx+1:], sess.Messages[insertIdx:])
	sess.Messages[insertIdx] = summaryItem(summaryText)

	// After insertion the layout should be:
	// [0] old user (summarized)
	// [1] old assistant (summarized)
	// [2] summary  <-- inserted here
	// [3] kept user
	// [4] kept assistant
	require.Len(t, sess.Messages, 5)
	assert.Equal(t, summaryText, sess.Messages[2].Summary)
	assert.Equal(t, "kept question", sess.Messages[3].Message.Message.Content)
	assert.Equal(t, "kept answer", sess.Messages[4].Message.Message.Content)

	// Now verify what GetMessages produces — this is what the LLM sees.
	a := agent.New("test", "you are a test agent")
	msgs := sess.GetMessages(a)

	// Filter out system messages to inspect conversation messages only.
	var conv []chat.Message
	for _, m := range msgs {
		if m.Role != chat.MessageRoleSystem {
			conv = append(conv, m)
		}
	}

	// The LLM should see: [summary as user msg] [kept user] [kept assistant]
	require.Len(t, conv, 3, "LLM should see summary + 2 kept messages")
	assert.Contains(t, conv[0].Content, summaryText, "first conv message should be the summary")
	assert.Equal(t, "kept question", conv[1].Content)
	assert.Equal(t, "kept answer", conv[2].Content)
}

// TestSummaryInsertionWithPreviousSummary verifies that a second compaction
// inserts its summary at the right position relative to the first summary.
func TestSummaryInsertionWithPreviousSummary(t *testing.T) {
	c := newSessionCompactor(session.NewInMemorySessionStore())
	c.settings.KeepRecentTokens = 50

	// Simulate a session that already had one compaction.
	sess := session.New()
	sess.Messages = []session.Item{
		userItem(padContent(100)),           // 0: old, before first summary
		assistantItem(padContent(100)),      // 1: old, before first summary
		summaryItem("first compaction"),     // 2: previous summary
		userItem(padContent(100)),           // 3: will be summarized
		assistantItem(padContent(100)),      // 4: will be summarized
		userItem("second kept question"),    // 5: kept
		assistantItem("second kept answer"), // 6: kept
	}

	prep := c.prepareCompaction(sess)
	require.NotNil(t, prep)
	assert.Equal(t, "first compaction", prep.PreviousSummary)

	// Insert summary at FirstKeptIndex
	summaryText := "second compaction"
	insertIdx := prep.FirstKeptIndex
	sess.Messages = append(sess.Messages, session.Item{})
	copy(sess.Messages[insertIdx+1:], sess.Messages[insertIdx:])
	sess.Messages[insertIdx] = summaryItem(summaryText)

	// Verify GetMessages sees only the latest summary + kept messages
	a := agent.New("test", "you are a test agent")
	msgs := sess.GetMessages(a)

	var conv []chat.Message
	for _, m := range msgs {
		if m.Role != chat.MessageRoleSystem {
			conv = append(conv, m)
		}
	}

	require.Len(t, conv, 3, "LLM should see second summary + 2 kept messages")
	assert.Contains(t, conv[0].Content, summaryText)
	assert.Equal(t, "second kept question", conv[1].Content)
	assert.Equal(t, "second kept answer", conv[2].Content)
}
