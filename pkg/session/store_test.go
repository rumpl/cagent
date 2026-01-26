package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/chat"
)

func TestStoreAgentName(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_store.db")

	store, err := NewSQLiteSessionStore(tempDB)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	testAgent1 := agent.New("test-agent-1", "test prompt 1")
	testAgent2 := agent.New("test-agent-2", "test prompt 2")

	session := &Session{
		ID:           "test-session",
		InputTokens:  100,
		OutputTokens: 200,
		CreatedAt:    time.Now(),
	}

	// Store the session
	err = store.AddSession(t.Context(), session)
	require.NoError(t, err)

	// Add messages using AddItem
	item1 := NewMessageItem(UserMessage("Hello"))
	_, err = store.AddItem(t.Context(), session.ID, &item1)
	require.NoError(t, err)

	item2 := NewMessageItem(NewAgentMessage(testAgent1, &chat.Message{
		Role:    chat.MessageRoleAssistant,
		Content: "Hello from test-agent-1",
	}))
	_, err = store.AddItem(t.Context(), session.ID, &item2)
	require.NoError(t, err)

	item3 := NewMessageItem(NewAgentMessage(testAgent2, &chat.Message{
		Role:    chat.MessageRoleUser,
		Content: "Another message from test-agent-2",
	}))
	_, err = store.AddItem(t.Context(), session.ID, &item3)
	require.NoError(t, err)

	// Retrieve the session
	retrievedSession, err := store.GetSession(t.Context(), "test-session")
	require.NoError(t, err)
	require.NotNil(t, retrievedSession)

	assert.Len(t, retrievedSession.GetAllMessages(), 3)

	// First message should be user message with empty agent name
	assert.Empty(t, retrievedSession.Messages[0].Message.AgentName)
	assert.Equal(t, "Hello", retrievedSession.Messages[0].Message.Message.Content)

	// Second message should have the first agent's name
	assert.Equal(t, "test-agent-1", retrievedSession.Messages[1].Message.AgentName)
	assert.Equal(t, "Hello from test-agent-1", retrievedSession.Messages[1].Message.Message.Content)

	// Third message should have the second agent's name
	assert.Equal(t, "test-agent-2", retrievedSession.Messages[2].Message.AgentName)
	assert.Equal(t, "Another message from test-agent-2", retrievedSession.Messages[2].Message.Message.Content)
}

func TestStoreMultipleAgents(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_store_multi.db")

	store, err := NewSQLiteSessionStore(tempDB)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	agent1 := agent.New("agent-1", "agent 1 prompt")
	agent2 := agent.New("agent-2", "agent 2 prompt")

	session := &Session{
		ID:        "multi-agent-session",
		CreatedAt: time.Now(),
	}

	// Store the session
	err = store.AddSession(t.Context(), session)
	require.NoError(t, err)

	// Add messages using AddItem
	item1 := NewMessageItem(UserMessage("Start conversation"))
	_, err = store.AddItem(t.Context(), session.ID, &item1)
	require.NoError(t, err)

	item2 := NewMessageItem(NewAgentMessage(agent1, &chat.Message{
		Role:    chat.MessageRoleAssistant,
		Content: "Response from agent 1",
	}))
	_, err = store.AddItem(t.Context(), session.ID, &item2)
	require.NoError(t, err)

	item3 := NewMessageItem(NewAgentMessage(agent2, &chat.Message{
		Role:    chat.MessageRoleAssistant,
		Content: "Response from agent 2",
	}))
	_, err = store.AddItem(t.Context(), session.ID, &item3)
	require.NoError(t, err)

	// Retrieve the session
	retrievedSession, err := store.GetSession(t.Context(), "multi-agent-session")
	require.NoError(t, err)
	require.NotNil(t, retrievedSession)

	assert.Len(t, retrievedSession.Messages, 3)

	// First message should be user message with empty agent name
	assert.Empty(t, retrievedSession.Messages[0].Message.AgentName)

	// Second message should have agent-1 name
	assert.Equal(t, "agent-1", retrievedSession.Messages[1].Message.AgentName)
	assert.Equal(t, "Response from agent 1", retrievedSession.Messages[1].Message.Message.Content)

	// Third message should have agent-2 name
	assert.Equal(t, "agent-2", retrievedSession.Messages[2].Message.AgentName)
	assert.Equal(t, "Response from agent 2", retrievedSession.Messages[2].Message.Message.Content)
}

