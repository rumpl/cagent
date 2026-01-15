package storev2

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tools"
)

func TestStoreAgentName(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_store.db")

	store, err := New(tempDB)
	require.NoError(t, err)
	defer store.Close()

	testAgent1 := agent.New("test-agent-1", "test prompt 1")
	testAgent2 := agent.New("test-agent-2", "test prompt 2")

	sess := &session.Session{
		ID: "test-session",
		Messages: []session.Item{
			session.NewMessageItem(session.UserMessage("Hello")),
			session.NewMessageItem(session.NewAgentMessage(testAgent1, &chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: "Hello from test-agent-1",
			})),
			session.NewMessageItem(session.NewAgentMessage(testAgent2, &chat.Message{
				Role:    chat.MessageRoleUser,
				Content: "Another message from test-agent-2",
			})),
		},
		InputTokens:  100,
		OutputTokens: 200,
		CreatedAt:    time.Now(),
	}

	err = store.AddSession(t.Context(), sess)
	require.NoError(t, err)

	retrievedSession, err := store.GetSession(t.Context(), "test-session")
	require.NoError(t, err)
	require.NotNil(t, retrievedSession)

	assert.Len(t, retrievedSession.GetAllMessages(), 3)
	assert.Empty(t, retrievedSession.Messages[0].Message.AgentName)
	assert.Equal(t, "Hello", retrievedSession.Messages[0].Message.Message.Content)
	assert.Equal(t, "test-agent-1", retrievedSession.Messages[1].Message.AgentName)
	assert.Equal(t, "Hello from test-agent-1", retrievedSession.Messages[1].Message.Message.Content)
	assert.Equal(t, "test-agent-2", retrievedSession.Messages[2].Message.AgentName)
	assert.Equal(t, "Another message from test-agent-2", retrievedSession.Messages[2].Message.Message.Content)
}

func TestGetSessions(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_get_sessions.db")

	store, err := New(tempDB)
	require.NoError(t, err)
	defer store.Close()

	testAgent := agent.New("test-agent", "test prompt")

	session1 := &session.Session{
		ID: "session-1",
		Messages: []session.Item{
			session.NewMessageItem(session.NewAgentMessage(testAgent, &chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: "Message from session 1",
			})),
		},
		CreatedAt: time.Now().Add(-1 * time.Hour),
	}

	session2 := &session.Session{
		ID: "session-2",
		Messages: []session.Item{
			session.NewMessageItem(session.NewAgentMessage(testAgent, &chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: "Message from session 2",
			})),
		},
		CreatedAt: time.Now(),
	}

	err = store.AddSession(t.Context(), session1)
	require.NoError(t, err)
	err = store.AddSession(t.Context(), session2)
	require.NoError(t, err)

	sessions, err := store.GetSessions(t.Context())
	require.NoError(t, err)
	assert.Len(t, sessions, 2)

	for _, s := range sessions {
		assert.Len(t, s.Messages, 1)
		assert.Equal(t, "test-agent", s.Messages[0].Message.AgentName)
	}
}

func TestGetSessionSummaries(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_get_session_summaries.db")

	store, err := New(tempDB)
	require.NoError(t, err)
	defer store.Close()

	testAgent := agent.New("test-agent", "test prompt")

	session1Time := time.Now().UTC().Add(-1 * time.Hour).Truncate(time.Second)
	session2Time := time.Now().UTC().Truncate(time.Second)

	session1 := &session.Session{
		ID:    "session-1",
		Title: "First Session",
		Messages: []session.Item{
			session.NewMessageItem(session.NewAgentMessage(testAgent, &chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: "A very long message that should not be loaded when getting summaries",
			})),
		},
		CreatedAt: session1Time,
	}

	session2 := &session.Session{
		ID:    "session-2",
		Title: "Second Session",
		Messages: []session.Item{
			session.NewMessageItem(session.NewAgentMessage(testAgent, &chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: "Another long message that should not be loaded when getting summaries",
			})),
		},
		CreatedAt: session2Time,
	}

	err = store.AddSession(t.Context(), session1)
	require.NoError(t, err)
	err = store.AddSession(t.Context(), session2)
	require.NoError(t, err)

	summaries, err := store.GetSessionSummaries(t.Context())
	require.NoError(t, err)
	assert.Len(t, summaries, 2)

	assert.Equal(t, "session-2", summaries[0].ID)
	assert.Equal(t, "Second Session", summaries[0].Title)
	assert.Equal(t, session2Time, summaries[0].CreatedAt)

	assert.Equal(t, "session-1", summaries[1].ID)
	assert.Equal(t, "First Session", summaries[1].Title)
	assert.Equal(t, session1Time, summaries[1].CreatedAt)
}

