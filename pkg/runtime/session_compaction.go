package runtime

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/model/provider"
	"github.com/docker/docker-agent/pkg/model/provider/options"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/team"
)

//go:embed prompts/compaction-system.txt
var compactionSystemPrompt string

//go:embed prompts/compaction-user.txt
var compactionUserPrompt string

const (
	defaultReserveTokens    = 16384
	defaultKeepRecentTokens = 20000
)

// CompactionSettings controls the compaction behavior.
type CompactionSettings struct {
	ReserveTokens    int
	KeepRecentTokens int
}

// DefaultCompactionSettings returns the default compaction settings.
func DefaultCompactionSettings() CompactionSettings {
	return CompactionSettings{
		ReserveTokens:    defaultReserveTokens,
		KeepRecentTokens: defaultKeepRecentTokens,
	}
}

// CompactionPreparation holds all the information needed to perform compaction.
type CompactionPreparation struct {
	// MessagesToSummarize are the complete turns that will be summarized.
	MessagesToSummarize []session.Item

	// TurnPrefixMessages are messages from a split turn's early part (only set when IsSplitTurn is true).
	TurnPrefixMessages []session.Item

	// IsSplitTurn is true when a single turn exceeds KeepRecentTokens and the cut
	// point lands mid-turn at an assistant message.
	IsSplitTurn bool

	// PreviousSummary is the summary from the most recent compaction, if any.
	PreviousSummary string

	// FirstKeptIndex is the index into session.Messages where kept messages begin.
	FirstKeptIndex int
}

type summaryResult struct {
	Summary string
	Cost    float64
}

type sessionCompactor struct {
	sessionStore session.Store
	settings     CompactionSettings
}

func newSessionCompactor(sessionStore session.Store) *sessionCompactor {
	return &sessionCompactor{
		sessionStore: sessionStore,
		settings:     DefaultCompactionSettings(),
	}
}

// ShouldCompact returns true if the context token count exceeds the threshold.
func ShouldCompact(contextTokens, contextWindow int64, reserveTokens int) bool {
	if contextWindow <= 0 {
		return false
	}

	threshold := max(contextWindow-int64(reserveTokens), 0)
	return contextTokens > threshold
}

// Compact generates a summary for older messages and inserts it before the kept tail.
func (c *sessionCompactor) Compact(ctx context.Context, sess *session.Session, model provider.Provider, additionalPrompt string, events chan Event, agentName string) {
	events <- SessionCompaction(sess.ID, "started", agentName)
	defer func() {
		events <- SessionCompaction(sess.ID, "completed", agentName)
	}()

	prep := c.prepareCompaction(sess)
	if prep == nil {
		events <- Warning("Session is empty. Start a conversation before compacting.", agentName)
		return
	}

	result, err := c.generateSummary(ctx, model, prep, additionalPrompt)
	if err != nil {
		slog.Error("Failed to generate session summary", "session_id", sess.ID, "error", err)
		events <- Error(err.Error())
		return
	}
	if result.Summary == "" {
		return
	}

	summaryItem := session.Item{Summary: result.Summary, Cost: result.Cost}

	// Insert the summary right before FirstKeptIndex so that GetMessages
	// (which starts reading from lastSummaryIndex+1) will see the kept
	// messages immediately after the summary.
	insertIdx := prep.FirstKeptIndex
	sess.Messages = append(sess.Messages, session.Item{})
	copy(sess.Messages[insertIdx+1:], sess.Messages[insertIdx:])
	sess.Messages[insertIdx] = summaryItem

	// Reset the token counters to reflect the compacted context. We only keep an
	// estimate here; the next provider response will replace it with real usage.
	sess.InputTokens = int64(estimateTotalTokens(sess.Messages, insertIdx))
	sess.OutputTokens = 0
	sess.Cost = sess.OwnCost()

	if err := c.sessionStore.UpdateSession(ctx, sess); err != nil {
		slog.Error("Compaction failed: could not persist session",
			"session_id", sess.ID,
			"error", err)
	}

	slog.Debug("Generated session summary",
		"session_id", sess.ID,
		"summary_length", len(result.Summary),
		"compaction_cost", result.Cost,
		"input_tokens", sess.InputTokens,
	)
	events <- SessionSummary(sess.ID, result.Summary, agentName)
}

