package session

import (
	"log/slog"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/chat"
)

// PromptContext is the fully derived prompt payload for a single model call.
// It keeps both the exact message list sent to the provider and the named
// sections that produced it so runtime code can inspect or mutate the prompt
// without writing directly into the durable transcript.
type PromptContext struct {
	InvariantSystemMessages []chat.Message
	ContextSystemMessages   []chat.Message
	SummaryMessages         []chat.Message
	TranscriptMessages      []chat.Message
	Messages                []chat.Message
	Metadata                PromptContextMetadata
}

// PromptContextMetadata records the builder decisions that shaped the final
// prompt for the turn.
type PromptContextMetadata struct {
	SummaryStartIndex    int
	MaxHistoryItems      int
	MaxOldToolCallTokens int
	SystemMessages       int
	ConversationMessages int
}

// PromptBuilder derives a PromptContext from a Session transcript and an agent
// configuration.
type PromptBuilder struct {
	session *Session
	agent   *agent.Agent
}

func NewPromptBuilder(s *Session, a *agent.Agent) *PromptBuilder {
	return &PromptBuilder{session: s, agent: a}
}

// BuildPromptContext returns the exact prompt context that would be sent to the
// model for the current session and agent.
func (s *Session) BuildPromptContext(a *agent.Agent) *PromptContext {
	return NewPromptBuilder(s, a).Build()
}

func (b *PromptBuilder) Build() *PromptContext {
	ctx := &PromptContext{}
	if b == nil || b.session == nil || b.agent == nil {
		return ctx
	}

	s := b.session
	a := b.agent

	slog.Debug("Building prompt context", "agent", a.Name(), "session_id", s.ID)

	ctx.InvariantSystemMessages = buildInvariantSystemMessages(a)
	markLastMessageAsCacheControl(ctx.InvariantSystemMessages)

	ctx.ContextSystemMessages = buildContextSpecificSystemMessages(a, s)
	markLastMessageAsCacheControl(ctx.ContextSystemMessages)

	items := s.snapshotItems()
	ctx.SummaryMessages, ctx.Metadata.SummaryStartIndex = buildSessionSummaryMessages(items)
	ctx.TranscriptMessages = buildTranscriptMessages(items, ctx.Metadata.SummaryStartIndex)

	messages := make([]chat.Message, 0,
		len(ctx.InvariantSystemMessages)+
			len(ctx.ContextSystemMessages)+
			len(ctx.SummaryMessages)+
			len(ctx.TranscriptMessages),
	)
	messages = append(messages, ctx.InvariantSystemMessages...)
	messages = append(messages, ctx.ContextSystemMessages...)
	messages = append(messages, ctx.SummaryMessages...)
	messages = append(messages, ctx.TranscriptMessages...)

	ctx.Metadata.MaxHistoryItems = a.NumHistoryItems()
	if ctx.Metadata.MaxHistoryItems > 0 {
		messages = trimMessages(messages, ctx.Metadata.MaxHistoryItems)
	}

	ctx.Metadata.MaxOldToolCallTokens = s.MaxOldToolCallTokens
	if ctx.Metadata.MaxOldToolCallTokens == 0 {
		ctx.Metadata.MaxOldToolCallTokens = DefaultMaxOldToolCallTokens
	}
	if ctx.Metadata.MaxOldToolCallTokens > 0 {
		messages = truncateOldToolContent(messages, ctx.Metadata.MaxOldToolCallTokens)
	}

	ctx.Messages = sanitizeToolCalls(messages)

	for i := range ctx.Messages {
		if ctx.Messages[i].Role == chat.MessageRoleSystem {
			ctx.Metadata.SystemMessages++
		} else {
			ctx.Metadata.ConversationMessages++
		}
	}

	slog.Debug("Built prompt context",
		"agent", a.Name(),
		"session_id", s.ID,
		"total_messages", len(ctx.Messages),
		"system_messages", ctx.Metadata.SystemMessages,
		"conversation_messages", ctx.Metadata.ConversationMessages,
		"max_history_items", ctx.Metadata.MaxHistoryItems,
		"max_old_tool_call_tokens", ctx.Metadata.MaxOldToolCallTokens,
	)

	return ctx
}

func (s *Session) snapshotItems() []Item {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]Item, len(s.Messages))
	for i, item := range s.Messages {
		if item.Message != nil {
			items[i] = Item{
				Message:    deepCopyMessage(item.Message),
				SubSession: item.SubSession,
				Summary:    item.Summary,
				Cost:       item.Cost,
			}
			continue
		}
		items[i] = item
	}
	return items
}

func buildTranscriptMessages(items []Item, startIndex int) []chat.Message {
	var messages []chat.Message
	for i := startIndex; i < len(items); i++ {
		item := items[i]
		if item.IsMessage() {
			messages = append(messages, item.Message.Message)
		}
	}
	return messages
}