func TestGetSessions(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_get_sessions.db")

	store, err := NewSQLiteSessionStore(tempDB)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	testAgent := agent.New("test-agent", "test prompt")

	session1 := &Session{
		ID:        "session-1",
		CreatedAt: time.Now().Add(-1 * time.Hour),
	}

	session2 := &Session{
		ID:        "session-2",
		CreatedAt: time.Now(),
	}

	// Store the sessions
	err = store.AddSession(t.Context(), session1)
	require.NoError(t, err)
	err = store.AddSession(t.Context(), session2)
	require.NoError(t, err)

	// Add messages using AddItem
	item1 := NewMessageItem(NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleAssistant,
		Content: "Message from session 1",
	}))
	_, err = store.AddItem(t.Context(), session1.ID, &item1)
	require.NoError(t, err)

	item2 := NewMessageItem(NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleAssistant,
		Content: "Message from session 2",
	}))
	_, err = store.AddItem(t.Context(), session2.ID, &item2)
	require.NoError(t, err)

	// Retrieve all sessions
	sessions, err := store.GetSessions(t.Context())
	require.NoError(t, err)
	assert.Len(t, sessions, 2)

	for _, session := range sessions {
		assert.Len(t, session.Messages, 1)
		assert.Equal(t, "test-agent", session.Messages[0].Message.AgentName)
	}
}

func TestGetSessionSummaries(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_get_session_summaries.db")

	store, err := NewSQLiteSessionStore(tempDB)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	testAgent := agent.New("test-agent", "test prompt")

	session1Time := time.Now().UTC().Add(-1 * time.Hour).Truncate(time.Second)
	session2Time := time.Now().UTC().Truncate(time.Second)

	session1 := &Session{
		ID:    "session-1",
		Title: "First Session",
		Messages: []Item{
			NewMessageItem(NewAgentMessage(testAgent, &chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: "A very long message that should not be loaded when getting summaries",
			})),
		},
		CreatedAt: session1Time,
	}

	session2 := &Session{
		ID:    "session-2",
		Title: "Second Session",
		Messages: []Item{
			NewMessageItem(NewAgentMessage(testAgent, &chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: "Another long message that should not be loaded when getting summaries",
			})),
		},
		CreatedAt: session2Time,
	}

	// Store the sessions
	err = store.AddSession(t.Context(), session1)
	require.NoError(t, err)
	err = store.AddSession(t.Context(), session2)
	require.NoError(t, err)

	// Retrieve summaries (should be lightweight, without messages)
	summaries, err := store.GetSessionSummaries(t.Context())
	require.NoError(t, err)
	assert.Len(t, summaries, 2)

	// Summaries should be ordered by created_at DESC (most recent first)
	assert.Equal(t, "session-2", summaries[0].ID)
	assert.Equal(t, "Second Session", summaries[0].Title)
	assert.Equal(t, session2Time, summaries[0].CreatedAt)

	assert.Equal(t, "session-1", summaries[1].ID)
	assert.Equal(t, "First Session", summaries[1].Title)
	assert.Equal(t, session1Time, summaries[1].CreatedAt)
}

