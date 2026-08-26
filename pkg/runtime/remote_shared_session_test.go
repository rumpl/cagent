package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/api"
	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/session"
)

// sharedSessionClient is a stubRemoteClient that records what each turn
// submits and lets the test drive the session's shared event stream, so both
// halves of "two clients, one session" can be exercised without a server.
type sharedSessionClient struct {
	stubRemoteClient

	mu        sync.Mutex
	submitted [][]api.Message
	runErr    error

	// frames is handed to the runtime's tail as the shared event stream.
	frames chan SessionStreamFrame
	// since records the resume point the tail asked for.
	since uint64
	// turnFrames is what a turn's own response stream returns, sequence
	// numbers included, exactly as the server sends them.
	turnFrames []SessionStreamFrame
	// turnGate, when non-nil, keeps a turn's response stream open until it
	// is closed, so a test can act while the turn is in flight.
	turnGate chan struct{}
}

func newSharedSessionClient() *sharedSessionClient {
	return &sharedSessionClient{frames: make(chan SessionStreamFrame, 16)}
}

func (c *sharedSessionClient) RunAgent(_ context.Context, _, _ string, msgs []api.Message, _ string) (<-chan SessionStreamFrame, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.runErr != nil {
		return nil, c.runErr
	}
	c.submitted = append(c.submitted, msgs)
	ch := make(chan SessionStreamFrame, len(c.turnFrames))
	for _, frame := range c.turnFrames {
		ch <- frame
	}
	gate := c.turnGate
	if gate == nil {
		close(ch)
		return ch, nil
	}
	go func() {
		<-gate
		close(ch)
	}()
	return ch, nil
}

func (c *sharedSessionClient) RunAgentWithAgentName(ctx context.Context, sessionID, agent, _ string, msgs []api.Message, model string) (<-chan SessionStreamFrame, error) {
	return c.RunAgent(ctx, sessionID, agent, msgs, model)
}

func (c *sharedSessionClient) StreamSessionEventsFrom(_ context.Context, _ string, since uint64) (<-chan SessionStreamFrame, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.since = since
	return c.frames, nil
}

func (c *sharedSessionClient) turns() [][]api.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.submitted
}

func (c *sharedSessionClient) resumePoint() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.since
}

func drain(t *testing.T, events <-chan Event) {
	t.Helper()
	for range events {
	}
}

func userContents(msgs []api.Message) []string {
	out := make([]string, 0, len(msgs))
	for _, msg := range msgs {
		out = append(out, msg.Content)
	}
	return out
}

// TestRemoteRuntime_SubmitsOnlyUnseenUserMessages pins the contract that makes
// a server-side session shareable: the server owns the transcript, so each
// turn hands over only the user messages it has not seen. Replaying the whole
// local history (the previous behaviour) duplicated it server-side and would
// clobber what another client contributed.
func TestRemoteRuntime_SubmitsOnlyUnseenUserMessages(t *testing.T) {
	t.Parallel()

	client := newSharedSessionClient()
	sess := session.New(session.WithID("s1"))
	rt, err := NewRemoteRuntime(client, WithRemoteSession(sess, 0))
	require.NoError(t, err)

	sess.AddMessage(session.UserMessage("first"))
	drain(t, rt.RunStream(t.Context(), sess))

	// The server appended the assistant's reply to its own transcript; the
	// local session only ever grows by what the user types.
	sess.AddMessage(session.NewAgentMessage("root", &chat.Message{Role: chat.MessageRoleAssistant, Content: "reply"}))
	sess.AddMessage(session.UserMessage("second"))
	drain(t, rt.RunStream(t.Context(), sess))

	turns := client.turns()
	require.Len(t, turns, 2)
	assert.Equal(t, []string{"first"}, userContents(turns[0]))
	assert.Equal(t, []string{"second"}, userContents(turns[1]), "a turn must not resend history the server already has")
}

// TestRemoteRuntime_AttachedSessionSubmitsOnlyLocalAdditions covers opening a
// session another client started: its history comes from the server's
// snapshot, so none of it may be submitted again.
func TestRemoteRuntime_AttachedSessionSubmitsOnlyLocalAdditions(t *testing.T) {
	t.Parallel()

	client := newSharedSessionClient()
	sess := session.New(session.WithID("s1"))
	sess.AddMessage(session.UserMessage("from the other client"))
	sess.AddMessage(session.NewAgentMessage("root", &chat.Message{Role: chat.MessageRoleAssistant, Content: "reply"}))

	rt, err := NewRemoteRuntime(client, WithRemoteSession(sess, 12))
	require.NoError(t, err)

	sess.AddMessage(session.UserMessage("mine"))
	drain(t, rt.RunStream(t.Context(), sess))

	turns := client.turns()
	require.Len(t, turns, 1)
	assert.Equal(t, []string{"mine"}, userContents(turns[0]))
}

