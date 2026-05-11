package cloudbridge

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/docker/docker-agent/pkg/runtime"
	"github.com/docker/docker-agent/pkg/session"
)

// EventObserver forwards runtime events to the Agentic Platform's per-session
// event bus so the AP web UI can render streaming updates in real time.
//
// Implements [runtime.EventObserver]. Each event is JSON-marshaled and
// enqueued onto the MirrorStore's background worker; PublishLocalEvent is
// sent best-effort and never blocks the runtime's event loop.
//
// Events that carry a different session ID (i.e. sub-session events emitted
// from a delegated task) are filtered out so the parent session's stream
// only contains its own events. This mirrors the filtering done by
// [runtime.PersistenceObserver].
type EventObserver struct {
	mirror *MirrorStore
}

// NewEventObserver returns an observer that forwards runtime events to AP
// via the supplied MirrorStore. Returns nil when ms is nil so callers can
// pass cloudbridge.NewEventObserver(cloudbridge.Wrap(...)) unconditionally
// and have it short-circuit when the bridge is disabled.
func NewEventObserver(ms *MirrorStore) *EventObserver {
	if ms == nil {
		return nil
	}
	return &EventObserver{mirror: ms}
}

// OnRunStart implements [runtime.EventObserver]; nothing to do at run start.
func (o *EventObserver) OnRunStart(_ context.Context, _ *session.Session) {}

// OnEvent implements [runtime.EventObserver]. Forwards events scoped to
// sess.ID to AP via the MirrorStore's background worker.
func (o *EventObserver) OnEvent(_ context.Context, sess *session.Session, event runtime.Event) {
	if o == nil || o.mirror == nil || sess == nil || sess.ID == "" {
		return
	}
	// Only publish events for sessions the user has explicitly opted in to
	// remote control. Idle observation has zero cost on the wire.
	if !o.mirror.IsActive(sess.ID) {
		return
	}
	// Filter out events that belong to a different session (e.g. sub-session
	// events from transfer_task). Same pattern as PersistenceObserver.
	if scoped, ok := event.(runtime.SessionScoped); ok && scoped.GetSessionID() != sess.ID {
		return
	}

	raw, err := json.Marshal(event)
	if err != nil {
		slog.Warn("cloudbridge: failed to marshal event for AP publish",
			"session_id", sess.ID, "error", err)
		return
	}
	o.mirror.PublishEvent(sess.ID, extractEventType(raw), raw)
}

// extractEventType pulls the "type" field out of a runtime event JSON blob.
// Returns "" when absent or unparseable; the value is only used for
// server-side logging so a miss is harmless.
func extractEventType(raw []byte) string {
	var probe struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &probe) != nil {
		return ""
	}
	return probe.Type
}