func TestStoreAgentNameJSON(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_store_json.db")

	store, err := NewSQLiteSessionStore(tempDB)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	agent1 := agent.New("my-agent", "test prompt")
	agent2 := agent.New("another-agent", "another prompt")

	session := &Session{
		ID:        "json-test-session",
		CreatedAt: time.Now(),
	}

	// Store the session
	err = store.AddSession(t.Context(), session)
	require.NoError(t, err)

	// Add messages using AddItem
	item1 := NewMessageItem(UserMessage("User input"))
	_, err = store.AddItem(t.Context(), session.ID, &item1)
	require.NoError(t, err)

	item2 := NewMessageItem(NewAgentMessage(agent1, &chat.Message{
		Role:    chat.MessageRoleAssistant,
		Content: "Response from my-agent",
	}))
	_, err = store.AddItem(t.Context(), session.ID, &item2)
	require.NoError(t, err)

	item3 := NewMessageItem(NewAgentMessage(agent2, &chat.Message{
		Role:    chat.MessageRoleAssistant,
		Content: "Response from another-agent",
	}))
	_, err = store.AddItem(t.Context(), session.ID, &item3)
	require.NoError(t, err)

	// Retrieve the session
	retrievedSession, err := store.GetSession(t.Context(), "json-test-session")
	require.NoError(t, err)
	require.NotNil(t, retrievedSession)

	assert.Empty(t, retrievedSession.Messages[0].Message.AgentName)                  // User message
	assert.Equal(t, "my-agent", retrievedSession.Messages[1].Message.AgentName)      // First agent
	assert.Equal(t, "another-agent", retrievedSession.Messages[2].Message.AgentName) // Second agent
}

func TestNewSQLiteSessionStore_DirectoryNotWritable(t *testing.T) {
	readOnlyDir := filepath.Join(t.TempDir(), "readonly")
	err := os.Mkdir(readOnlyDir, 0o555)
	require.NoError(t, err)

	_, err = NewSQLiteSessionStore(filepath.Join(readOnlyDir, "session.db"))
	require.Error(t, err)

	assert.Contains(t, err.Error(), "cannot create database")
	assert.Contains(t, err.Error(), "permission denied or file cannot be created")
}

func TestUpdateSession_LazyCreation(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_lazy.db")

	store, err := NewSQLiteSessionStore(tempDB)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	testAgent := agent.New("test-agent", "test prompt")

	// Create a session but don't add it to the store (simulating lazy creation)
	session := &Session{
		ID:        "lazy-session",
		CreatedAt: time.Now(),
	}

	// Verify session doesn't exist yet
	_, err = store.GetSession(t.Context(), "lazy-session")
	require.ErrorIs(t, err, ErrNotFound)

	// UpdateSession should create the session (upsert behavior)
	err = store.UpdateSession(t.Context(), session)
	require.NoError(t, err)

	// Now add messages via AddItem
	item1 := NewMessageItem(UserMessage("Hello"))
	_, err = store.AddItem(t.Context(), session.ID, &item1)
	require.NoError(t, err)

	item2 := NewMessageItem(NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleAssistant,
		Content: "Hi there!",
	}))
	_, err = store.AddItem(t.Context(), session.ID, &item2)
	require.NoError(t, err)

	// Now the session should exist with messages
	retrieved, err := store.GetSession(t.Context(), "lazy-session")
	require.NoError(t, err)
	assert.Len(t, retrieved.Messages, 2)
	assert.Equal(t, "Hello", retrieved.Messages[0].Message.Message.Content)
	assert.Equal(t, "Hi there!", retrieved.Messages[1].Message.Message.Content)
}

func TestUpdateSession_LazyCreation_InMemory(t *testing.T) {
	store := NewInMemorySessionStore()

	testAgent := agent.New("test-agent", "test prompt")

	// Create a session but don't add it to the store
	session := &Session{
		ID:        "lazy-session",
		CreatedAt: time.Now(),
	}

	// Verify session doesn't exist yet
	_, err := store.GetSession(t.Context(), "lazy-session")
	require.ErrorIs(t, err, ErrNotFound)

	// UpdateSession should create it (upsert)
	err = store.UpdateSession(t.Context(), session)
	require.NoError(t, err)

	// Add messages via AddItem
	item1 := NewMessageItem(UserMessage("Hello"))
	_, err = store.AddItem(t.Context(), session.ID, &item1)
	require.NoError(t, err)

	item2 := NewMessageItem(NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleAssistant,
		Content: "Hi there!",
	}))
	_, err = store.AddItem(t.Context(), session.ID, &item2)
	require.NoError(t, err)

	// Now the session should exist with messages
	retrieved, err := store.GetSession(t.Context(), "lazy-session")
	require.NoError(t, err)
	assert.Len(t, retrieved.Messages, 2)
}