func TestUpdateSession_LazyCreation(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_lazy.db")

	store, err := New(tempDB)
	require.NoError(t, err)
	defer store.Close()

	testAgent := agent.New("test-agent", "test prompt")

	sess := &session.Session{
		ID:        "lazy-session",
		CreatedAt: time.Now(),
	}

	_, err = store.GetSession(t.Context(), "lazy-session")
	require.ErrorIs(t, err, session.ErrNotFound)

	sess.Messages = []session.Item{
		session.NewMessageItem(session.UserMessage("Hello")),
		session.NewMessageItem(session.NewAgentMessage(testAgent, &chat.Message{
			Role:    chat.MessageRoleAssistant,
			Content: "Hi there!",
		})),
	}

	err = store.UpdateSession(t.Context(), sess)
	require.NoError(t, err)

	retrieved, err := store.GetSession(t.Context(), "lazy-session")
	require.NoError(t, err)
	assert.Len(t, retrieved.Messages, 2)
	assert.Equal(t, "Hello", retrieved.Messages[0].Message.Message.Content)
	assert.Equal(t, "Hi there!", retrieved.Messages[1].Message.Message.Content)
}

func TestStorePermissions(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_permissions.db")

	store, err := New(tempDB)
	require.NoError(t, err)
	defer store.Close()

	sess := &session.Session{
		ID:        "permissions-session",
		CreatedAt: time.Now(),
		Permissions: &session.PermissionsConfig{
			Allow: []string{"read_*", "think"},
			Deny:  []string{"shell:cmd=rm*", "dangerous_tool"},
		},
	}

	err = store.AddSession(t.Context(), sess)
	require.NoError(t, err)

	retrieved, err := store.GetSession(t.Context(), "permissions-session")
	require.NoError(t, err)
	require.NotNil(t, retrieved.Permissions)

	assert.Equal(t, []string{"read_*", "think"}, retrieved.Permissions.Allow)
	assert.Equal(t, []string{"shell:cmd=rm*", "dangerous_tool"}, retrieved.Permissions.Deny)
}

func TestAgentModelOverrides(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_model_overrides.db")

	store, err := New(tempDB)
	require.NoError(t, err)
	defer store.Close()

	sess := &session.Session{
		ID:        "model-override-session",
		Title:     "Test Session",
		CreatedAt: time.Now(),
		AgentModelOverrides: map[string]string{
			"root":       "openai/gpt-4o",
			"researcher": "anthropic/claude-sonnet-4-0",
		},
	}

	err = store.AddSession(t.Context(), sess)
	require.NoError(t, err)

	retrieved, err := store.GetSession(t.Context(), "model-override-session")
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	assert.Len(t, retrieved.AgentModelOverrides, 2)
	assert.Equal(t, "openai/gpt-4o", retrieved.AgentModelOverrides["root"])
	assert.Equal(t, "anthropic/claude-sonnet-4-0", retrieved.AgentModelOverrides["researcher"])
}

func TestCustomModelsUsed(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_custom_models.db")

	store, err := New(tempDB)
	require.NoError(t, err)
	defer store.Close()

	sess := &session.Session{
		ID:        "custom-models-session",
		Title:     "Test Session",
		CreatedAt: time.Now(),
		CustomModelsUsed: []string{
			"openai/gpt-4o-mini",
			"anthropic/claude-sonnet-4-0",
		},
	}

	err = store.AddSession(t.Context(), sess)
	require.NoError(t, err)

	retrieved, err := store.GetSession(t.Context(), "custom-models-session")
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	assert.Len(t, retrieved.CustomModelsUsed, 2)
	assert.Contains(t, retrieved.CustomModelsUsed, "openai/gpt-4o-mini")
	assert.Contains(t, retrieved.CustomModelsUsed, "anthropic/claude-sonnet-4-0")
}

