package runtime

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/session"
)

// streamingState tracks the accumulated content for a streaming assistant message.
type streamingState struct {
	content          strings.Builder
	reasoningContent strings.Builder
	agentName        string
	messageID        int64 // ID of the current streaming message (0 if none)
}

type persistenceObserver struct {
	store     session.Store
	mu        sync.Mutex
	streaming map[string]*streamingState
}

func newPersistenceObserver(store session.Store) *persistenceObserver {
	return &persistenceObserver{
		store:     store,
		streaming: make(map[string]*streamingState),
	}
}

func (p *persistenceObserver) runtimeObservers() RuntimeObservers {
	return RuntimeObservers{
		UserMessageAdded:    []UserMessageObserver{p.userMessageAdded},
		AssistantChunk:      []AssistantChunkObserver{p.assistantChunk},
		MessageAdded:        []MessageObserver{p.messageAdded},
		SessionSummaryAdded: []SessionSummaryObserver{p.sessionSummaryAdded},
		TokenUsageUpdated:   []TokenUsageObserver{p.tokenUsageUpdated},
		SubSessionCompleted: []SubSessionObserver{p.subSessionCompleted},
		SessionTitleUpdated: []SessionTitleObserver{p.sessionTitleUpdated},
	}
}

func shouldSkipPersistence(sess *session.Session) bool {
	return sess == nil || sess.IsSubSession()
}

func (p *persistenceObserver) streamingState(sessionID string) *streamingState {
	state := p.streaming[sessionID]
	if state == nil {
		state = &streamingState{}
		p.streaming[sessionID] = state
	}
	return state
}

func (p *persistenceObserver) resetStreaming(sessionID string) {
	state := p.streaming[sessionID]
	if state == nil {
		return
	}
	state.content.Reset()
	state.reasoningContent.Reset()
	state.agentName = ""
	state.messageID = 0
}

func (p *persistenceObserver) userMessageAdded(ctx context.Context, observed *ObservedUserMessage) error {
	if observed == nil || shouldSkipPersistence(observed.Session) || observed.Message == nil {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.resetStreaming(observed.Session.ID)
	_, err := p.store.AddMessage(ctx, observed.Session.ID, observed.Message)
	return err
}

func (p *persistenceObserver) assistantChunk(ctx context.Context, observed *ObservedAssistantChunk) error {
	if observed == nil || shouldSkipPersistence(observed.Session) || observed.Agent == nil {
		return nil
	}
	if observed.ContentDelta == "" && observed.ReasoningDelta == "" {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	state := p.streamingState(observed.Session.ID)
	state.agentName = observed.Agent.Name()
	state.content.WriteString(observed.ContentDelta)
	state.reasoningContent.WriteString(observed.ReasoningDelta)
	return p.persistStreamingContentLocked(ctx, observed.Session.ID, state)
}

func (p *persistenceObserver) messageAdded(ctx context.Context, observed *ObservedMessage) error {
	if observed == nil || shouldSkipPersistence(observed.Session) || observed.Message == nil {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	state := p.streamingState(observed.Session.ID)
	if observed.Message.Message.Role == chat.MessageRoleAssistant && state.messageID != 0 {
		if err := p.store.UpdateMessage(ctx, state.messageID, observed.Message); err != nil {
			return err
		}
		p.resetStreaming(observed.Session.ID)
		return nil
	}

	_, err := p.store.AddMessage(ctx, observed.Session.ID, observed.Message)
	if observed.Message.Message.Role == chat.MessageRoleAssistant {
		p.resetStreaming(observed.Session.ID)
	}
	return err
}

func (p *persistenceObserver) sessionSummaryAdded(ctx context.Context, observed *ObservedSessionSummary) error {
	if observed == nil || shouldSkipPersistence(observed.Session) {
		return nil
	}
	return p.store.AddSummary(ctx, observed.Session.ID, observed.Summary, observed.FirstKeptEntry)
}

func (p *persistenceObserver) tokenUsageUpdated(ctx context.Context, observed *ObservedTokenUsage) error {
	if observed == nil || shouldSkipPersistence(observed.Session) || observed.Usage == nil {
		return nil
	}
	return p.store.UpdateSessionTokens(ctx, observed.Session.ID, observed.Usage.InputTokens, observed.Usage.OutputTokens, observed.Usage.Cost)
}

func (p *persistenceObserver) subSessionCompleted(ctx context.Context, observed *ObservedSubSession) error {
	if observed == nil || observed.ParentSession == nil || observed.SubSession == nil {
		return nil
	}
	return p.store.AddSubSession(ctx, observed.ParentSession.ID, observed.SubSession)
}

func (p *persistenceObserver) sessionTitleUpdated(ctx context.Context, observed *ObservedSessionTitle) error {
	if observed == nil || observed.Session == nil {
		return nil
	}
	return p.store.UpdateSessionTitle(ctx, observed.Session.ID, observed.Title)
}

func (p *persistenceObserver) persistStreamingContentLocked(ctx context.Context, sessionID string, streaming *streamingState) error {
	msg := &session.Message{
		AgentName: streaming.agentName,
		Message: chat.Message{
			Role:             chat.MessageRoleAssistant,
			Content:          streaming.content.String(),
			ReasoningContent: streaming.reasoningContent.String(),
		},
	}

	if streaming.messageID == 0 {
		id, err := p.store.AddMessage(ctx, sessionID, msg)
		if err != nil {
			return err
		}
		streaming.messageID = id
		slog.Debug("[PERSIST] Created streaming message", "session_id", sessionID, "message_id", id, "agent", streaming.agentName)
		return nil
	}

	return p.store.UpdateMessage(ctx, streaming.messageID, msg)
}