func TestStorePermissions(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_permissions.db")

	store, err := NewSQLiteSessionStore(tempDB)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	// Create a session with permissions
	session := &Session{
		ID:        "permissions-session",
		CreatedAt: time.Now(),
		Permissions: &PermissionsConfig{
			Allow: []string{"read_*", "think"},
			Deny:  []string{"shell:cmd=rm*", "dangerous_tool"},
		},
	}

	// Store the session
	err = store.AddSession(t.Context(), session)
	require.NoError(t, err)

	// Retrieve the session
	retrieved, err := store.GetSession(t.Context(), "permissions-session")
	require.NoError(t, err)
	require.NotNil(t, retrieved.Permissions)

	assert.Equal(t, []string{"read_*", "think"}, retrieved.Permissions.Allow)
	assert.Equal(t, []string{"shell:cmd=rm*", "dangerous_tool"}, retrieved.Permissions.Deny)
}

func TestStorePermissions_NilPermissions(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_nil_permissions.db")

	store, err := NewSQLiteSessionStore(tempDB)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	// Create a session without permissions (legacy behavior)
	session := &Session{
		ID:        "no-permissions-session",
		CreatedAt: time.Now(),
	}

	// Store the session
	err = store.AddSession(t.Context(), session)
	require.NoError(t, err)

	// Retrieve the session
	retrieved, err := store.GetSession(t.Context(), "no-permissions-session")
	require.NoError(t, err)

	// Permissions should be nil
	assert.Nil(t, retrieved.Permissions)
}

func TestUpdateSession_Permissions(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_update_permissions.db")

	store, err := NewSQLiteSessionStore(tempDB)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	// Create a session without permissions
	session := &Session{
		ID:        "update-permissions-session",
		CreatedAt: time.Now(),
	}

	err = store.AddSession(t.Context(), session)
	require.NoError(t, err)

	// Update with permissions
	session.Permissions = &PermissionsConfig{
		Allow: []string{"safe_*"},
		Deny:  []string{"dangerous_*"},
	}

	err = store.UpdateSession(t.Context(), session)
	require.NoError(t, err)

	// Retrieve and verify
	retrieved, err := store.GetSession(t.Context(), "update-permissions-session")
	require.NoError(t, err)
	require.NotNil(t, retrieved.Permissions)

	assert.Equal(t, []string{"safe_*"}, retrieved.Permissions.Allow)
	assert.Equal(t, []string{"dangerous_*"}, retrieved.Permissions.Deny)
}

func TestAgentModelOverrides_SQLite(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_model_overrides.db")

	store, err := NewSQLiteSessionStore(tempDB)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	// Create a session with model overrides
	session := &Session{
		ID:        "model-override-session",
		Title:     "Test Session",
		CreatedAt: time.Now(),
		AgentModelOverrides: map[string]string{
			"root":       "openai/gpt-4o",
			"researcher": "anthropic/claude-sonnet-4-0",
		},
	}

	// Store the session
	err = store.AddSession(t.Context(), session)
	require.NoError(t, err)

	// Retrieve the session
	retrieved, err := store.GetSession(t.Context(), "model-override-session")
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	// Verify model overrides were persisted
	assert.Len(t, retrieved.AgentModelOverrides, 2)
	assert.Equal(t, "openai/gpt-4o", retrieved.AgentModelOverrides["root"])
	assert.Equal(t, "anthropic/claude-sonnet-4-0", retrieved.AgentModelOverrides["researcher"])
}

func TestAgentModelOverrides_Update(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_model_overrides_update.db")

	store, err := NewSQLiteSessionStore(tempDB)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	// Create a session without model overrides
	session := &Session{
		ID:        "update-model-override-session",
		Title:     "Test Session",
		CreatedAt: time.Now(),
	}

	err = store.AddSession(t.Context(), session)
	require.NoError(t, err)

	// Update the session with model overrides
	session.AgentModelOverrides = map[string]string{
		"root": "google/gemini-2.5-flash",
	}

	err = store.UpdateSession(t.Context(), session)
	require.NoError(t, err)

	// Retrieve and verify
	retrieved, err := store.GetSession(t.Context(), "update-model-override-session")
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	assert.Len(t, retrieved.AgentModelOverrides, 1)
	assert.Equal(t, "google/gemini-2.5-flash", retrieved.AgentModelOverrides["root"])
}