// prepareCompaction analyzes the session and determines what to summarize.
// Returns nil if there is nothing to summarize.
func (c *sessionCompactor) prepareCompaction(sess *session.Session) *CompactionPreparation {
	items := sess.Messages
	if len(items) == 0 {
		return nil
	}

	// Find the last summary index (previous compaction).
	lastSummaryIdx := -1
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].Summary != "" {
			lastSummaryIdx = i
			break
		}
	}

	// Determine the range of items to consider (after last summary).
	startIdx := lastSummaryIdx + 1
	if startIdx >= len(items) {
		return nil
	}

	// Calculate cut point based on token budget.
	cutIdx := c.findCutIndex(items, startIdx)
	if cutIdx <= startIdx {
		return nil
	}

	prep := &CompactionPreparation{FirstKeptIndex: cutIdx}
	if lastSummaryIdx >= 0 {
		prep.PreviousSummary = items[lastSummaryIdx].Summary
	}

	c.analyzeSplitTurn(prep, items, startIdx, cutIdx)
	return prep
}

func (c *sessionCompactor) findCutIndex(items []session.Item, startIdx int) int {
	keepBudget := c.settings.KeepRecentTokens
	tokensAccumulated := 0

	for i := len(items) - 1; i >= startIdx; i-- {
		tokens := estimateItemTokens(items[i])
		if tokensAccumulated+tokens > keepBudget {
			cutIdx := i + 1
			return snapToValidCutPoint(items, startIdx, cutIdx)
		}
		tokensAccumulated += tokens
	}

	return startIdx
}

func (c *sessionCompactor) analyzeSplitTurn(prep *CompactionPreparation, items []session.Item, startIdx, cutIdx int) {
	turnStartIdx := findTurnStart(items, startIdx, cutIdx)
	isSplitTurn := turnStartIdx >= startIdx && cutIdx > turnStartIdx && !isAtTurnBoundary(items, startIdx, cutIdx)

	if isSplitTurn {
		prep.IsSplitTurn = true
		if turnStartIdx > startIdx {
			prep.MessagesToSummarize = items[startIdx:turnStartIdx]
		}
		prep.TurnPrefixMessages = items[turnStartIdx:cutIdx]
		return
	}

	prep.MessagesToSummarize = items[startIdx:cutIdx]
}

// generateSummary calls the LLM to create a structured summary.
func (c *sessionCompactor) generateSummary(ctx context.Context, model provider.Provider, prep *CompactionPreparation, additionalPrompt string) (summaryResult, error) {
	if prep.IsSplitTurn && len(prep.MessagesToSummarize) == 0 && len(prep.TurnPrefixMessages) == 0 {
		return summaryResult{}, nil
	}

	if prep.IsSplitTurn {
		return c.generateSplitTurnSummary(ctx, model, prep, additionalPrompt)
	}

	return c.generateSimpleSummary(ctx, model, prep, additionalPrompt)
}

// generateSimpleSummary generates a summary for non-split-turn compaction.
func (c *sessionCompactor) generateSimpleSummary(ctx context.Context, model provider.Provider, prep *CompactionPreparation, additionalPrompt string) (summaryResult, error) {
	var conversationText string
	if prep.PreviousSummary != "" {
		conversationText = "[Previous Summary]\n" + prep.PreviousSummary + "\n\n"
	}
	conversationText += serializeItems(prep.MessagesToSummarize)

	return c.callSummaryModel(ctx, model, conversationText, additionalPrompt)
}