func TestDeleteSession(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_delete.db")

	store, err := New(tempDB)
	require.NoError(t, err)
	defer store.Close()

	sess := &session.Session{
		ID:        "delete-me",
		CreatedAt: time.Now(),
	}

	err = store.AddSession(t.Context(), sess)
	require.NoError(t, err)

	_, err = store.GetSession(t.Context(), "delete-me")
	require.NoError(t, err)

	err = store.DeleteSession(t.Context(), "delete-me")
	require.NoError(t, err)

	_, err = store.GetSession(t.Context(), "delete-me")
	require.ErrorIs(t, err, session.ErrNotFound)
}

func TestSetSessionStarred(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_starred.db")

	store, err := New(tempDB)
	require.NoError(t, err)
	defer store.Close()

	sess := &session.Session{
		ID:        "star-me",
		CreatedAt: time.Now(),
		Starred:   false,
	}

	err = store.AddSession(t.Context(), sess)
	require.NoError(t, err)

	err = store.SetSessionStarred(t.Context(), "star-me", true)
	require.NoError(t, err)

	retrieved, err := store.GetSession(t.Context(), "star-me")
	require.NoError(t, err)
	assert.True(t, retrieved.Starred)

	err = store.SetSessionStarred(t.Context(), "star-me", false)
	require.NoError(t, err)

	retrieved, err = store.GetSession(t.Context(), "star-me")
	require.NoError(t, err)
	assert.False(t, retrieved.Starred)
}

func TestToolCallsAndDefinitions(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_tool_calls.db")

	store, err := New(tempDB)
	require.NoError(t, err)
	defer store.Close()

	testAgent := agent.New("test-agent", "test prompt")

	sess := &session.Session{
		ID:        "tool-calls-session",
		CreatedAt: time.Now(),
		Messages: []session.Item{
			session.NewMessageItem(session.NewAgentMessage(testAgent, &chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: "I'll help you with that.",
				ToolCalls: []tools.ToolCall{
					{
						ID:   "call_123",
						Type: "function",
						Function: tools.FunctionCall{
							Name:      "shell",
							Arguments: `{"cmd": "ls -la"}`,
						},
					},
				},
				ToolDefinitions: []tools.Tool{
					{
						Name:        "shell",
						Category:    "system",
						Description: "Execute shell commands",
					},
				},
			})),
			session.NewMessageItem(&session.Message{
				Message: chat.Message{
					Role:       chat.MessageRoleTool,
					Content:    "file1.txt\nfile2.txt",
					ToolCallID: "call_123",
				},
			}),
		},
	}

	err = store.AddSession(t.Context(), sess)
	require.NoError(t, err)

	retrieved, err := store.GetSession(t.Context(), "tool-calls-session")
	require.NoError(t, err)
	require.Len(t, retrieved.Messages, 2)

	assistantMsg := retrieved.Messages[0].Message.Message
	require.Len(t, assistantMsg.ToolCalls, 1)
	assert.Equal(t, "call_123", assistantMsg.ToolCalls[0].ID)
	assert.Equal(t, "shell", assistantMsg.ToolCalls[0].Function.Name)
	assert.JSONEq(t, `{"cmd": "ls -la"}`, assistantMsg.ToolCalls[0].Function.Arguments)

	require.Len(t, assistantMsg.ToolDefinitions, 1)
	assert.Equal(t, "shell", assistantMsg.ToolDefinitions[0].Name)
	assert.Equal(t, "system", assistantMsg.ToolDefinitions[0].Category)

	toolMsg := retrieved.Messages[1].Message.Message
	assert.Equal(t, chat.MessageRoleTool, toolMsg.Role)
	assert.Equal(t, "call_123", toolMsg.ToolCallID)
	assert.Equal(t, "file1.txt\nfile2.txt", toolMsg.Content)
}