func TestAgentModelOverrides_EmptyMap(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_model_overrides_empty.db")

	store, err := NewSQLiteSessionStore(tempDB)
	require.NoError(t, err)
	defer store.(*SQLiteSessionStore).Close()

	// Create a session without model overrides (nil map)
	session := &Session{
		ID:        "no-override-session",
		Title:     "Test Session",
		CreatedAt: time.Now(),
	}

	err = store.AddSession(t.Context(), session)
	require.NoError(t, err)

	// Retrieve the session
	retrieved, err := store.GetSession(t.Context(), "no-override-session")
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	// Verify no model overrides (should be nil or empty)
	assert.Empty(t, retrieved.AgentModelOverrides)
}

func TestThinking_Persistence(t *testing.T) {
	t.Parallel()

	t.Run("default is true (thinking enabled)", func(t *testing.T) {
		t.Parallel()

		store, err := NewSQLiteSessionStore(filepath.Join(t.TempDir(), "test.db"))
		require.NoError(t, err)
		defer store.(*SQLiteSessionStore).Close()

		session := &Session{
			ID:        "thinking-default-session",
			Title:     "Test Session",
			CreatedAt: time.Now(),
			Thinking:  true, // Default value for new sessions
		}

		err = store.AddSession(t.Context(), session)
		require.NoError(t, err)

		retrieved, err := store.GetSession(t.Context(), "thinking-default-session")
		require.NoError(t, err)
		assert.True(t, retrieved.Thinking)
	})

	t.Run("persists when set to false (thinking disabled)", func(t *testing.T) {
		t.Parallel()

		store, err := NewSQLiteSessionStore(filepath.Join(t.TempDir(), "test.db"))
		require.NoError(t, err)
		defer store.(*SQLiteSessionStore).Close()

		session := &Session{
			ID:        "thinking-disabled-session",
			Title:     "Test Session",
			CreatedAt: time.Now(),
			Thinking:  false,
		}

		err = store.AddSession(t.Context(), session)
		require.NoError(t, err)

		retrieved, err := store.GetSession(t.Context(), "thinking-disabled-session")
		require.NoError(t, err)
		assert.False(t, retrieved.Thinking)
	})

	t.Run("updates correctly via toggle", func(t *testing.T) {
		t.Parallel()

		store, err := NewSQLiteSessionStore(filepath.Join(t.TempDir(), "test.db"))
		require.NoError(t, err)
		defer store.(*SQLiteSessionStore).Close()

		session := &Session{
			ID:        "thinking-toggle-session",
			Title:     "Test Session",
			CreatedAt: time.Now(),
			Thinking:  true,
		}

		err = store.AddSession(t.Context(), session)
		require.NoError(t, err)

		// Simulate toggle: true -> false
		session.Thinking = !session.Thinking
		err = store.UpdateSession(t.Context(), session)
		require.NoError(t, err)

		retrieved, err := store.GetSession(t.Context(), "thinking-toggle-session")
		require.NoError(t, err)
		assert.False(t, retrieved.Thinking)

		// Toggle again: false -> true
		session.Thinking = !session.Thinking
		err = store.UpdateSession(t.Context(), session)
		require.NoError(t, err)

		retrieved, err = store.GetSession(t.Context(), "thinking-toggle-session")
		require.NoError(t, err)
		assert.True(t, retrieved.Thinking)
	})
}