// generateSplitTurnSummary handles the case where a single turn exceeds the keep budget.
// It generates two summaries and merges them.
func (c *sessionCompactor) generateSplitTurnSummary(ctx context.Context, model provider.Provider, prep *CompactionPreparation, additionalPrompt string) (summaryResult, error) {
	var parts []summaryResult

	// Part 1: History summary (previous summary + complete turns before the split).
	if prep.PreviousSummary != "" || len(prep.MessagesToSummarize) > 0 {
		historyText := serializeItems(prep.MessagesToSummarize)
		if prep.PreviousSummary != "" {
			historyText = "[Previous Summary]\n" + prep.PreviousSummary + "\n\n" + historyText
		}

		part, err := c.callSummaryModel(ctx, model, historyText, additionalPrompt)
		if err != nil {
			return summaryResult{}, err
		}
		if part.Summary != "" {
			parts = append(parts, part)
		}
	}

	// Part 2: Turn prefix summary.
	if len(prep.TurnPrefixMessages) > 0 {
		part, err := c.summarizePart(ctx, model, prep.TurnPrefixMessages, additionalPrompt)
		if err != nil {
			return summaryResult{}, err
		}
		if part.Summary != "" {
			parts = append(parts, part)
		}
	}

	if len(parts) == 0 {
		return summaryResult{}, nil
	}
	if len(parts) == 1 {
		return parts[0], nil
	}

	mergeText := "[Summary 1 - Previous History]\n" + parts[0].Summary + "\n\n[Summary 2 - Current Turn Prefix]\n" + parts[1].Summary
	merged, err := c.callSummaryModel(ctx, model, mergeText, "Merge these two summaries into a single coherent summary, preserving all important details from both.")
	if err != nil {
		return summaryResult{}, err
	}
	merged.Cost += parts[0].Cost + parts[1].Cost
	return merged, nil
}

func (c *sessionCompactor) summarizePart(ctx context.Context, model provider.Provider, items []session.Item, prompt string) (summaryResult, error) {
	return c.callSummaryModel(ctx, model, serializeItems(items), prompt)
}

// callSummaryModel invokes the LLM with the conversation text and returns the summary.
func (c *sessionCompactor) callSummaryModel(ctx context.Context, model provider.Provider, conversationText, additionalPrompt string) (summaryResult, error) {
	summaryModel := provider.CloneWithOptions(ctx, model, options.WithStructuredOutput(nil))
	root := agent.New("root", compactionSystemPrompt, agent.WithModel(summaryModel))
	newTeam := team.New(team.WithAgents(root))

	summarySession := session.New()
	summarySession.Title = "Generating summary..."
	summarySession.AddMessage(&session.Message{
		Message: chat.Message{
			Role:      chat.MessageRoleUser,
			Content:   conversationText,
			CreatedAt: time.Now().Format(time.RFC3339),
		},
	})

	prompt := compactionUserPrompt
	if additionalPrompt != "" {
		prompt += "\n\nAdditional instructions: " + additionalPrompt
	}
	summarySession.AddMessage(&session.Message{
		Message: chat.Message{
			Role:      chat.MessageRoleUser,
			Content:   prompt,
			CreatedAt: time.Now().Format(time.RFC3339),
		},
	})

	summaryRuntime, err := New(newTeam, WithSessionCompaction(false))
	if err != nil {
		return summaryResult{}, fmt.Errorf("creating summary runtime: %w", err)
	}

	if _, err = summaryRuntime.Run(ctx, summarySession); err != nil {
		return summaryResult{}, fmt.Errorf("generating summary: %w", err)
	}

	return summaryResult{
		Summary: summarySession.GetLastAssistantMessageContent(),
		Cost:    summarySession.TotalCost(),
	}, nil
}

// estimateItemTokens returns an approximate token count for a session item.
// Uses the len/4 heuristic.
func estimateItemTokens(item session.Item) int {
	if item.Summary != "" {
		return len(item.Summary) / 4
	}
	if item.Message != nil {
		return estimateMessageTokens(item.Message)
	}
	if item.SubSession != nil {
		total := 0
		for _, sub := range item.SubSession.Messages {
			total += estimateItemTokens(sub)
		}
		return total
	}
	return 0
}

// estimateMessageTokens returns an approximate token count for a message.
func estimateMessageTokens(msg *session.Message) int {
	tokens := len(msg.Message.Content) / 4
	tokens += len(msg.Message.ReasoningContent) / 4
	for _, part := range msg.Message.MultiContent {
		tokens += len(part.Text) / 4
	}
	for _, tc := range msg.Message.ToolCalls {
		tokens += len(tc.Function.Arguments) / 4
		tokens += len(tc.Function.Name) / 4
	}
	return tokens
}

