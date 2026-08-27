package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClient_StreamSessionEvents_DeliversMultipleEvents verifies that the
// SSE stream stays open across multiple events instead of being torn down
// when StreamSessionEvents returns. This is a regression test for a bug
// where a deferred cancel() on the streaming context killed the in-flight
// HTTP request as soon as the function returned, turning the stream into
// a one-shot read.
func TestClient_StreamSessionEvents_DeliversMultipleEvents(t *testing.T) {
	t.Parallel()

	// proceed gates each subsequent event on the client having consumed
	// the previous one, guaranteeing the events arrive in separate reads
	// (the one-shot-read regression) without timing dependence. Buffered
	// so a failing run leaves the reader loop unblocked instead of
	// deadlocking the test.
	proceed := make(chan struct{}, 3)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Errorf("ResponseWriter must support flushing")
			return
		}

		for i := 1; i <= 3; i++ {
			if i > 1 {
				if _, ok := <-proceed; !ok {
					return
				}
			}
			fmt.Fprintf(w, "data: {\"type\":\"session_title\",\"session_id\":\"s\",\"title\":\"t%d\"}\n\n", i)
			flusher.Flush()
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(proceed) })

	c, err := NewClient(srv.URL)
	require.NoError(t, err)

	ch, err := c.StreamSessionEvents(t.Context(), "s")
	require.NoError(t, err)

	var titles []string
	for ev := range ch {
		titleEv, ok := ev.(*SessionTitleEvent)
		if !ok {
			continue
		}
		titles = append(titles, titleEv.Title)
		if len(titles) < 3 {
			proceed <- struct{}{}
		}
	}

	assert.Equal(t, []string{"t1", "t2", "t3"}, titles)
}

// TestClient_StreamSessionEvents_StopsWhenContextCancelled verifies that
// cancelling the caller's context tears down the stream and closes the
// returned channel.
func TestClient_StreamSessionEvents_StopsWhenContextCancelled(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)

		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				fmt.Fprint(w, "data: {\"type\":\"session_title\",\"session_id\":\"s\",\"title\":\"x\"}\n\n")
				flusher.Flush()
			}
		}
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	ch, err := c.StreamSessionEvents(ctx, "s")
	require.NoError(t, err)

	// Drain at least one event to confirm the stream is live.
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("no events received before cancel")
	}

	cancel()

	// Channel must close in a bounded time after cancel.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("channel was not closed after context cancel")
		}
	}
}

// TestClient_StreamSessionEventsFrom_CarriesSequenceAndControlFrames covers
// the resumable shape of the shared session stream: each event carries the
// server's sequence number (so a reconnect can resume from it), the resume
// point is forwarded as ?since=, and the two control frames are surfaced as
// such rather than parsed as events.
func TestClient_StreamSessionEventsFrom_CarriesSequenceAndControlFrames(t *testing.T) {
	t.Parallel()

	gotSince := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSince <- r.URL.Query().Get("since")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: {\"type\":\"gap\"}\n\n")
		fmt.Fprint(w, "id: 8\ndata: {\"type\":\"session_title\",\"session_id\":\"s\",\"title\":\"t\"}\n\n")
		fmt.Fprint(w, "id: 9\ndata: {\"type\":\"session_exited\"}\n\n")
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL)
	require.NoError(t, err)

	frames, err := c.StreamSessionEventsFrom(t.Context(), "s", 7)
	require.NoError(t, err)

	var got []SessionStreamFrame
	for frame := range frames {
		got = append(got, frame)
	}

	require.Len(t, got, 3)
	assert.Equal(t, "7", <-gotSince)

	assert.Equal(t, SessionStreamGap, got[0].Control)
	assert.Zero(t, got[0].Seq, "a control frame outside the sequenced stream carries no sequence number")

	require.NotNil(t, got[1].Event)
	assert.Equal(t, uint64(8), got[1].Seq)
	titleEvent, ok := got[1].Event.(*SessionTitleEvent)
	require.True(t, ok, "expected a session title event, got %T", got[1].Event)
	assert.Equal(t, "t", titleEvent.Title)

	assert.Equal(t, SessionStreamExited, got[2].Control)
	assert.Nil(t, got[2].Event)
}

// TestClient_RunAgent_CarriesSharedStreamPositions verifies that a turn's own
// response reports where each of its events landed on the session's shared
// event stream. A client watching both streams needs those positions to tell
// its own turn from another client's.
func TestClient_RunAgent_CarriesSharedStreamPositions(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "id: 4\ndata: {\"type\":\"stream_started\",\"session_id\":\"s\",\"agent_name\":\"root\"}\n\n")
		fmt.Fprint(w, "id: 5\ndata: {\"type\":\"stream_stopped\",\"session_id\":\"s\",\"agent_name\":\"root\"}\n\n")
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL)
	require.NoError(t, err)

	frames, err := c.RunAgent(t.Context(), "s", "agent.yaml", nil, "")
	require.NoError(t, err)

	var seqs []uint64
	var types []string
	for frame := range frames {
		seqs = append(seqs, frame.Seq)
		types = append(types, fmt.Sprintf("%T", frame.Event))
	}

	assert.Equal(t, []uint64{4, 5}, seqs)
	assert.Equal(t, []string{"*runtime.StreamStartedEvent", "*runtime.StreamStoppedEvent"}, types)
}

// TestClient_RunAgent_ReportsBusySessionTypedError pins the typed error a
// client needs to react to another client already running a turn on the
// session (rather than string-matching the message).
func TestClient_RunAgent_ReportsBusySessionTypedError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		fmt.Fprint(w, `{"message":"session is already processing a request"}`)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL)
	require.NoError(t, err)

	_, err = c.RunAgent(t.Context(), "s", "agent.yaml", nil, "")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRemoteSessionBusy)
}

func TestClient_GetSessionSummaries(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/sessions", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{
			"id":"session-1",
			"title":"Remote session",
			"created_at":"2026-05-14T12:30:00Z",
			"starred":true,
			"num_messages":7,
			"cost":0.42,
			"working_dir":"/workspace",
			"attributes":{"source":"remote"}
		}]`)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL)
	require.NoError(t, err)

	summaries, err := NewRemoteSessionStore(c).GetSessionSummaries(t.Context())
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	assert.Equal(t, "session-1", summaries[0].ID)
	assert.Equal(t, "Remote session", summaries[0].Title)
	assert.Equal(t, time.Date(2026, time.May, 14, 12, 30, 0, 0, time.UTC), summaries[0].CreatedAt)
	assert.True(t, summaries[0].Starred)
	assert.Equal(t, 7, summaries[0].NumMessages)
	assert.InDelta(t, 0.42, summaries[0].Cost, 0)
	assert.Equal(t, "/workspace", summaries[0].WorkingDir)
	assert.Equal(t, map[string]string{"source": "remote"}, summaries[0].Attributes)
}