func TestMessageUsage(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_usage.db")

	store, err := New(tempDB)
	require.NoError(t, err)
	defer store.Close()

	testAgent := agent.New("test-agent", "test prompt")

	sess := &session.Session{
		ID:        "usage-session",
		CreatedAt: time.Now(),
		Messages: []session.Item{
			session.NewMessageItem(session.NewAgentMessage(testAgent, &chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: "Hello!",
				Usage: &chat.Usage{
					InputTokens:       100,
					OutputTokens:      50,
					CachedInputTokens: 20,
					CacheWriteTokens:  10,
					ReasoningTokens:   5,
				},
				Cost: 0.0025,
			})),
		},
	}

	err = store.AddSession(t.Context(), sess)
	require.NoError(t, err)

	retrieved, err := store.GetSession(t.Context(), "usage-session")
	require.NoError(t, err)
	require.Len(t, retrieved.Messages, 1)

	msg := retrieved.Messages[0].Message.Message
	require.NotNil(t, msg.Usage)
	assert.Equal(t, int64(100), msg.Usage.InputTokens)
	assert.Equal(t, int64(50), msg.Usage.OutputTokens)
	assert.Equal(t, int64(20), msg.Usage.CachedInputTokens)
	assert.Equal(t, int64(10), msg.Usage.CacheWriteTokens)
	assert.Equal(t, int64(5), msg.Usage.ReasoningTokens)
	assert.InDelta(t, 0.0025, msg.Cost, 0.0001)
}

func TestMultiContent(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_multicontent.db")

	store, err := New(tempDB)
	require.NoError(t, err)
	defer store.Close()

	sess := &session.Session{
		ID:        "multicontent-session",
		CreatedAt: time.Now(),
		Messages: []session.Item{
			session.NewMessageItem(&session.Message{
				Message: chat.Message{
					Role: chat.MessageRoleUser,
					MultiContent: []chat.MessagePart{
						{Type: chat.MessagePartTypeText, Text: "What's in this image?"},
						{Type: chat.MessagePartTypeImageURL, ImageURL: &chat.MessageImageURL{
							URL:    "https://example.com/image.png",
							Detail: chat.ImageURLDetailHigh,
						}},
					},
				},
			}),
		},
	}

	err = store.AddSession(t.Context(), sess)
	require.NoError(t, err)

	retrieved, err := store.GetSession(t.Context(), "multicontent-session")
	require.NoError(t, err)
	require.Len(t, retrieved.Messages, 1)

	msg := retrieved.Messages[0].Message.Message
	require.Len(t, msg.MultiContent, 2)

	assert.Equal(t, chat.MessagePartTypeText, msg.MultiContent[0].Type)
	assert.Equal(t, "What's in this image?", msg.MultiContent[0].Text)

	assert.Equal(t, chat.MessagePartTypeImageURL, msg.MultiContent[1].Type)
	require.NotNil(t, msg.MultiContent[1].ImageURL)
	assert.Equal(t, "https://example.com/image.png", msg.MultiContent[1].ImageURL.URL)
	assert.Equal(t, chat.ImageURLDetailHigh, msg.MultiContent[1].ImageURL.Detail)
}

func TestSubSessions(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_subsessions.db")

	store, err := New(tempDB)
	require.NoError(t, err)
	defer store.Close()

	testAgent := agent.New("test-agent", "test prompt")
	subAgent := agent.New("sub-agent", "sub prompt")

	subSession := &session.Session{
		ID:        "sub-session-1",
		Title:     "Sub Session",
		CreatedAt: time.Now(),
		Messages: []session.Item{
			session.NewMessageItem(session.UserMessage("Sub task")),
			session.NewMessageItem(session.NewAgentMessage(subAgent, &chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: "Sub response",
			})),
		},
	}

	sess := &session.Session{
		ID:        "parent-session",
		Title:     "Parent Session",
		CreatedAt: time.Now(),
		Messages: []session.Item{
			session.NewMessageItem(session.UserMessage("Main task")),
			session.NewMessageItem(session.NewAgentMessage(testAgent, &chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: "I'll delegate this.",
			})),
			session.NewSubSessionItem(subSession),
			session.NewMessageItem(session.NewAgentMessage(testAgent, &chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: "Sub task completed.",
			})),
		},
	}

	err = store.AddSession(t.Context(), sess)
	require.NoError(t, err)

	retrieved, err := store.GetSession(t.Context(), "parent-session")
	require.NoError(t, err)
	require.Len(t, retrieved.Messages, 4)

	assert.True(t, retrieved.Messages[0].IsMessage())
	assert.True(t, retrieved.Messages[1].IsMessage())
	assert.True(t, retrieved.Messages[2].IsSubSession())
	assert.True(t, retrieved.Messages[3].IsMessage())

	subSess := retrieved.Messages[2].SubSession
	require.NotNil(t, subSess)
	assert.Equal(t, "sub-session-1", subSess.ID)
	assert.Equal(t, "Sub Session", subSess.Title)
	require.Len(t, subSess.Messages, 2)
	assert.Equal(t, "Sub task", subSess.Messages[0].Message.Message.Content)
	assert.Equal(t, "Sub response", subSess.Messages[1].Message.Message.Content)
}

