package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/api"
	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/runtime"
	"github.com/docker/docker-agent/pkg/session"
)

// emittingRuntime is a fakeRuntime whose RunStream emits a fixed set of
// events before ending, so a turn's event flow can be observed.
type emittingRuntime struct {
	fakeRuntime

	events []runtime.Event
}

func (e *emittingRuntime) RunStream(context.Context, *session.Session) <-chan runtime.Event {
	ch := make(chan runtime.Event, len(e.events))
	for _, ev := range e.events {
		ch <- ev
	}
	close(ch)
	return ch
}

// collectEvents drains a session's event log for up to timeout, returning the
// events it saw. The stream is cancelled once want events have arrived.
func collectEvents(t *testing.T, sm *SessionManager, sessionID string, want int) []any {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	var got []any
	sm.StreamEvents(ctx, sessionID, nil, func(_ uint64, event any) {
		got = append(got, event)
		if len(got) >= want {
			cancel()
		}
	})
	return got
}

// TestRunSession_MirrorsTurnEventsToSessionLog is the server half of sharing a
// session between processes: the events of a turn one client started must
// reach every client tailing GET /api/sessions/:id/events, not only the HTTP
// caller that started it.
func TestRunSession_MirrorsTurnEventsToSessionLog(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sess := session.New()
	fake := &emittingRuntime{events: []runtime.Event{
		runtime.StreamStarted(sess.ID, "root"),
		runtime.StreamStopped(sess.ID, "root", ""),
	}}
	sm := newTestSessionManager(t, sess, fake)

	// Another client is watching the session, which is what gives it a log.
	require.True(t, sm.EnsureEventLog(sess.ID))

	stream, err := sm.RunSession(ctx, sess.ID, "agent.yaml", "", []api.Message{{Content: "hi"}}, "")
	require.NoError(t, err)
	for range stream {
	}

	got := collectEvents(t, sm, sess.ID, 2)
	require.Len(t, got, 2)
	assert.IsType(t, &runtime.StreamStartedEvent{}, got[0])
	assert.IsType(t, &runtime.StreamStoppedEvent{}, got[1])
}

// TestRunSession_DoesNotCreateEventLogForUnwatchedSession keeps the mirroring
// cheap: turn events are large (tool output), so a session no client is
// watching must not accumulate a buffer of them.
func TestRunSession_DoesNotCreateEventLogForUnwatchedSession(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sess := session.New()
	fake := &emittingRuntime{events: []runtime.Event{runtime.StreamStarted(sess.ID, "root")}}
	sm := newTestSessionManager(t, sess, fake)

	stream, err := sm.RunSession(ctx, sess.ID, "agent.yaml", "", []api.Message{{Content: "hi"}}, "")
	require.NoError(t, err)
	for range stream {
	}

	assert.False(t, sm.HasEventSource(sess.ID))
}

// TestRunSession_TagsTurnEventsWithSharedStreamPosition verifies that the
// caller's own stream reports where each of its events landed on the shared
// stream. That is what lets a client that also tails the session recognise
// its own turn instead of rendering it twice.
func TestRunSession_TagsTurnEventsWithSharedStreamPosition(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sess := session.New()
	fake := &emittingRuntime{events: []runtime.Event{
		runtime.StreamStarted(sess.ID, "root"),
		runtime.StreamStopped(sess.ID, "root", ""),
	}}
	sm := newTestSessionManager(t, sess, fake)
	require.True(t, sm.EnsureEventLog(sess.ID))

	stream, err := sm.RunSession(ctx, sess.ID, "agent.yaml", "", []api.Message{{Content: "hi"}}, "")
	require.NoError(t, err)
	var seqs []uint64
	for frame := range stream {
		seqs = append(seqs, frame.Seq)
	}

	assert.Equal(t, []uint64{1, 2}, seqs)
	last, ok := sm.LastEventSeq(sess.ID)
	require.True(t, ok)
	assert.Equal(t, uint64(2), last, "the caller's numbering is the shared stream's own")
}