func TestAddItem_Message(t *testing.T) {
	t.Parallel()

	t.Run("SQLite", func(t *testing.T) {
		t.Parallel()

		store, err := NewSQLiteSessionStore(filepath.Join(t.TempDir(), "test.db"))
		require.NoError(t, err)
		defer store.(*SQLiteSessionStore).Close()

		testAddItemMessage(t, store)
	})

	t.Run("InMemory", func(t *testing.T) {
		t.Parallel()

		store := NewInMemorySessionStore()
		testAddItemMessage(t, store)
	})
}

func testAddItemMessage(t *testing.T, store Store) {
	t.Helper()

	testAgent := agent.New("test-agent", "test prompt")

	// Create a session first
	sess := &Session{
		ID:        "item-test-session",
		CreatedAt: time.Now(),
	}
	err := store.AddSession(t.Context(), sess)
	require.NoError(t, err)

	// Add a message item
	msg := NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleAssistant,
		Content: "Hello, I am a test message",
	})
	item := NewMessageItem(msg)

	itemID, err := store.AddItem(t.Context(), sess.ID, &item)
	require.NoError(t, err)
	assert.NotEmpty(t, itemID)

	// Retrieve items
	items, err := store.GetItems(t.Context(), sess.ID)
	require.NoError(t, err)
	assert.Len(t, items, 1)

	assert.Equal(t, itemID, items[0].ID)
	assert.Equal(t, 0, items[0].Position)
	assert.NotNil(t, items[0].Message)
	assert.Equal(t, "test-agent", items[0].Message.AgentName)
	assert.Equal(t, "Hello, I am a test message", items[0].Message.Message.Content)
}

func TestAddItem_Summary(t *testing.T) {
	t.Parallel()

	t.Run("SQLite", func(t *testing.T) {
		t.Parallel()

		store, err := NewSQLiteSessionStore(filepath.Join(t.TempDir(), "test.db"))
		require.NoError(t, err)
		defer store.(*SQLiteSessionStore).Close()

		testAddItemSummary(t, store)
	})

	t.Run("InMemory", func(t *testing.T) {
		t.Parallel()

		store := NewInMemorySessionStore()
		testAddItemSummary(t, store)
	})
}

func testAddItemSummary(t *testing.T, store Store) {
	t.Helper()

	// Create a session first
	sess := &Session{
		ID:        "summary-test-session",
		CreatedAt: time.Now(),
	}
	err := store.AddSession(t.Context(), sess)
	require.NoError(t, err)

	// Add a summary item
	item := Item{Summary: "This is a test summary of the conversation"}

	itemID, err := store.AddItem(t.Context(), sess.ID, &item)
	require.NoError(t, err)
	assert.NotEmpty(t, itemID)

	// Retrieve items
	items, err := store.GetItems(t.Context(), sess.ID)
	require.NoError(t, err)
	assert.Len(t, items, 1)

	assert.Equal(t, itemID, items[0].ID)
	assert.Equal(t, "This is a test summary of the conversation", items[0].Summary)
}

func TestUpdateItem(t *testing.T) {
	t.Parallel()

	t.Run("SQLite", func(t *testing.T) {
		t.Parallel()

		store, err := NewSQLiteSessionStore(filepath.Join(t.TempDir(), "test.db"))
		require.NoError(t, err)
		defer store.(*SQLiteSessionStore).Close()

		testUpdateItem(t, store)
	})

	t.Run("InMemory", func(t *testing.T) {
		t.Parallel()

		store := NewInMemorySessionStore()
		testUpdateItem(t, store)
	})
}

func testUpdateItem(t *testing.T, store Store) {
	t.Helper()

	testAgent := agent.New("test-agent", "test prompt")

	// Create a session first
	sess := &Session{
		ID:        "update-item-test-session",
		CreatedAt: time.Now(),
	}
	err := store.AddSession(t.Context(), sess)
	require.NoError(t, err)

	// Add a message item
	msg := NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleAssistant,
		Content: "Original content",
	})
	item := NewMessageItem(msg)

	itemID, err := store.AddItem(t.Context(), sess.ID, &item)
	require.NoError(t, err)

	// Update the item
	updatedMsg := NewAgentMessage(testAgent, &chat.Message{
		Role:    chat.MessageRoleAssistant,
		Content: "Updated content",
	})
	updatedItem := NewMessageItem(updatedMsg)

	err = store.UpdateItem(t.Context(), itemID, &updatedItem)
	require.NoError(t, err)

	// Retrieve and verify
	items, err := store.GetItems(t.Context(), sess.ID)
	require.NoError(t, err)
	assert.Len(t, items, 1)

	assert.Equal(t, "Updated content", items[0].Message.Message.Content)
}