// TestRemoteRuntime_ResubmitsAfterDispatchFailure verifies that user input the
// server never received is offered again, instead of being marked submitted
// and silently lost.
func TestRemoteRuntime_ResubmitsAfterDispatchFailure(t *testing.T) {
	t.Parallel()

	client := newSharedSessionClient()
	client.runErr = errors.New("dial tcp: connection refused")
	sess := session.New(session.WithID("s1"))
	rt, err := NewRemoteRuntime(client, WithRemoteSession(sess, 0))
	require.NoError(t, err)

	sess.AddMessage(session.UserMessage("first"))
	drain(t, rt.RunStream(t.Context(), sess))
	require.Empty(t, client.turns())

	client.mu.Lock()
	client.runErr = nil
	client.mu.Unlock()
	drain(t, rt.RunStream(t.Context(), sess))

	turns := client.turns()
	require.Len(t, turns, 1)
	assert.Equal(t, []string{"first"}, userContents(turns[0]))
}

// TestRemoteRuntime_TailsSharedSessionStream is the observer half: a turn run
// by another client attached to the same session reaches this client as a
// background event, so both processes show the same thing.
func TestRemoteRuntime_TailsSharedSessionStream(t *testing.T) {
	t.Parallel()

	client := newSharedSessionClient()
	sess := session.New(session.WithID("s1"))
	rt, err := NewRemoteRuntime(client, WithRemoteSession(sess, 7))
	require.NoError(t, err)
	t.Cleanup(func() { _ = rt.Close() })

	observed := make(chan Event, 4)
	rt.OnBackgroundEvent(func(ev Event) { observed <- ev })

	// Replayed history (at or below the snapshot's position) is already on
	// screen and must not be rendered twice.
	client.frames <- SessionStreamFrame{Seq: 7, Event: UserMessage("old", "s1", nil)}
	client.frames <- SessionStreamFrame{Seq: 8, Event: UserMessage("from the other client", "s1", nil)}

	select {
	case ev := <-observed:
		userEvent, ok := ev.(*UserMessageEvent)
		require.True(t, ok, "expected a user message event, got %T", ev)
		assert.Equal(t, "from the other client", userEvent.Message)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the peer's event")
	}

	assert.Equal(t, uint64(7), client.resumePoint(), "the tail must resume from the snapshot's position")
}

// TestRemoteRuntime_SuppressesOwnTurnEcho verifies that this client's own turn
// is not rendered twice: its events arrive both on its RunStream response and
// on the shared stream every client sees.
func TestRemoteRuntime_SuppressesOwnTurnEcho(t *testing.T) {
	t.Parallel()

	client := newSharedSessionClient()
	// The turn's own response reports where its events landed on the shared
	// stream, so the client knows its echo ends at seq 3.
	client.turnFrames = []SessionStreamFrame{
		{Seq: 1, Event: UserMessage("mine", "s1", nil)},
		{Seq: 3, Event: StreamStopped("s1", "root", "")},
	}
	sess := session.New(session.WithID("s1"))
	rt, err := NewRemoteRuntime(client, WithRemoteSession(sess, 0))
	require.NoError(t, err)
	t.Cleanup(func() { _ = rt.Close() })

	observed := make(chan Event, 4)
	rt.OnBackgroundEvent(func(ev Event) { observed <- ev })

	sess.AddMessage(session.UserMessage("mine"))
	drain(t, rt.RunStream(t.Context(), sess))

	// The shared stream carries the same events again — this is the echo.
	client.frames <- SessionStreamFrame{Seq: 1, Event: UserMessage("mine", "s1", nil)}
	client.frames <- SessionStreamFrame{Seq: 3, Event: StreamStopped("s1", "root", "")}
	// What comes after it is another client's work.
	client.frames <- SessionStreamFrame{Seq: 4, Event: UserMessage("theirs", "s1", nil)}

	select {
	case ev := <-observed:
		userEvent, ok := ev.(*UserMessageEvent)
		require.True(t, ok, "expected a user message event, got %T", ev)
		assert.Equal(t, "theirs", userEvent.Message, "own-turn events must not be rendered twice")
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the peer's event")
	}
}