// TestSessionEvents_SubscribesBeforeFirstEvent verifies that a client can
// attach to an existing session that has not emitted anything yet: the
// endpoint creates the log rather than 404ing, so no later event is missed.
func TestSessionEvents_SubscribesBeforeFirstEvent(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	store := session.NewInMemorySessionStore()
	sess := session.New()
	require.NoError(t, store.AddSession(ctx, sess))

	sm := NewSessionManager(ctx, config.Sources{}, store, 0, &config.RuntimeConfig{})
	srv := NewWithManager(sm, "")
	ln, err := Listen(ctx, "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(ctx, ln) }()

	addr := "http://" + ln.Addr().String()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr+"/api/sessions/"+sess.ID+"/events", http.NoBody)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.True(t, sm.HasEventSource(sess.ID))

	unknown, err := http.NewRequestWithContext(ctx, http.MethodGet, addr+"/api/sessions/does-not-exist/events", http.NoBody)
	require.NoError(t, err)
	unknownResp, err := http.DefaultClient.Do(unknown)
	require.NoError(t, err)
	defer unknownResp.Body.Close()
	assert.Equal(t, http.StatusNotFound, unknownResp.StatusCode)
}

// TestTwoRemoteRuntimesShareOneSession is the whole feature end to end: two
// RemoteRuntime clients — as two `docker agent run --remote` processes would
// be — drive the same server-side session. The one that did not start the
// turn observes it live, and its own next turn submits only its new message,
// not the history the server already holds.
func TestTwoRemoteRuntimesShareOneSession(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	sess := session.New()
	fake := &emittingRuntime{events: []runtime.Event{
		runtime.StreamStarted(sess.ID, "root"),
		runtime.AgentChoice("root", sess.ID, "hello from the agent"),
		runtime.StreamStopped(sess.ID, "root", ""),
	}}
	sm := newTestSessionManager(t, sess, fake)

	srv := NewWithManager(sm, "")
	ln, err := Listen(ctx, "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(ctx, ln) }()
	addr := "http://" + ln.Addr().String()

	// The first client drives the session it created.
	driverClient, err := runtime.NewClient(addr)
	require.NoError(t, err)
	driverSession := session.New(session.WithID(sess.ID))
	driver, err := runtime.NewRemoteRuntime(driverClient, runtime.WithRemoteSession(driverSession, 0))
	require.NoError(t, err)
	t.Cleanup(func() { _ = driver.Close() })

	// The second opens the same session by ID: snapshot first, then tail
	// from the position it was read at.
	observerClient, err := runtime.NewClient(addr)
	require.NoError(t, err)
	snapshot, err := observerClient.GetSessionSnapshot(ctx, sess.ID)
	require.NoError(t, err)
	observerSession := session.New(session.WithID(snapshot.ID))
	observer, err := runtime.NewRemoteRuntime(observerClient, runtime.WithRemoteSession(observerSession, snapshot.LastEventSeq))
	require.NoError(t, err)
	t.Cleanup(func() { _ = observer.Close() })

	seen := make(chan runtime.Event, 16)
	observer.OnBackgroundEvent(func(ev runtime.Event) { seen <- ev })

	driverSession.AddMessage(session.UserMessage("hi"))
	for range driver.RunStream(ctx, driverSession) {
	}

	var content string
	deadline := time.After(10 * time.Second)
	for content == "" {
		select {
		case ev := <-seen:
			if choice, ok := ev.(*runtime.AgentChoiceEvent); ok {
				content = choice.Content
			}
		case <-deadline:
			t.Fatal("timed out waiting for the other client's turn")
		}
	}
	assert.Equal(t, "hello from the agent", content)

	// The server now holds the turn; the observer's own next turn must add
	// only its new message to it.
	observerSession.AddMessage(session.UserMessage("and now mine"))
	for range observer.RunStream(ctx, observerSession) {
	}

	stored, err := sm.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	var prompts []string
	for _, msg := range stored.GetAllMessages() {
		if msg.Message.Role == chat.MessageRoleUser {
			prompts = append(prompts, msg.Message.Content)
		}
	}
	assert.Equal(t, []string{"hi", "and now mine"}, prompts, "neither client may replay the shared history")

	// A turn the driver starts after the observer ran its own must still
	// reach the observer: suppressing its own echo may not silence the
	// stream for good.
	driverSession.AddMessage(session.UserMessage("one more"))
	for range driver.RunStream(ctx, driverSession) {
	}

	content = ""
	deadline = time.After(10 * time.Second)
	for content == "" {
		select {
		case ev := <-seen:
			if choice, ok := ev.(*runtime.AgentChoiceEvent); ok {
				content = choice.Content
			}
		case <-deadline:
			t.Fatal("timed out waiting for the other client's later turn")
		}
	}
	assert.Equal(t, "hello from the agent", content)
}
