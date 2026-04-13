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

// InsertContextSystemMessages appends context-specific system messages to the
// prompt context and inserts them into the final message list immediately after
// the invariant system section. This preserves the already-trimmed transcript
// projection while letting runtime middlewares add ephemeral prompt context.
func (ctx *PromptContext) InsertContextSystemMessages(messages ...chat.Message) {
	if ctx == nil || len(messages) == 0 {
		return
	}

	insertionPoint := len(ctx.InvariantSystemMessages) + len(ctx.ContextSystemMessages)
	if insertionPoint > len(ctx.Messages) {
		insertionPoint = len(ctx.Messages)
	}

	ctx.ContextSystemMessages = append(ctx.ContextSystemMessages, messages...)

	updated := make([]chat.Message, 0, len(ctx.Messages)+len(messages))
	updated = append(updated, ctx.Messages[:insertionPoint]...)
	updated = append(updated, messages...)
	updated = append(updated, ctx.Messages[insertionPoint:]...)
	ctx.Messages = sanitizeToolCalls(updated)
	ctx.recountMessageTypes()
}

func (ctx *PromptContext) recountMessageTypes() {
	if ctx == nil {
		return
	}
	ctx.Metadata.SystemMessages = 0
	ctx.Metadata.ConversationMessages = 0
	for i := range ctx.Messages {
		if ctx.Messages[i].Role == chat.MessageRoleSystem {
			ctx.Metadata.SystemMessages++
		} else {
			ctx.Metadata.ConversationMessages++
		}
	}
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

// BuildBasePromptContext returns the prompt context derived only from durable
// transcript state plus invariant agent configuration. Runtime-specific prompt
// injections (date, environment, prompt files, hook-provided context, etc.)
// are intentionally excluded so the runtime can add them as build-context
// middlewares.
func (s *Session) BuildBasePromptContext(a *agent.Agent) *PromptContext {
	return NewPromptBuilder(s, a).BuildBase()
}

func (b *PromptBuilder) Build() *PromptContext {
	ctx := b.BuildBase()
	if b == nil || b.session == nil || b.agent == nil {
		return ctx
	}

	contextMessages := BuildContextSpecificSystemMessages(b.agent, b.session)
	markLastMessageAsCacheControl(contextMessages)
	ctx.InsertContextSystemMessages(contextMessages...)

	slog.Debug("Built prompt context",
		"agent", b.agent.Name(),
		"session_id", b.session.ID,
		"total_messages", len(ctx.Messages),
		"system_messages", ctx.Metadata.SystemMessages,
		"conversation_messages", ctx.Metadata.ConversationMessages,
		"max_history_items", ctx.Metadata.MaxHistoryItems,
		"max_old_tool_call_tokens", ctx.Metadata.MaxOldToolCallTokens,
	)

	return ctx
}

func (b *PromptBuilder) BuildBase() *PromptContext {
	ctx := &PromptContext{}
	if b == nil || b.session == nil || b.agent == nil {
		return ctx
	}

	s := b.session
	a := b.agent

	slog.Debug("Building prompt context", "agent", a.Name(), "session_id", s.ID)

	ctx.InvariantSystemMessages = buildInvariantSystemMessages(a)
	markLastMessageAsCacheControl(ctx.InvariantSystemMessages)

	items := s.snapshotItems()
	ctx.SummaryMessages, ctx.Metadata.SummaryStartIndex = buildSessionSummaryMessages(items)
	ctx.TranscriptMessages = buildTranscriptMessages(items, ctx.Metadata.SummaryStartIndex)

	messages := make([]chat.Message, 0,
		len(ctx.InvariantSystemMessages)+
			len(ctx.SummaryMessages)+
			len(ctx.TranscriptMessages),
	)
	messages = append(messages, ctx.InvariantSystemMessages...)
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
	ctx.recountMessageTypes()
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
