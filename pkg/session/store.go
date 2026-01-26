package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/docker/cagent/pkg/concurrent"
	"github.com/docker/cagent/pkg/sqliteutil"
)

var (
	ErrEmptyID  = errors.New("session ID cannot be empty")
	ErrNotFound = errors.New("session not found")
)

// Summary contains lightweight session metadata for listing purposes.
// This is used instead of loading full Session objects with all messages.
type Summary struct {
	ID        string
	Title     string
	CreatedAt time.Time
	Starred   bool
}

// Store defines the interface for session storage
type Store interface {
	AddSession(ctx context.Context, session *Session) error
	GetSession(ctx context.Context, id string) (*Session, error)
	GetSessions(ctx context.Context) ([]*Session, error)
	GetSessionSummaries(ctx context.Context) ([]Summary, error)
	DeleteSession(ctx context.Context, id string) error
	UpdateSession(ctx context.Context, session *Session) error
	SetSessionStarred(ctx context.Context, id string, starred bool) error
	AddMessage(ctx context.Context, sessionID string, message *Item) (string, error)
	EditMessage(ctx context.Context, sessionID, messageID string, message *Item) error
}

type InMemorySessionStore struct {
	sessions *concurrent.Map[string, *Session]
}

func NewInMemorySessionStore() Store {
	return &InMemorySessionStore{
		sessions: concurrent.NewMap[string, *Session](),
	}
}

func (s *InMemorySessionStore) AddSession(_ context.Context, session *Session) error {
	if session.ID == "" {
		return ErrEmptyID
	}
	s.sessions.Store(session.ID, session)
	return nil
}

func (s *InMemorySessionStore) GetSession(_ context.Context, id string) (*Session, error) {
	if id == "" {
		return nil, ErrEmptyID
	}
	session, exists := s.sessions.Load(id)
	if !exists {
		return nil, ErrNotFound
	}
	return session, nil
}

func (s *InMemorySessionStore) GetSessions(_ context.Context) ([]*Session, error) {
	sessions := make([]*Session, 0, s.sessions.Length())
	s.sessions.Range(func(key string, value *Session) bool {
		sessions = append(sessions, value)
		return true
	})
	return sessions, nil
}

func (s *InMemorySessionStore) GetSessionSummaries(_ context.Context) ([]Summary, error) {
	summaries := make([]Summary, 0, s.sessions.Length())
	s.sessions.Range(func(_ string, value *Session) bool {
		summaries = append(summaries, Summary{
			ID:        value.ID,
			Title:     value.Title,
			CreatedAt: value.CreatedAt,
			Starred:   value.Starred,
		})
		return true
	})
	return summaries, nil
}

func (s *InMemorySessionStore) DeleteSession(_ context.Context, id string) error {
	if id == "" {
		return ErrEmptyID
	}
	_, exists := s.sessions.Load(id)
	if !exists {
		return ErrNotFound
	}
	s.sessions.Delete(id)
	return nil
}

// UpdateSession updates an existing session, or creates it if it doesn't exist (upsert).
// This enables lazy session persistence - sessions are only stored when they have content.
func (s *InMemorySessionStore) UpdateSession(_ context.Context, session *Session) error {
	if session.ID == "" {
		return ErrEmptyID
	}
	s.sessions.Store(session.ID, session)
	return nil
}

// SetSessionStarred sets the starred status of a session.
func (s *InMemorySessionStore) SetSessionStarred(_ context.Context, id string, starred bool) error {
	if id == "" {
		return ErrEmptyID
	}
	session, exists := s.sessions.Load(id)
	if !exists {
		return ErrNotFound
	}
	session.Starred = starred
	s.sessions.Store(id, session)
	return nil
}