func TestAddSubSession(t *testing.T) {
	t.Parallel()

	t.Run("SQLite", func(t *testing.T) {
		t.Parallel()

		store, err := NewSQLiteSessionStore(filepath.Join(t.TempDir(), "test.db"))
		require.NoError(t, err)
		defer store.(*SQLiteSessionStore).Close()

		testAddSubSession(t, store)
	})

	t.Run("InMemory", func(t *testing.T) {
		t.Parallel()

		store := NewInMemorySessionStore()
		testAddSubSession(t, store)
	})
}

func testAddSubSession(t *testing.T, store Store) {
	t.Helper()

	testAgent := agent.New("test-agent", "test prompt")

	// Create a parent session
	parentSess := &Session{
		ID:        "parent-session",
		CreatedAt: time.Now(),
	}
	err := store.AddSession(t.Context(), parentSess)
	require.NoError(t, err)

	// Create a sub-session
	subSess := &Session{
		Title:     "Sub-session for task",
		CreatedAt: time.Now(),
		Messages: []Item{
			NewMessageItem(NewAgentMessage(testAgent, &chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: "Sub-session message",
			})),
		},
	}

	// Add the sub-session
	subSessID, err := store.AddSubSession(t.Context(), parentSess.ID, subSess)
	require.NoError(t, err)
	assert.NotEmpty(t, subSessID)

	// Verify the sub-session was stored with parent_id
	retrievedSubSess, err := store.GetSession(t.Context(), subSessID)
	require.NoError(t, err)
	assert.Equal(t, "Sub-session for task", retrievedSubSess.Title)
	assert.Equal(t, parentSess.ID, retrievedSubSess.ParentID)

	// Verify the parent session has a reference item
	items, err := store.GetItems(t.Context(), parentSess.ID)
	require.NoError(t, err)
	assert.Len(t, items, 1)

	assert.Equal(t, subSessID, items[0].SubSessionID)
	assert.NotNil(t, items[0].SubSession)
	assert.Equal(t, "Sub-session for task", items[0].SubSession.Title)
}

func TestGetItems_Ordering(t *testing.T) {
	t.Parallel()

	t.Run("SQLite", func(t *testing.T) {
		t.Parallel()

		store, err := NewSQLiteSessionStore(filepath.Join(t.TempDir(), "test.db"))
		require.NoError(t, err)
		defer store.(*SQLiteSessionStore).Close()

		testGetItemsOrdering(t, store)
	})

	t.Run("InMemory", func(t *testing.T) {
		t.Parallel()

		store := NewInMemorySessionStore()
		testGetItemsOrdering(t, store)
	})
}

func testGetItemsOrdering(t *testing.T, store Store) {
	t.Helper()

	testAgent := agent.New("test-agent", "test prompt")

	// Create a session
	sess := &Session{
		ID:        "ordering-test-session",
		CreatedAt: time.Now(),
	}
	err := store.AddSession(t.Context(), sess)
	require.NoError(t, err)

	// Add multiple items
	for i := range 5 {
		msg := NewAgentMessage(testAgent, &chat.Message{
			Role:    chat.MessageRoleAssistant,
			Content: "Message " + string(rune('A'+i)),
		})
		item := NewMessageItem(msg)
		_, err := store.AddItem(t.Context(), sess.ID, &item)
		require.NoError(t, err)
	}

	// Retrieve and verify order
	items, err := store.GetItems(t.Context(), sess.ID)
	require.NoError(t, err)
	assert.Len(t, items, 5)

	for i := range 5 {
		assert.Equal(t, i, items[i].Position)
		assert.Equal(t, "Message "+string(rune('A'+i)), items[i].Message.Message.Content)
	}
}
