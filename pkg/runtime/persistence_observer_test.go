package runtime

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/session"
)

func TestPersistenceObserver_PersistsLiveSubSessionStreaming(t *testing.T) {
	ctx := t.Context()
	store, err := session.NewSQLiteSessionStore(filepath.Join(t.TempDir(), "sessions.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	obs := newPersistenceObserver(store)
	parent := session.New(session.WithID("parent-session"))
	obs.OnRunStart(ctx, parent)

	child := session.New(
		session.WithID("child-session"),
		session.WithParentID(parent.ID),
		session.WithSystemMessage("child system prompt"),
		session.WithImplicitUserMessage("Please proceed."),
		session.WithSendUserMessage(false),
		session.WithTitle("Transferred task"),
	)
	child.PersistLive = true
	obs.OnRunStart(ctx, child)

	obs.OnEvent(ctx, child, AgentChoice("developer", child.ID, "hel"))
	storedChild, err := store.GetSession(ctx, child.ID)
	require.NoError(t, err)
	require.Len(t, storedChild.Messages, 3)
	assert.Equal(t, chat.MessageRoleSystem, storedChild.Messages[0].Message.Message.Role)
	assert.True(t, storedChild.Messages[1].Message.Implicit)
	assert.Equal(t, "hel", storedChild.Messages[2].Message.Message.Content)

	obs.OnEvent(ctx, child, AgentChoice("developer", child.ID, "lo"))
	storedChild, err = store.GetSession(ctx, child.ID)
	require.NoError(t, err)
	require.Len(t, storedChild.Messages, 3, "streaming deltas should update one row, not append rows")
	assert.Equal(t, "hello", storedChild.Messages[2].Message.Message.Content)

	final := session.NewAgentMessage("developer", &chat.Message{
		Role:    chat.MessageRoleAssistant,
		Content: "hello",
	})
	obs.OnEvent(ctx, child, MessageAdded(child.ID, final, "developer"))
	storedChild, err = store.GetSession(ctx, child.ID)
	require.NoError(t, err)
	require.Len(t, storedChild.Messages, 3)
	assert.Equal(t, "hello", storedChild.Messages[2].Message.Message.Content)

	obs.OnEvent(ctx, parent, SubSessionCompleted(parent.ID, child, "root"))
	storedParent, err := store.GetSession(ctx, parent.ID)
	require.NoError(t, err)
	require.Len(t, storedParent.Messages, 1)
	require.NotNil(t, storedParent.Messages[0].SubSession)
	assert.Equal(t, child.ID, storedParent.Messages[0].SubSession.ID)
	require.Len(t, storedParent.Messages[0].SubSession.Messages, 3)

	obs.OnEvent(ctx, parent, SubSessionCompleted(parent.ID, child, "root"))
	storedParent, err = store.GetSession(ctx, parent.ID)
	require.NoError(t, err)
	require.Len(t, storedParent.Messages, 1, "sub-session completion should be idempotent")
}
