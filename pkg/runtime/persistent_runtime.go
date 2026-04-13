package runtime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/team"
)

// PersistentRuntime wraps a LocalRuntime and persists session changes through
// typed runtime observers instead of inferring them from presentation events.
type PersistentRuntime struct {
	*LocalRuntime
}

// New creates a new runtime for an agent and its team.
// The runtime automatically persists session changes to the configured store.
// Returns a Runtime interface which wraps LocalRuntime with persistence handling.
func New(agents *team.Team, opts ...Opt) (Runtime, error) {
	r, err := NewLocalRuntime(agents, opts...)
	if err != nil {
		return nil, err
	}

	mergeRuntimeObservers(&r.observers, newPersistenceObserver(r.sessionStore).runtimeObservers())

	return &PersistentRuntime{LocalRuntime: r}, nil
}

// RunStream persists the initial session metadata and then delegates to the
// underlying LocalRuntime. Subsequent transcript/token updates are handled by
// typed observers installed during construction.
func (r *PersistentRuntime) RunStream(ctx context.Context, sess *session.Session) <-chan Event {
	if !sess.IsSubSession() {
		if err := r.sessionStore.UpdateSession(ctx, sess); err != nil {
			slog.Warn("Failed to persist initial session", "session_id", sess.ID, "error", err)
		}
	}
	return r.LocalRuntime.RunStream(ctx, sess)
}

// Run wraps RunStream and returns the final session messages.
func (r *PersistentRuntime) Run(ctx context.Context, sess *session.Session) ([]session.Message, error) {
	eventsChan := r.RunStream(ctx, sess)

	for event := range eventsChan {
		if errEvent, ok := event.(*ErrorEvent); ok {
			return nil, fmt.Errorf("%s", errEvent.Error)
		}
	}

	return sess.GetAllMessages(), nil
}