// AddMessage adds a message to a session and returns the message ID.
func (s *InMemorySessionStore) AddMessage(_ context.Context, sessionID string, item *Item) (string, error) {
	if sessionID == "" {
		return "", ErrEmptyID
	}
	session, exists := s.sessions.Load(sessionID)
	if !exists {
		return "", ErrNotFound
	}
	messageID := uuid.New().String()
	item.ID = messageID
	session.Messages = append(session.Messages, *item)
	s.sessions.Store(sessionID, session)

	// If this is a sub-session, also store it in the sessions map
	if item.SubSession != nil {
		s.sessions.Store(item.SubSession.ID, item.SubSession)
	}

	return messageID, nil
}

// EditMessage updates an existing message by ID.
func (s *InMemorySessionStore) EditMessage(_ context.Context, sessionID, messageID string, item *Item) error {
	if sessionID == "" || messageID == "" {
		return ErrEmptyID
	}
	session, exists := s.sessions.Load(sessionID)
	if !exists {
		return ErrNotFound
	}
	for i := range session.Messages {
		if session.Messages[i].ID == messageID {
			item.ID = messageID
			session.Messages[i] = *item
			s.sessions.Store(sessionID, session)
			return nil
		}
	}
	return ErrNotFound
}

// SQLiteSessionStore implements Store using SQLite
type SQLiteSessionStore struct {
	db *sql.DB
}

