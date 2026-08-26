package root

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/api"
	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/runtime"
	"github.com/docker/docker-agent/pkg/session"
)

// TestRemoteBackendSession covers how a run gets hold of the server-side
// session it drives: created fresh, or opened by ID — possibly one another
// process is using right now.
func TestRemoteBackendSession(t *testing.T) {
	t.Parallel()

	t.Run("attaches to an existing session from its snapshot", func(t *testing.T) {
		t.Parallel()

		created := time.Now().UTC().Truncate(time.Second)
		snapshot := api.SessionSnapshotResponse{
			ID:           "shared-1",
			Title:        "shared work",
			CreatedAt:    created,
			WorkingDir:   "/srv/work",
			InputTokens:  120,
			OutputTokens: 34,
			LastEventSeq: 17,
			Messages: []session.Message{
				{Message: chat.Message{Role: chat.MessageRoleUser, Content: "from the other client"}},
				{AgentName: "root", Message: chat.Message{Role: chat.MessageRoleAssistant, Content: "on it"}},
			},
		}

		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			_ = json.NewEncoder(w).Encode(snapshot)
		}))
		t.Cleanup(srv.Close)

		client, err := runtime.NewClient(srv.URL)
		require.NoError(t, err)

		b := &remoteBackend{flags: &runExecFlags{remoteAddress: srv.URL}, agentFileName: "agent.yaml"}
		sess, seq, err := b.session(t.Context(), client, runtime.CreateSessionRequest{ResumeSessionID: "shared-1"})
		require.NoError(t, err)

		assert.Equal(t, "/api/sessions/shared-1/snapshot", gotPath)
		assert.Equal(t, "shared-1", sess.ID)
		assert.Equal(t, "shared work", sess.TitleSnapshot())
		assert.Equal(t, "/srv/work", sess.WorkingDir)
		assert.Equal(t, created, sess.CreatedAt)
		assert.Len(t, sess.GetAllMessages(), 2, "the server's history is rebuilt locally so the TUI can render it")
		input, output := sess.Usage()
		assert.Equal(t, int64(120), input)
		assert.Equal(t, int64(34), output)
		assert.Equal(t, uint64(17), seq, "the shared stream is tailed from the snapshot's position")
	})

	t.Run("reports a session that does not exist server-side", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
		}))
		t.Cleanup(srv.Close)

		client, err := runtime.NewClient(srv.URL)
		require.NoError(t, err)

		b := &remoteBackend{flags: &runExecFlags{remoteAddress: srv.URL}, agentFileName: "agent.yaml"}
		_, _, err = b.session(t.Context(), client, runtime.CreateSessionRequest{ResumeSessionID: "nope"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), `opening remote session "nope"`)
	})

	t.Run("creates a new session without --session", func(t *testing.T) {
		t.Parallel()

		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			_ = json.NewEncoder(w).Encode(session.New(session.WithID("fresh-1")))
		}))
		t.Cleanup(srv.Close)

		client, err := runtime.NewClient(srv.URL)
		require.NoError(t, err)

		b := &remoteBackend{flags: &runExecFlags{remoteAddress: srv.URL}, agentFileName: "agent.yaml"}
		sess, seq, err := b.session(t.Context(), client, runtime.CreateSessionRequest{})
		require.NoError(t, err)
		assert.Equal(t, "/api/sessions", gotPath)
		assert.Equal(t, "fresh-1", sess.ID)
		assert.Zero(t, seq)
		assert.Empty(t, sess.GetAllMessages())
	})
}