// TestRemoteRuntime_TailReconnectsFromLastSeenSequence verifies the gapless
// reconnect: a dropped connection (or one that hit its max duration) resumes
// from the last sequence number observed, not from the buffer's start.
func TestRemoteRuntime_TailReconnectsFromLastSeenSequence(t *testing.T) {
	t.Parallel()

	client := newSharedSessionClient()
	sess := session.New(session.WithID("s1"))
	rt, err := NewRemoteRuntime(client, WithRemoteSession(sess, 0))
	require.NoError(t, err)
	t.Cleanup(func() { _ = rt.Close() })

	observed := make(chan Event, 4)
	rt.OnBackgroundEvent(func(ev Event) { observed <- ev })

	client.frames <- SessionStreamFrame{Seq: 9, Event: UserMessage("theirs", "s1", nil)}
	select {
	case <-observed:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the peer's event")
	}

	// Drop the connection: the tail must reconnect asking for everything
	// after seq 9.
	next := make(chan SessionStreamFrame, 4)
	client.mu.Lock()
	previous := client.frames
	client.frames = next
	client.mu.Unlock()
	close(previous)

	require.Eventually(t, func() bool { return client.resumePoint() == 9 }, 5*time.Second, 10*time.Millisecond)
}

// busySessionClient refuses the first turn the way a server does when another
// client is already running one, and records the follow-up it receives.
type busySessionClient struct {
	sharedSessionClient

	followUps [][]api.Message
}

func (c *busySessionClient) RunAgent(context.Context, string, string, []api.Message, string) (<-chan SessionStreamFrame, error) {
	return nil, fmt.Errorf("%w: session is already processing a request", ErrRemoteSessionBusy)
}

func (c *busySessionClient) RunAgentWithAgentName(ctx context.Context, sessionID, agent, _ string, msgs []api.Message, model string) (<-chan SessionStreamFrame, error) {
	return c.RunAgent(ctx, sessionID, agent, msgs, model)
}

func (c *busySessionClient) FollowUpSession(_ context.Context, _ string, msgs []api.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.followUps = append(c.followUps, msgs)
	return nil
}

// TestRemoteRuntime_QueuesInputWhileAnotherClientHoldsTheTurn verifies that
// typing while the other client's turn runs defers the message to the
// server's follow-up queue — the session is shared, so being busy is normal
// and must not be an error.
func TestRemoteRuntime_QueuesInputWhileAnotherClientHoldsTheTurn(t *testing.T) {
	t.Parallel()

	client := &busySessionClient{sharedSessionClient: *newSharedSessionClient()}
	sess := session.New(session.WithID("s1"))
	rt, err := NewRemoteRuntime(client, WithRemoteSession(sess, 0))
	require.NoError(t, err)

	sess.AddMessage(session.UserMessage("mine"))
	var got []Event
	for ev := range rt.RunStream(t.Context(), sess) {
		got = append(got, ev)
	}

	require.Len(t, got, 1)
	warning, ok := got[0].(*WarningEvent)
	require.True(t, ok, "expected a warning, got %T", got[0])
	assert.Contains(t, warning.Message, "will be picked up")

	client.mu.Lock()
	defer client.mu.Unlock()
	require.Len(t, client.followUps, 1)
	assert.Equal(t, []string{"mine"}, userContents(client.followUps[0]))
}

// TestRemoteRuntime_SuppressesOwnTurnEchoWithoutReportedPositions covers a
// turn that started before anything was watching the session: its response
// carries no stream positions, so the frames the shared stream produced while
// it ran are still its own echo and must not be rendered again.
func TestRemoteRuntime_SuppressesOwnTurnEchoWithoutReportedPositions(t *testing.T) {
	t.Parallel()

	client := newSharedSessionClient()
	// A turn on a session with no event log gets no positions back.
	client.turnFrames = []SessionStreamFrame{{Event: StreamStopped("s1", "root", "")}}
	client.turnGate = make(chan struct{})
	sess := session.New(session.WithID("s1"))
	rt, err := NewRemoteRuntime(client, WithRemoteSession(sess, 0))
	require.NoError(t, err)
	t.Cleanup(func() { _ = rt.Close() })

	observed := make(chan Event, 4)
	rt.OnBackgroundEvent(func(ev Event) { observed <- ev })

	sess.AddMessage(session.UserMessage("mine"))
	turnDone := make(chan struct{})
	go func() {
		defer close(turnDone)
		for range rt.RunStream(t.Context(), sess) {
		}
	}()
	require.Eventually(t, func() bool { return len(client.turns()) == 1 }, 5*time.Second, 10*time.Millisecond)

	// The log appeared mid-turn, so the shared stream numbers the turn's
	// tail from 1. It arrives while the turn is still in flight.
	client.frames <- SessionStreamFrame{Seq: 1, Event: UserMessage("mine", "s1", nil)}
	require.Eventually(t, func() bool { return rt.tailPoint() == 1 }, 5*time.Second, 10*time.Millisecond)

	close(client.turnGate)
	<-turnDone

	client.frames <- SessionStreamFrame{Seq: 2, Event: UserMessage("theirs", "s1", nil)}

	select {
	case ev := <-observed:
		userEvent, ok := ev.(*UserMessageEvent)
		require.True(t, ok, "expected a user message event, got %T", ev)
		assert.Equal(t, "theirs", userEvent.Message)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the peer's event")
	}
}