// NewSQLiteSessionStore creates a new SQLite session store
func NewSQLiteSessionStore(path string) (Store, error) {
	db, err := sqliteutil.OpenDB(path)
	if err != nil {
		return nil, err
	}

	_, err = db.ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			messages TEXT,
			created_at TEXT
		)
	`)
	if err != nil {
		db.Close()
		if sqliteutil.IsCantOpenError(err) {
			return nil, sqliteutil.DiagnoseDBOpenError(path, err)
		}
		return nil, err
	}

	// Initialize and run migrations
	migrationManager := NewMigrationManager(db)
	err = migrationManager.InitializeMigrations(context.Background())
	if err != nil {
		return nil, err
	}

	return &SQLiteSessionStore{db: db}, nil
}

// AddSession adds a new session to the store
func (s *SQLiteSessionStore) AddSession(ctx context.Context, session *Session) error {
	if session.ID == "" {
		return ErrEmptyID
	}

	permissionsJSON := ""
	if session.Permissions != nil {
		permBytes, err := json.Marshal(session.Permissions)
		if err != nil {
			return err
		}
		permissionsJSON = string(permBytes)
	}

	// Marshal agent model overrides (default to empty object if nil)
	agentModelOverridesJSON := "{}"
	if len(session.AgentModelOverrides) > 0 {
		overridesBytes, err := json.Marshal(session.AgentModelOverrides)
		if err != nil {
			return err
		}
		agentModelOverridesJSON = string(overridesBytes)
	}

	// Marshal custom models used (default to empty array if nil)
	customModelsUsedJSON := "[]"
	if len(session.CustomModelsUsed) > 0 {
		customBytes, err := json.Marshal(session.CustomModelsUsed)
		if err != nil {
			return err
		}
		customModelsUsedJSON = string(customBytes)
	}

	// Convert empty ParentID to nil for proper NULL storage
	var parentID *string
	if session.ParentID != "" {
		parentID = &session.ParentID
	}

	// Insert session metadata (messages column is kept for backward compatibility but not used)
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO sessions (id, messages, tools_approved, input_tokens, output_tokens, title, send_user_message, max_iterations, working_dir, created_at, permissions, agent_model_overrides, custom_models_used, thinking, parent_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		session.ID, "[]", session.ToolsApproved, session.InputTokens, session.OutputTokens, session.Title, session.SendUserMessage, session.MaxIterations, session.WorkingDir, session.CreatedAt.Format(time.RFC3339), permissionsJSON, agentModelOverridesJSON, customModelsUsedJSON, session.Thinking, parentID)
	if err != nil {
		return err
	}

	// Insert messages into the messages table
	for position := range session.Messages {
		item := &session.Messages[position]
		if item.ID == "" {
			item.ID = uuid.New().String()
		}
		itemJSON, err := json.Marshal(item)
		if err != nil {
			return err
		}
		_, err = s.db.ExecContext(ctx,
			"INSERT INTO messages (id, session_id, position, data) VALUES (?, ?, ?, ?)",
			item.ID, session.ID, position, string(itemJSON))
		if err != nil {
			return err
		}
	}

	return nil
}

// scanSession scans a single row into a Session struct
// scanSessionMetadata scans a single row into a Session struct without messages
// (messages are loaded separately from the messages table)
func scanSessionMetadata(scanner interface {
	Scan(dest ...any) error
},
) (*Session, error) {
	var toolsApprovedStr, inputTokensStr, outputTokensStr, titleStr, costStr, sendUserMessageStr, maxIterationsStr, createdAtStr, starredStr, agentModelOverridesJSON, customModelsUsedJSON, thinkingStr string
	var sessionID string
	var workingDir sql.NullString
	var permissionsJSON sql.NullString
	var parentID sql.NullString

	err := scanner.Scan(&sessionID, &toolsApprovedStr, &inputTokensStr, &outputTokensStr, &titleStr, &costStr, &sendUserMessageStr, &maxIterationsStr, &workingDir, &createdAtStr, &starredStr, &permissionsJSON, &agentModelOverridesJSON, &customModelsUsedJSON, &thinkingStr, &parentID)
	if err != nil {
		return nil, err
	}

	toolsApproved, err := strconv.ParseBool(toolsApprovedStr)
	if err != nil {
		return nil, err
	}

	inputTokens, err := strconv.ParseInt(inputTokensStr, 10, 64)
	if err != nil {
		return nil, err
	}

	outputTokens, err := strconv.ParseInt(outputTokensStr, 10, 64)
	if err != nil {
		return nil, err
	}

	cost, err := strconv.ParseFloat(costStr, 64)
	if err != nil {
		return nil, err
	}

	sendUserMessage, err := strconv.ParseBool(sendUserMessageStr)
	if err != nil {
		return nil, err
	}

	maxIterations, err := strconv.Atoi(maxIterationsStr)
	if err != nil {
		return nil, err
	}

	createdAt, err := time.Parse(time.RFC3339, createdAtStr)
	if err != nil {
		return nil, err
	}

	starred, err := strconv.ParseBool(starredStr)
	if err != nil {
		return nil, err
	}

	thinking, err := strconv.ParseBool(thinkingStr)
	if err != nil {
		return nil, err
	}

	// Parse permissions if present
	var permissions *PermissionsConfig
	if permissionsJSON.Valid && permissionsJSON.String != "" {
		permissions = &PermissionsConfig{}
		if err := json.Unmarshal([]byte(permissionsJSON.String), permissions); err != nil {
			return nil, err
		}
	}

	// Parse agent model overrides (may be empty or "{}")
	var agentModelOverrides map[string]string
	if agentModelOverridesJSON != "" && agentModelOverridesJSON != "{}" {
		if err := json.Unmarshal([]byte(agentModelOverridesJSON), &agentModelOverrides); err != nil {
			return nil, err
		}
	}

	// Parse custom models used (may be empty or "[]")
	var customModelsUsed []string
	if customModelsUsedJSON != "" && customModelsUsedJSON != "[]" {
		if err := json.Unmarshal([]byte(customModelsUsedJSON), &customModelsUsed); err != nil {
			return nil, err
		}
	}

	return &Session{
		ID:                  sessionID,
		Title:               titleStr,
		ToolsApproved:       toolsApproved,
		Thinking:            thinking,
		InputTokens:         inputTokens,
		OutputTokens:        outputTokens,
		Cost:                cost,
		SendUserMessage:     sendUserMessage,
		MaxIterations:       maxIterations,
		CreatedAt:           createdAt,
		WorkingDir:          workingDir.String,
		Starred:             starred,
		Permissions:         permissions,
		AgentModelOverrides: agentModelOverrides,
		CustomModelsUsed:    customModelsUsed,
		ParentID:            parentID.String,
	}, nil
}

// loadSessionMessages loads messages for a session from the messages table
func (s *SQLiteSessionStore) loadSessionMessages(ctx context.Context, sessionID string) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT data FROM messages WHERE session_id = ? ORDER BY position", sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		var itemJSON string
		if err := rows.Scan(&itemJSON); err != nil {
			return nil, err
		}

		var item Item
		if err := json.Unmarshal([]byte(itemJSON), &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Resolve sub-session references
	for i := range items {
		if items[i].SubSessionID != "" {
			subSession, err := s.GetSession(ctx, items[i].SubSessionID)
			if err != nil {
				return nil, err
			}
			items[i].SubSession = subSession
			items[i].SubSessionID = "" // Clear the reference since we've loaded the full session
		}
	}

	return items, nil
}

// GetSession retrieves a session by ID
func (s *SQLiteSessionStore) GetSession(ctx context.Context, id string) (*Session, error) {
	if id == "" {
		return nil, ErrEmptyID
	}

	row := s.db.QueryRowContext(ctx,
		"SELECT id, tools_approved, input_tokens, output_tokens, title, cost, send_user_message, max_iterations, working_dir, created_at, starred, permissions, agent_model_overrides, custom_models_used, thinking, parent_id FROM sessions WHERE id = ?", id)

	session, err := scanSessionMetadata(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	// Load messages from the messages table
	messages, err := s.loadSessionMessages(ctx, id)
	if err != nil {
		return nil, err
	}
	session.Messages = messages

	return session, nil
}

// GetSessions retrieves all top-level sessions (excludes sub-sessions)
func (s *SQLiteSessionStore) GetSessions(ctx context.Context) ([]*Session, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, tools_approved, input_tokens, output_tokens, title, cost, send_user_message, max_iterations, working_dir, created_at, starred, permissions, agent_model_overrides, custom_models_used, thinking, parent_id FROM sessions WHERE parent_id IS NULL ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		session, err := scanSessionMetadata(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}

	// Load messages for each session
	for _, session := range sessions {
		messages, err := s.loadSessionMessages(ctx, session.ID)
		if err != nil {
			return nil, err
		}
		session.Messages = messages
	}

	return sessions, nil
}

// GetSessionSummaries retrieves lightweight session metadata for listing.
// This is much faster than GetSessions as it doesn't load message content.
// Excludes sub-sessions (only returns top-level sessions).
func (s *SQLiteSessionStore) GetSessionSummaries(ctx context.Context) ([]Summary, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, title, created_at, starred FROM sessions WHERE parent_id IS NULL ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []Summary
	for rows.Next() {
		var id, title, createdAtStr, starredStr string
		if err := rows.Scan(&id, &title, &createdAtStr, &starredStr); err != nil {
			return nil, err
		}
		createdAt, err := time.Parse(time.RFC3339, createdAtStr)
		if err != nil {
			return nil, err
		}
		starred, err := strconv.ParseBool(starredStr)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, Summary{
			ID:        id,
			Title:     title,
			CreatedAt: createdAt,
			Starred:   starred,
		})
	}

	return summaries, nil
}

// DeleteSession deletes a session by ID
func (s *SQLiteSessionStore) DeleteSession(ctx context.Context, id string) error {
	if id == "" {
		return ErrEmptyID
	}

	result, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE id = ?", id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

// UpdateSession updates an existing session, or creates it if it doesn't exist (upsert).
// This only updates session metadata (title, tokens, etc.) - NOT messages.
// Use AddMessage/EditMessage for message persistence.
func (s *SQLiteSessionStore) UpdateSession(ctx context.Context, session *Session) error {
	if session.ID == "" {
		return ErrEmptyID
	}

	permissionsJSON := ""
	if session.Permissions != nil {
		permBytes, err := json.Marshal(session.Permissions)
		if err != nil {
			return err
		}
		permissionsJSON = string(permBytes)
	}

	// Marshal agent model overrides (default to empty object if nil)
	agentModelOverridesJSON := "{}"
	if len(session.AgentModelOverrides) > 0 {
		overridesBytes, err := json.Marshal(session.AgentModelOverrides)
		if err != nil {
			return err
		}
		agentModelOverridesJSON = string(overridesBytes)
	}

	// Marshal custom models used (default to empty array if nil)
	customModelsUsedJSON := "[]"
	if len(session.CustomModelsUsed) > 0 {
		customBytes, err := json.Marshal(session.CustomModelsUsed)
		if err != nil {
			return err
		}
		customModelsUsedJSON = string(customBytes)
	}

	// Convert empty ParentID to nil for proper NULL storage
	var parentID *string
	if session.ParentID != "" {
		parentID = &session.ParentID
	}

	// Use INSERT OR REPLACE for upsert behavior - creates if not exists, updates if exists
	// Note: messages column is kept empty for backward compatibility
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, messages, tools_approved, input_tokens, output_tokens, title, cost, send_user_message, max_iterations, working_dir, created_at, starred, permissions, agent_model_overrides, custom_models_used, thinking, parent_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   title = excluded.title,
		   tools_approved = excluded.tools_approved,
		   input_tokens = excluded.input_tokens,
		   output_tokens = excluded.output_tokens,
		   cost = excluded.cost,
		   send_user_message = excluded.send_user_message,
		   max_iterations = excluded.max_iterations,
		   working_dir = excluded.working_dir,
		   starred = excluded.starred,
		   permissions = excluded.permissions,
		   agent_model_overrides = excluded.agent_model_overrides,
		   custom_models_used = excluded.custom_models_used,
		   thinking = excluded.thinking,
		   parent_id = excluded.parent_id`,
		session.ID, "[]", session.ToolsApproved, session.InputTokens, session.OutputTokens,
		session.Title, session.Cost, session.SendUserMessage, session.MaxIterations, session.WorkingDir,
		session.CreatedAt.Format(time.RFC3339), session.Starred, permissionsJSON, agentModelOverridesJSON, customModelsUsedJSON, session.Thinking, parentID)

	return err
}

