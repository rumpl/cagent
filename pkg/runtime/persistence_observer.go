package runtime

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/session"
)

// PersistenceObserver is the stock [EventObserver] that mirrors the
// runtime's event stream to a [session.Store]:
//
//   - persists the initial session row on [OnRunStart];
//   - seeds forwarded sub-sessions (transfer_task and forked skills) with
//     their initial synthetic messages so later streaming deltas can update
//     the same child row while the sub-session is still running;
//   - tracks streaming assistant content (AgentChoice and
//     AgentChoiceReasoning) into a single growing message row, finalised
//     on [MessageAddedEvent];
//   - persists user messages, sub-session attachments, summaries, token
//     usage, and session-title updates as they fly past.
//
// [SessionScoped]-mismatch filtering lives inside [OnEvent] so forwarded
// child events cannot pollute the parent's transcript. Sub-session streams
// are persisted only when the runtime marks them PersistLive; background
// pinned sessions are intentionally left to their owner to avoid orphan rows.
//
// The runtime auto-registers one of these in [NewLocalRuntime] against
// the configured store. Custom sinks (telemetry, audit, A2A, ...) layer
// alongside via [WithEventObserver].
type PersistenceObserver struct {
	store session.Store

	mu        sync.Mutex
	streaming map[string]*streamingState
}

// streamingState holds the in-flight streaming assistant message
// across consecutive AgentChoice / AgentChoiceReasoning events. Reset
// to its zero value on every UserMessageEvent / MessageAddedEvent.
type streamingState struct {
	content          strings.Builder
	reasoningContent strings.Builder
	agentName        string
	messageID        int64 // ID of the in-flight row, 0 for none.
}

// newPersistenceObserver returns an observer that persists to store, or
// nil when store is nil so the constructor can call [WithEventObserver]
// unconditionally without a guard.
func newPersistenceObserver(store session.Store) *PersistenceObserver {
	if store == nil {
		return nil
	}
	return &PersistenceObserver{store: store, streaming: make(map[string]*streamingState)}
}

// OnRunStart persists the session row before the run loop starts. Forwarded
// sub-sessions are also seeded with their initial system/implicit messages;
// those messages are not emitted as UserMessageEvent values during the child
// stream, but they are part of the child transcript and must keep their leading
// positions before the live assistant row is created.
func (p *PersistenceObserver) OnRunStart(ctx context.Context, sess *session.Session) {
	p.resetStreaming(sess.ID)

	if sess.IsSubSession() {
		if !sess.PersistLive {
			return
		}
		p.persistSubSessionStart(ctx, sess)
		return
	}

	if err := p.store.UpdateSession(ctx, sess); err != nil {
		slog.WarnContext(ctx, "Failed to persist initial session", "session_id", sess.ID, "error", err)
	}
}

// OnEvent applies the per-event-type persistence rules. Events tagged with a
// different session id are ignored, which drops forwarded child-stream events
// from the parent observer while allowing the child observer to persist them
// against the child session itself.
func (p *PersistenceObserver) OnEvent(ctx context.Context, sess *session.Session, event Event) {
	if sess.IsSubSession() && !sess.PersistLive {
		return
	}
	if scoped, ok := event.(SessionScoped); ok && scoped.GetSessionID() != sess.ID {
		return
	}

	switch e := event.(type) {
	case *AgentChoiceEvent:
		p.mu.Lock()
		st := p.streamingForLocked(e.SessionID)
		st.content.WriteString(e.Content)
		st.agentName = e.AgentName
		p.persistStreamingContentLocked(ctx, e.SessionID, st)
		p.mu.Unlock()

	case *AgentChoiceReasoningEvent:
		p.mu.Lock()
		st := p.streamingForLocked(e.SessionID)
		st.reasoningContent.WriteString(e.Content)
		st.agentName = e.AgentName
		p.persistStreamingContentLocked(ctx, e.SessionID, st)
		p.mu.Unlock()

	case *UserMessageEvent:
		p.resetStreaming(e.SessionID)
		if _, err := p.store.AddMessage(ctx, e.SessionID, session.UserMessage(e.Message, e.MultiContent...)); err != nil {
			slog.WarnContext(ctx, "Failed to persist user message", "session_id", e.SessionID, "error", err)
		}

	case *MessageAddedEvent:
		// Finalise the streaming row (if any) with the canonical
		// MessageAddedEvent payload, then reset for the next stream.
		p.mu.Lock()
		st := p.streaming[e.SessionID]
		messageID := int64(0)
		if st != nil {
			messageID = st.messageID
		}
		var err error
		if messageID != 0 {
			err = p.store.UpdateMessage(ctx, messageID, e.Message)
		} else {
			_, err = p.store.AddMessage(ctx, e.SessionID, e.Message)
		}
		delete(p.streaming, e.SessionID)
		p.mu.Unlock()
		if err != nil {
			slog.WarnContext(ctx, "Failed to persist message",
				"session_id", e.SessionID, "message_id", messageID, "error", err)
		}

	case *SubSessionCompletedEvent:
		if subSess, ok := e.SubSession.(*session.Session); ok {
			if err := p.store.AddSubSession(ctx, e.ParentSessionID, subSess); err != nil {
				slog.WarnContext(ctx, "Failed to persist sub-session", "parent_id", e.ParentSessionID, "error", err)
			}
		}

	case *SessionSummaryEvent:
		if err := p.store.AddSummary(ctx, e.SessionID, e.Summary, e.FirstKeptEntry); err != nil {
			slog.WarnContext(ctx, "Failed to persist summary", "session_id", e.SessionID, "error", err)
		}

	case *TokenUsageEvent:
		if e.Usage != nil {
			if err := p.store.UpdateSessionTokens(ctx, e.SessionID, e.Usage.InputTokens, e.Usage.OutputTokens, e.Usage.Cost); err != nil {
				slog.WarnContext(ctx, "Failed to persist token usage", "session_id", e.SessionID, "error", err)
			}
		}

	case *SessionTitleEvent:
		if err := p.store.UpdateSessionTitle(ctx, e.SessionID, e.Title); err != nil {
			slog.WarnContext(ctx, "Failed to persist session title", "session_id", e.SessionID, "error", err)
		}
	}
}