func TestSummaryItems(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_summary.db")

	store, err := New(tempDB)
	require.NoError(t, err)
	defer store.Close()

	testAgent := agent.New("test-agent", "test prompt")

	sess := &session.Session{
		ID:        "summary-session",
		CreatedAt: time.Now(),
		Messages: []session.Item{
			session.NewMessageItem(session.UserMessage("First message")),
			session.NewMessageItem(session.NewAgentMessage(testAgent, &chat.Message{
				Role:    chat.MessageRoleAssistant,
				Content: "First response",
			})),
			{Summary: "This is a summary of the conversation so far."},
			session.NewMessageItem(session.UserMessage("Continue")),
		},
	}

	err = store.AddSession(t.Context(), sess)
	require.NoError(t, err)

	retrieved, err := store.GetSession(t.Context(), "summary-session")
	require.NoError(t, err)
	require.Len(t, retrieved.Messages, 4)

	assert.True(t, retrieved.Messages[0].IsMessage())
	assert.True(t, retrieved.Messages[1].IsMessage())
	assert.Equal(t, "This is a summary of the conversation so far.", retrieved.Messages[2].Summary)
	assert.True(t, retrieved.Messages[3].IsMessage())
}

func TestEmptyID(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_empty_id.db")

	store, err := New(tempDB)
	require.NoError(t, err)
	defer store.Close()

	err = store.AddSession(t.Context(), &session.Session{ID: ""})
	require.ErrorIs(t, err, session.ErrEmptyID)

	_, err = store.GetSession(t.Context(), "")
	require.ErrorIs(t, err, session.ErrEmptyID)

	err = store.DeleteSession(t.Context(), "")
	require.ErrorIs(t, err, session.ErrEmptyID)

	err = store.UpdateSession(t.Context(), &session.Session{ID: ""})
	require.ErrorIs(t, err, session.ErrEmptyID)

	err = store.SetSessionStarred(t.Context(), "", true)
	require.ErrorIs(t, err, session.ErrEmptyID)
}

func TestToolDefinitionDeduplication(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_tool_dedup.db")

	store, err := New(tempDB)
	require.NoError(t, err)
	defer store.Close()

	testAgent := agent.New("test-agent", "test prompt")
	shellTool := tools.Tool{
		Name:        "shell",
		Category:    "system",
		Description: "Execute shell commands",
	}

	sess := &session.Session{
		ID:        "dedup-session",
		CreatedAt: time.Now(),
		Messages: []session.Item{
			session.NewMessageItem(session.NewAgentMessage(testAgent, &chat.Message{
				Role:            chat.MessageRoleAssistant,
				Content:         "First call",
				ToolDefinitions: []tools.Tool{shellTool},
			})),
			session.NewMessageItem(session.NewAgentMessage(testAgent, &chat.Message{
				Role:            chat.MessageRoleAssistant,
				Content:         "Second call",
				ToolDefinitions: []tools.Tool{shellTool},
			})),
		},
	}

	err = store.AddSession(t.Context(), sess)
	require.NoError(t, err)

	// Verify only one tool definition exists in the database
	var count int
	err = store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM tool_definitions WHERE name = 'shell'`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// But both messages should reference it
	retrieved, err := store.GetSession(t.Context(), "dedup-session")
	require.NoError(t, err)
	require.Len(t, retrieved.Messages, 2)
	assert.Len(t, retrieved.Messages[0].Message.Message.ToolDefinitions, 1)
	assert.Len(t, retrieved.Messages[1].Message.Message.ToolDefinitions, 1)
}