// estimateTotalTokens estimates the total tokens from startIdx to the end of the items slice.
func estimateTotalTokens(items []session.Item, startIdx int) int {
	total := 0
	for i := startIdx; i < len(items); i++ {
		total += estimateItemTokens(items[i])
	}
	return total
}

// isValidCutPoint returns true if the item at the given index is a valid place to cut.
// Valid cut points are user messages, assistant messages, and summaries.
// Tool result messages must stay with their tool call.
func isValidCutPoint(item session.Item) bool {
	if item.Summary != "" {
		return true
	}
	if item.Message == nil {
		return false
	}
	role := item.Message.Message.Role
	return role == chat.MessageRoleUser || role == chat.MessageRoleAssistant
}

// snapToValidCutPoint adjusts the cut index to land on a valid cut point.
// Searches forward from cutIdx to find the next valid position.
func snapToValidCutPoint(items []session.Item, startIdx, cutIdx int) int {
	for i := cutIdx; i < len(items); i++ {
		if isValidCutPoint(items[i]) {
			return i
		}
	}
	// If no valid cut point found forward, search backwards.
	for i := cutIdx - 1; i > startIdx; i-- {
		if isValidCutPoint(items[i]) {
			return i + 1
		}
	}
	return startIdx
}

// findTurnStart finds the index of the user message that starts the turn containing cutIdx.
// A turn begins with a user message and includes all subsequent messages until the next user message.
func findTurnStart(items []session.Item, startIdx, cutIdx int) int {
	for i := cutIdx - 1; i >= startIdx; i-- {
		if items[i].Message != nil && items[i].Message.Message.Role == chat.MessageRoleUser {
			return i
		}
	}
	return startIdx
}

// isAtTurnBoundary returns true if cutIdx is at the start of a turn (user message).
func isAtTurnBoundary(items []session.Item, startIdx, cutIdx int) bool {
	if cutIdx < startIdx || cutIdx >= len(items) {
		return true
	}
	return items[cutIdx].Message != nil && items[cutIdx].Message.Message.Role == chat.MessageRoleUser
}

// serializeItems converts session items to text for the summarization model.
func serializeItems(items []session.Item) string {
	var b strings.Builder
	for _, item := range items {
		if item.Summary != "" {
			b.WriteString("[Previous Summary]: ")
			b.WriteString(item.Summary)
			b.WriteString("\n")
			continue
		}
		if item.Message == nil {
			continue
		}
		serializeMessage(&b, item.Message)
	}
	return b.String()
}

// serializeMessage writes a single message in the serialization format.
func serializeMessage(b *strings.Builder, msg *session.Message) {
	m := msg.Message
	switch m.Role {
	case chat.MessageRoleUser:
		b.WriteString("[User]: ")
		b.WriteString(m.Content)
		b.WriteString("\n")
	case chat.MessageRoleAssistant:
		if m.ReasoningContent != "" {
			b.WriteString("[Assistant thinking]: ")
			b.WriteString(m.ReasoningContent)
			b.WriteString("\n")
		}
		if m.Content != "" {
			b.WriteString("[Assistant]: ")
			b.WriteString(m.Content)
			b.WriteString("\n")
		}
		if len(m.ToolCalls) > 0 {
			b.WriteString("[Assistant tool calls]: ")
			for i, tc := range m.ToolCalls {
				if i > 0 {
					b.WriteString("; ")
				}
				b.WriteString(tc.Function.Name)
				b.WriteString("(")
				b.WriteString(tc.Function.Arguments)
				b.WriteString(")")
			}
			b.WriteString("\n")
		}
	case chat.MessageRoleTool:
		b.WriteString("[Tool result]: ")
		content := m.Content
		if len(content) > 2000 {
			content = content[:2000] + "... [truncated]"
		}
		b.WriteString(content)
		b.WriteString("\n")
	}
}
