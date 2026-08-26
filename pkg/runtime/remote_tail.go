package runtime

import (
	"context"
	"log/slog"
	"time"
)

const (
	// sessionTailReconnectDelay paces reconnection to the shared event
	// stream. Nothing is lost by waiting: the stream resumes by sequence
	// number and the server buffers events.
	sessionTailReconnectDelay = time.Second

	// maxHeldFrames caps the frames held while a local turn is in flight (see
	// holdSessionFrameLocked). The oldest are evicted first: they are the
	// deepest inside our own turn, hence the likeliest to be its echo, which
	// the release filter would discard anyway.
	maxHeldFrames = 256
)

// startSessionTail mirrors the session's shared event stream into the
// background handler, which is what makes a session opened in two processes
// show the same thing in both: whichever client runs a turn, the others
// render it from this stream. It runs until [RemoteRuntime.Close].
//
// The session is fixed when the tail starts. A client that switches to a
// different session (e.g. /new) stops observing other clients' work, which
// matches the rest of RemoteRuntime: the server-side session it was built for
// is the one it drives.
func (r *RemoteRuntime) startSessionTail() {
	if r.sessionID == "" {
		// The runtime was built without a session, so there is nothing to
		// attach to yet; RunStream learns the ID later and only that client's
		// own events matter.
		return
	}
	r.tailOnce.Do(func() {
		sessionID := r.sessionID
		r.tailMu.Lock()
		defer r.tailMu.Unlock()
		if r.tailClosed {
			return
		}
		// The tail outlives every individual request, so it hangs off a
		// context of its own, cancelled by Close.
		ctx, cancel := context.WithCancel(context.Background())
		r.stopTail = cancel
		go r.tailSession(ctx, sessionID)
	})
}

// tailSession keeps one connection to the session's event stream alive,
// reconnecting from the last sequence number it saw. A single connection is
// bounded by the client's streaming timeout, so reconnecting is the normal
// case rather than an error path — and resuming by sequence number is what
// keeps it gapless.
func (r *RemoteRuntime) tailSession(ctx context.Context, sessionID string) {
	for first := true; ctx.Err() == nil; first = false {
		if !first {
			select {
			case <-time.After(sessionTailReconnectDelay):
			case <-ctx.Done():
				return
			}
		}
		frames, err := r.client.StreamSessionEventsFrom(ctx, sessionID, r.tailPoint())
		if err != nil {
			slog.DebugContext(ctx, "Cannot tail shared session events", "session_id", sessionID, "error", err)
			continue
		}
		if r.consumeSessionFrames(ctx, sessionID, frames) {
			return
		}
	}
}

// consumeSessionFrames forwards one connection's frames and reports whether
// the session ended for good. Any other end of stream is a drop (or the
// connection's max duration) and the caller reconnects.
func (r *RemoteRuntime) consumeSessionFrames(ctx context.Context, sessionID string, frames <-chan SessionStreamFrame) bool {
	for frame := range frames {
		switch frame.Control {
		case SessionStreamExited:
			return true
		case SessionStreamGap:
			slog.WarnContext(ctx, "Shared session stream gap: events from other clients were missed",
				"session_id", sessionID)
			continue
		}
		if handler, ok := r.observeSessionFrame(frame); ok {
			handler(frame.Event)
		}
	}
	return false
}

// observeSessionFrame advances the resume point and reports whether the frame
// is news to this client. Events echoing its own turn are skipped: RunStream
// already delivered them, and the UI would render them twice.
func (r *RemoteRuntime) observeSessionFrame(frame SessionStreamFrame) (func(Event), bool) {
	r.tailMu.Lock()
	defer r.tailMu.Unlock()
	if frame.Seq > r.lastSeq {
		r.lastSeq = frame.Seq
	}
	if frame.Event == nil || r.background == nil {
		return nil, false
	}
	if r.ownStreams > 0 {
		r.holdSessionFrameLocked(frame)
		return nil, false
	}
	if frame.Seq <= r.suppressUpTo {
		return nil, false
	}
	return r.background, true
}

// holdSessionFrameLocked parks a frame that arrived while a local turn was in
// flight. Whether it is that turn's own echo or another client's work is only
// decidable once the turn ends and its end position is known (see
// endOwnTurn), so the frame waits rather than being guessed at.
func (r *RemoteRuntime) holdSessionFrameLocked(frame SessionStreamFrame) {
	if len(r.heldFrames) >= maxHeldFrames {
		r.heldFrames = append(r.heldFrames[:0], r.heldFrames[len(r.heldFrames)-maxHeldFrames+1:]...)
	}
	r.heldFrames = append(r.heldFrames, frame)
}

// tailPoint is the sequence number to resume the shared stream from.
func (r *RemoteRuntime) tailPoint() uint64 {
	r.tailMu.Lock()
	defer r.tailMu.Unlock()
	return r.lastSeq
}

// beginOwnTurn marks the start of a turn this client drives. Its events reach
// the UI through RunStream, so the shared stream's copy of them is held back
// until the turn's echo has been accounted for (see endOwnTurn).
func (r *RemoteRuntime) beginOwnTurn() {
	r.tailMu.Lock()
	defer r.tailMu.Unlock()
	r.ownStreams++
}

// endOwnTurn ends that hold. ownTurnEnd is the stream position the turn's last
// event had on the shared stream, as reported by the turn's own response, so
// held frames up to it are this client's echo and everything past it is
// another client's work, to be rendered.
//
// dispatched is false when the server never started the turn (it was busy, or
// the request failed). There is then no echo to skip and nothing is held back:
// moving the boundary would hide the turn that is actually running.
func (r *RemoteRuntime) endOwnTurn(ownTurnEnd uint64, dispatched bool) {
	r.tailMu.Lock()
	if dispatched && ownTurnEnd == 0 {
		// The turn reported no position: the session had no event log when
		// it started (nothing was watching it yet), so its events are only
		// numbered from the moment the log appeared. Everything seen so far
		// is still this turn's, since the server runs one turn at a time.
		ownTurnEnd = r.lastSeq
	}
	if dispatched && ownTurnEnd > r.suppressUpTo {
		r.suppressUpTo = ownTurnEnd
	}
	r.ownStreams--
	handler, release := r.releaseHeldFramesLocked()
	r.tailMu.Unlock()

	for _, frame := range release {
		handler(frame.Event)
	}
}

// releaseHeldFramesLocked returns the held frames that turned out to be
// another client's work, and the handler to give them to. The caller must
// hold tailMu and must deliver them after releasing it.
func (r *RemoteRuntime) releaseHeldFramesLocked() (func(Event), []SessionStreamFrame) {
	if r.ownStreams > 0 || len(r.heldFrames) == 0 {
		return nil, nil
	}
	held := r.heldFrames
	r.heldFrames = nil
	if r.background == nil {
		return nil, nil
	}
	release := make([]SessionStreamFrame, 0, len(held))
	for _, frame := range held {
		if frame.Seq > r.suppressUpTo {
			release = append(release, frame)
		}
	}
	return r.background, release
}