// persistSubSessionStart creates the child session row and stores the initial
// messages that exist before streaming starts. It is idempotent for resumed
// rows: existing rows get metadata refreshed, and seeding is skipped once any
// items are present.
func (p *PersistenceObserver) persistSubSessionStart(ctx context.Context, sess *session.Session) {
	var existing *session.Session
	found := false
	if got, err := p.store.GetSession(ctx, sess.ID); err == nil {
		existing = got
		found = true
	} else if !errors.Is(err, session.ErrNotFound) {
		slog.WarnContext(ctx, "Failed to check persisted sub-session", "session_id", sess.ID, "error", err)
	}

	if err := p.store.UpdateSession(ctx, sess); err != nil {
		slog.WarnContext(ctx, "Failed to persist sub-session metadata", "session_id", sess.ID, "error", err)
		return
	}
	if found && existing != nil && len(existing.Messages) > 0 {
		return
	}

	p.seedSessionItems(ctx, sess)
}

func (p *PersistenceObserver) seedSessionItems(ctx context.Context, sess *session.Session) {
	snapshot := sess.Clone()
	if snapshot == nil {
		return
	}
	for _, item := range snapshot.Messages {
		switch {
		case item.Message != nil:
			if _, err := p.store.AddMessage(ctx, snapshot.ID, item.Message); err != nil {
				slog.WarnContext(ctx, "Failed to seed sub-session message", "session_id", snapshot.ID, "error", err)
			}
		case item.SubSession != nil:
			if err := p.store.AddSubSession(ctx, snapshot.ID, item.SubSession); err != nil {
				slog.WarnContext(ctx, "Failed to seed nested sub-session", "session_id", snapshot.ID, "sub_session_id", item.SubSession.ID, "error", err)
			}
		case item.Summary != "":
			if err := p.store.AddSummary(ctx, snapshot.ID, item.Summary, item.FirstKeptEntry); err != nil {
				slog.WarnContext(ctx, "Failed to seed sub-session summary", "session_id", snapshot.ID, "error", err)
			}
		}
	}
}

func (p *PersistenceObserver) resetStreaming(sessionID string) {
	p.mu.Lock()
	delete(p.streaming, sessionID)
	p.mu.Unlock()
}

func (p *PersistenceObserver) streamingForLocked(sessionID string) *streamingState {
	if p.streaming == nil {
		p.streaming = make(map[string]*streamingState)
	}
	st := p.streaming[sessionID]
	if st == nil {
		st = &streamingState{}
		p.streaming[sessionID] = st
	}
	return st
}

// persistStreamingContentLocked creates or updates the streaming assistant
// message row. The runtime emits one AgentChoice / AgentChoiceReasoning
// event per delta chunk, so this fires repeatedly during a streaming
// response; we keep one row open and update it in place rather than
// creating a row per chunk. p.mu must be held by the caller.
func (p *PersistenceObserver) persistStreamingContentLocked(ctx context.Context, sessionID string, st *streamingState) {
	msg := &session.Message{
		AgentName: st.agentName,
		Message: chat.Message{
			Role:             chat.MessageRoleAssistant,
			Content:          st.content.String(),
			ReasoningContent: st.reasoningContent.String(),
		},
	}

	if st.messageID == 0 {
		id, err := p.store.AddMessage(ctx, sessionID, msg)
		if err != nil {
			slog.WarnContext(ctx, "Failed to create streaming message", "session_id", sessionID, "error", err)
			return
		}
		st.messageID = id
		slog.DebugContext(ctx, "[PERSIST] Created streaming message",
			"session_id", sessionID, "message_id", id, "agent", st.agentName)
		return
	}

	if err := p.store.UpdateMessage(ctx, st.messageID, msg); err != nil {
		slog.WarnContext(ctx, "Failed to update streaming message",
			"session_id", sessionID, "message_id", st.messageID, "error", err)
	}
}