// SetSessionStarred sets the starred status of a session.
func (s *SQLiteSessionStore) SetSessionStarred(ctx context.Context, id string, starred bool) error {
	if id == "" {
		return ErrEmptyID
	}

	result, err := s.db.ExecContext(ctx, "UPDATE sessions SET starred = ? WHERE id = ?", starred, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

// Close closes the database connection
func (s *SQLiteSessionStore) Close() error {
	return s.db.Close()
}

// AddMessage adds a message to a session and returns the message ID.
func (s *SQLiteSessionStore) AddMessage(ctx context.Context, sessionID string, item *Item) (string, error) {
	if sessionID == "" {
		return "", ErrEmptyID
	}

	messageID := uuid.New().String()
	item.ID = messageID

	// If this is a sub-session, store the sub-session separately and only keep a reference
	var itemToStore Item
	if item.SubSession != nil {
		// First, add the sub-session to the sessions table
		if err := s.AddSession(ctx, item.SubSession); err != nil {
			return "", err
		}
		// Store only the reference in the messages table
		itemToStore = Item{
			ID:           messageID,
			SubSessionID: item.SubSession.ID,
		}
	} else {
		itemToStore = *item
	}

	itemJSON, err := json.Marshal(itemToStore)
	if err != nil {
		return "", err
	}

	// Get the next position for this session
	var position int
	err = s.db.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(position), -1) + 1 FROM messages WHERE session_id = ?", sessionID).Scan(&position)
	if err != nil {
		return "", err
	}

	_, err = s.db.ExecContext(ctx,
		"INSERT INTO messages (id, session_id, position, data) VALUES (?, ?, ?, ?)",
		messageID, sessionID, position, string(itemJSON))
	if err != nil {
		return "", err
	}

	item.ID = messageID

	return messageID, nil
}

// EditMessage updates an existing message by ID.
func (s *SQLiteSessionStore) EditMessage(ctx context.Context, sessionID, messageID string, item *Item) error {
	if sessionID == "" || messageID == "" {
		return ErrEmptyID
	}

	item.ID = messageID

	itemJSON, err := json.Marshal(item)
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx,
		"UPDATE messages SET data = ? WHERE id = ? AND session_id = ?",
		string(itemJSON), messageID, sessionID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}
