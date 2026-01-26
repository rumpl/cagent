package runtime

import (
	"context"
	"log/slog"

	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/tools"
	mcptools "github.com/docker/cagent/pkg/tools/mcp"
)

// StorageRuntime wraps a Runtime and handles all session/message persistence.
// It implements the Runtime interface, intercepting events from RunStream
// and persisting changes to the session store transparently.
type StorageRuntime struct {
	runtime Runtime
	store   session.Store
}

func NewRuntime(agents *team.Team, store session.Store, opts ...Opt) (Runtime, error) {
	rt, err := New(agents, store, opts...)
	if err != nil {
		return nil, err
	}
	return &StorageRuntime{runtime: rt, store: store}, nil
}

// Unwrap returns the underlying runtime.
func (r *StorageRuntime) Unwrap() Runtime {
	return r.runtime
}

// CurrentAgentInfo delegates to the wrapped runtime.
func (r *StorageRuntime) CurrentAgentInfo(ctx context.Context) CurrentAgentInfo {
	return r.runtime.CurrentAgentInfo(ctx)
}

// CurrentAgentName delegates to the wrapped runtime.
func (r *StorageRuntime) CurrentAgentName() string {
	return r.runtime.CurrentAgentName()
}

// SetCurrentAgent delegates to the wrapped runtime.
func (r *StorageRuntime) SetCurrentAgent(agentName string) error {
	return r.runtime.SetCurrentAgent(agentName)
}

// CurrentAgentTools delegates to the wrapped runtime.
func (r *StorageRuntime) CurrentAgentTools(ctx context.Context) ([]tools.Tool, error) {
	return r.runtime.CurrentAgentTools(ctx)
}

// EmitStartupInfo delegates to the wrapped runtime.
func (r *StorageRuntime) EmitStartupInfo(ctx context.Context, events chan Event) {
	r.runtime.EmitStartupInfo(ctx, events)
}

// ResetStartupInfo delegates to the wrapped runtime.
func (r *StorageRuntime) ResetStartupInfo() {
	r.runtime.ResetStartupInfo()
}

// RunStream wraps the underlying runtime's RunStream and processes events
// for storage persistence.
func (r *StorageRuntime) RunStream(ctx context.Context, sess *session.Session) <-chan Event {
	events := r.runtime.RunStream(ctx, sess)

	// // Create storage listener state
	// listener := &storageListener{
	// 	store:         r.store,
	// 	session:       sess,
	// 	subSessions:   make(map[string]*session.Session),
	// 	activeSession: sess, // Start with main session as active
	// }

	out := make(chan Event, 128)
	go func() {
		defer close(out)
		for event := range events {
			// Process event for storage persistence
			// listener.processEvent(ctx, event)

			// Forward event to caller
			select {
			case out <- event:
			case <-ctx.Done():
				return
			}
		}
	}()

	return out
}

// Run delegates to the wrapped runtime.
func (r *StorageRuntime) Run(ctx context.Context, sess *session.Session) ([]session.Message, error) {
	return r.runtime.Run(ctx, sess)
}

// Resume delegates to the wrapped runtime.
func (r *StorageRuntime) Resume(ctx context.Context, req ResumeRequest) {
	r.runtime.Resume(ctx, req)
}

// ResumeElicitation delegates to the wrapped runtime.
func (r *StorageRuntime) ResumeElicitation(ctx context.Context, action tools.ElicitationAction, content map[string]any) error {
	return r.runtime.ResumeElicitation(ctx, action, content)
}

// SessionStore returns the storage runtime's session store.
func (r *StorageRuntime) SessionStore() session.Store {
	return r.store
}

// Summarize delegates to the wrapped runtime.
func (r *StorageRuntime) Summarize(ctx context.Context, sess *session.Session, additionalPrompt string, events chan Event) {
	r.runtime.Summarize(ctx, sess, additionalPrompt, events)
}

func (r *StorageRuntime) CurrentMCPPrompts(ctx context.Context) map[string]mcptools.PromptInfo {
	return r.runtime.CurrentMCPPrompts(ctx)
}

// storageListener maintains state for processing events and persisting to storage.
type storageListener struct {
	store         session.Store
	session       *session.Session
	subSessions   map[string]*session.Session // sessionID -> sub-session
	activeSession *session.Session            // currently active session (for events without session ID)
}

func (l *storageListener) processEvent(ctx context.Context, event Event) {
	// Skip persistence if the main session is a sub-session
	// (this happens when StorageRuntime is used recursively, which shouldn't occur)
	if l.session.IsSubSession() {
		return
	}

	switch e := event.(type) {
	case *StreamStartedEvent:
		l.handleStreamStarted(ctx, e)
	case *StreamStoppedEvent:
		l.handleStreamStopped(ctx, e)
	case *TokenUsageEvent:
		l.handleTokenUsage(ctx, e)
	case *ToolCallResponseEvent:
		l.handleToolCallResponse(ctx, e)
	case *SessionTitleEvent:
		l.handleSessionTitle(ctx, e)
	}
}

func (l *storageListener) handleStreamStarted(ctx context.Context, e *StreamStartedEvent) {
	// Check if this is a sub-session stream
	if subSess, ok := l.subSessions[e.SessionID]; ok {
		// Set sub-session as active for subsequent events
		l.activeSession = subSess
		// Sub-session already persisted in handleSubSessionCreated
		l.persistSessionMessages(ctx, subSess)
		return
	}

	// Main session - set as active
	l.activeSession = l.session

	// Ensure main session exists in the store
	if err := l.store.UpdateSession(ctx, l.session); err != nil {
		slog.Error("Failed to persist session", "error", err, "session_id", l.session.ID)
	}

	// Persist any messages that don't have IDs yet
	l.persistSessionMessages(ctx, l.session)
}

func (l *storageListener) handleStreamStopped(_ context.Context, e *StreamStoppedEvent) {
	// When a sub-session stops, switch back to the main session
	if _, ok := l.subSessions[e.SessionID]; ok {
		l.activeSession = l.session
	}
}

func (l *storageListener) handleTokenUsage(ctx context.Context, e *TokenUsageEvent) {
	// Check if this is a sub-session
	if subSess, ok := l.subSessions[e.SessionID]; ok {
		l.persistSessionMessages(ctx, subSess)
		return
	}

	// Main session - persist any new messages
	l.persistSessionMessages(ctx, l.session)
}

func (l *storageListener) handleToolCallResponse(ctx context.Context, _ *ToolCallResponseEvent) {
	// Tool response was added to session by runtime - persist it
	// Use activeSession which tracks the currently running session (main or sub)
	l.persistSessionMessages(ctx, l.activeSession)
}

func (l *storageListener) handleSessionTitle(ctx context.Context, e *SessionTitleEvent) {
	l.session.Title = e.Title
	if err := l.store.UpdateSession(ctx, l.session); err != nil {
		slog.Error("Failed to persist session title", "error", err, "session_id", l.session.ID)
	}
}

// persistSessionMessages finds messages in the given session without IDs and persists them
func (l *storageListener) persistSessionMessages(ctx context.Context, sess *session.Session) {
	for i := range sess.Messages {
		if sess.Messages[i].ID == "" {
			msgID, err := l.store.AddMessage(ctx, sess.ID, &sess.Messages[i])
			if err != nil {
				slog.Error("Failed to persist message", "error", err, "session_id", sess.ID, "index", i)
				continue
			}
			sess.Messages[i].ID = msgID
		}
	}
}
