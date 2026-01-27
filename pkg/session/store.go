package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/concurrent"
	"github.com/docker/cagent/pkg/sqliteutil"
)

// generateID generates a unique ID for messages and other entities.
func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

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

// convertMessagesToItems converts a slice of Messages to SessionItems for backward compatibility
func convertMessagesToItems(messages []Message) []Item {
	items := make([]Item, len(messages))
	for i := range messages {
		items[i] = NewMessageItem(&messages[i])
	}
	return items
}

// Store defines the interface for session storage
type Store interface {
	// Session metadata operations
	AddSession(ctx context.Context, session *Session) error
	GetSession(ctx context.Context, id string) (*Session, error)
	GetSessions(ctx context.Context) ([]*Session, error)
	GetSessionSummaries(ctx context.Context) ([]Summary, error)
	DeleteSession(ctx context.Context, id string) error
	UpdateSession(ctx context.Context, session *Session) error
	SetSessionStarred(ctx context.Context, id string, starred bool) error

	// Message operations (for normalized storage)
	AddMessage(ctx context.Context, sessionID string, msg *Message) (msgID string, err error)
	UpdateMessage(ctx context.Context, msgID string, msg *Message) error

	// Sub-session operations
	AddSubSession(ctx context.Context, parentSessionID string, subSession *Session) error

	// Summary operations
	AddSummary(ctx context.Context, sessionID, summary string) error
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

// AddMessage adds a message to a session in the in-memory store.
func (s *InMemorySessionStore) AddMessage(_ context.Context, sessionID string, msg *Message) (string, error) {
	if sessionID == "" {
		return "", ErrEmptyID
	}
	session, exists := s.sessions.Load(sessionID)
	if !exists {
		return "", ErrNotFound
	}
	msgID := generateID()
	session.Messages = append(session.Messages, NewMessageItem(msg))
	s.sessions.Store(sessionID, session)
	return msgID, nil
}

// UpdateMessage updates a message in the in-memory store by finding the last message.
func (s *InMemorySessionStore) UpdateMessage(_ context.Context, _ string, msg *Message) error {
	// For in-memory store, we find the session containing this message and update it
	// Since messages are stored by reference, we just update the last message
	// This is a simplified implementation - the actual message content is updated in place
	var found bool
	s.sessions.Range(func(_ string, session *Session) bool {
		for i := len(session.Messages) - 1; i >= 0; i-- {
			if session.Messages[i].IsMessage() {
				session.Messages[i].Message = msg
				found = true
				return false // stop iterating
			}
		}
		return true
	})
	if !found {
		return ErrNotFound
	}
	return nil
}

// AddSubSession adds a sub-session marker and stores the sub-session.
func (s *InMemorySessionStore) AddSubSession(_ context.Context, parentSessionID string, subSession *Session) error {
	if parentSessionID == "" {
		return ErrEmptyID
	}
	session, exists := s.sessions.Load(parentSessionID)
	if !exists {
		return ErrNotFound
	}
	// Add sub-session marker to parent
	session.Messages = append(session.Messages, NewSubSessionItem(subSession))
	s.sessions.Store(parentSessionID, session)
	// Also store the sub-session itself
	s.sessions.Store(subSession.ID, subSession)
	return nil
}

// AddSummary adds a summary to a session.
func (s *InMemorySessionStore) AddSummary(_ context.Context, sessionID, summary string) error {
	if sessionID == "" {
		return ErrEmptyID
	}
	session, exists := s.sessions.Load(sessionID)
	if !exists {
		return ErrNotFound
	}
	session.Messages = append(session.Messages, Item{Summary: summary})
	s.sessions.Store(sessionID, session)
	return nil
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

	itemsJSON, err := json.Marshal(session.Messages)
	if err != nil {
		return err
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

	_, err = s.db.ExecContext(ctx,
		"INSERT INTO sessions (id, messages, tools_approved, input_tokens, output_tokens, title, send_user_message, max_iterations, working_dir, created_at, permissions, agent_model_overrides, custom_models_used, thinking) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		session.ID, string(itemsJSON), session.ToolsApproved, session.InputTokens, session.OutputTokens, session.Title, session.SendUserMessage, session.MaxIterations, session.WorkingDir, session.CreatedAt.Format(time.RFC3339), permissionsJSON, agentModelOverridesJSON, customModelsUsedJSON, session.Thinking)
	return err
}

// scanSession scans a single row into a Session struct
func scanSession(scanner interface {
	Scan(dest ...any) error
},
) (*Session, error) {
	var messagesJSON, toolsApprovedStr, inputTokensStr, outputTokensStr, titleStr, costStr, sendUserMessageStr, maxIterationsStr, createdAtStr, starredStr, agentModelOverridesJSON, customModelsUsedJSON, thinkingStr string
	var sessionID string
	var workingDir sql.NullString
	var permissionsJSON sql.NullString

	err := scanner.Scan(&sessionID, &messagesJSON, &toolsApprovedStr, &inputTokensStr, &outputTokensStr, &titleStr, &costStr, &sendUserMessageStr, &maxIterationsStr, &workingDir, &createdAtStr, &starredStr, &permissionsJSON, &agentModelOverridesJSON, &customModelsUsedJSON, &thinkingStr)
	if err != nil {
		return nil, err
	}

	// Ok listen up, we used to only store messages in the database, but now we
	// store messages and sub-sessions. So we need to handle both cases.
	// Legacy format has Message structs directly, new format has Item wrappers.
	// When unmarshaling new format into []Message, we get empty structs.
	// We detect legacy format by checking if the first message has actual content.
	var items []Item
	var messages []Message
	if err := json.Unmarshal([]byte(messagesJSON), &messages); err != nil {
		return nil, err
	}
	// Check if this is legacy format by seeing if we got actual message content
	isLegacyFormat := len(messages) > 0 && (messages[0].AgentName != "" || messages[0].Message.Content != "" || messages[0].Message.Role != "")
	if isLegacyFormat {
		// Legacy format: messages were successfully parsed, convert them to items
		items = convertMessagesToItems(messages)
	} else {
		// New format: unmarshal directly as items
		if err := json.Unmarshal([]byte(messagesJSON), &items); err != nil {
			return nil, err
		}
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
		Messages:            items,
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
	}, nil
}

// GetSession retrieves a session by ID, reconstructing messages from normalized tables.
func (s *SQLiteSessionStore) GetSession(ctx context.Context, id string) (*Session, error) {
	if id == "" {
		return nil, ErrEmptyID
	}

	// First get the session metadata
	row := s.db.QueryRowContext(ctx,
		"SELECT id, messages, tools_approved, input_tokens, output_tokens, title, cost, send_user_message, max_iterations, working_dir, created_at, starred, permissions, agent_model_overrides, custom_models_used, thinking FROM sessions WHERE id = ?", id)

	session, err := scanSession(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	// Now reconstruct messages from normalized tables
	items, err := s.loadSessionItems(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("loading session items: %w", err)
	}

	// If we have items from normalized tables, use those; otherwise keep the legacy JSON blob
	if len(items) > 0 {
		session.Messages = items
	}

	return session, nil
}

// loadSessionItems loads all items (messages, sub-sessions, summaries) for a session and reconstructs the Items slice.
func (s *SQLiteSessionStore) loadSessionItems(ctx context.Context, sessionID string) ([]Item, error) {
	var allItems []positionedItem

	// Load messages
	msgRows, err := s.db.QueryContext(ctx,
		`SELECT position, agent_name, message, implicit FROM session_messages WHERE session_id = ? ORDER BY position`,
		sessionID)
	if err != nil {
		return nil, fmt.Errorf("querying messages: %w", err)
	}
	defer msgRows.Close()

	for msgRows.Next() {
		var position int
		var agentName sql.NullString
		var messageJSON string
		var implicit bool

		if err := msgRows.Scan(&position, &agentName, &messageJSON, &implicit); err != nil {
			return nil, fmt.Errorf("scanning message row: %w", err)
		}

		var chatMsg chat.Message
		if err := json.Unmarshal([]byte(messageJSON), &chatMsg); err != nil {
			return nil, fmt.Errorf("unmarshaling message: %w", err)
		}

		msg := &Message{
			AgentName: agentName.String,
			Message:   chatMsg,
			Implicit:  implicit,
		}

		allItems = append(allItems, positionedItem{
			position: position,
			item:     NewMessageItem(msg),
		})
	}
	if err := msgRows.Err(); err != nil {
		return nil, fmt.Errorf("iterating message rows: %w", err)
	}

	// Load sub-session markers - collect IDs first, then load sessions
	// (to avoid nested queries that may cause SQLite locking issues)
	type subSessionRef struct {
		id       string
		position int
	}
	var subSessionRefs []subSessionRef

	subRows, err := s.db.QueryContext(ctx,
		`SELECT id, position FROM session_subsessions WHERE parent_session_id = ? ORDER BY position`,
		sessionID)
	if err != nil {
		return nil, fmt.Errorf("querying sub-sessions: %w", err)
	}

	for subRows.Next() {
		var subSessionID string
		var position int

		if err := subRows.Scan(&subSessionID, &position); err != nil {
			subRows.Close()
			return nil, fmt.Errorf("scanning sub-session row: %w", err)
		}
		subSessionRefs = append(subSessionRefs, subSessionRef{id: subSessionID, position: position})
	}
	if err := subRows.Err(); err != nil {
		subRows.Close()
		return nil, fmt.Errorf("iterating sub-session rows: %w", err)
	}
	subRows.Close()

	// Now load each sub-session (after closing the rows iterator)
	for _, ref := range subSessionRefs {
		subSession, err := s.GetSession(ctx, ref.id)
		if err != nil {
			return nil, fmt.Errorf("loading sub-session %s: %w", ref.id, err)
		}

		allItems = append(allItems, positionedItem{
			position: ref.position,
			item:     NewSubSessionItem(subSession),
		})
	}

	// Load summaries
	sumRows, err := s.db.QueryContext(ctx,
		`SELECT position, summary FROM session_summaries WHERE session_id = ? ORDER BY position`,
		sessionID)
	if err != nil {
		return nil, fmt.Errorf("querying summaries: %w", err)
	}
	defer sumRows.Close()

	for sumRows.Next() {
		var position int
		var summary string

		if err := sumRows.Scan(&position, &summary); err != nil {
			return nil, fmt.Errorf("scanning summary row: %w", err)
		}

		allItems = append(allItems, positionedItem{
			position: position,
			item:     Item{Summary: summary},
		})
	}
	if err := sumRows.Err(); err != nil {
		return nil, fmt.Errorf("iterating summary rows: %w", err)
	}

	// Sort by position and extract items
	sortPositionedItems(allItems)

	items := make([]Item, len(allItems))
	for i, pi := range allItems {
		items[i] = pi.item
	}

	return items, nil
}

// positionedItem holds an item with its position for sorting
type positionedItem struct {
	position int
	item     Item
}

// sortPositionedItems sorts items by position using insertion sort (stable for small slices)
func sortPositionedItems(items []positionedItem) {
	for i := 1; i < len(items); i++ {
		j := i
		for j > 0 && items[j-1].position > items[j].position {
			items[j-1], items[j] = items[j], items[j-1]
			j--
		}
	}
}

// GetSessions retrieves all sessions
func (s *SQLiteSessionStore) GetSessions(ctx context.Context) ([]*Session, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, messages, tools_approved, input_tokens, output_tokens, title, cost, send_user_message, max_iterations, working_dir, created_at, starred, permissions, agent_model_overrides, custom_models_used, thinking FROM sessions ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}

	return sessions, nil
}

// GetSessionSummaries retrieves lightweight session metadata for listing.
// This is much faster than GetSessions as it doesn't load message content.
func (s *SQLiteSessionStore) GetSessionSummaries(ctx context.Context) ([]Summary, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, title, created_at, starred FROM sessions ORDER BY created_at DESC")
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

// UpdateSession updates session metadata only (not messages).
// Messages are stored separately via AddMessage. This enables lazy session persistence.
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

	// Use INSERT OR REPLACE for upsert behavior - creates if not exists, updates if exists
	// Messages column is set to empty array - messages are stored in session_messages table
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, messages, tools_approved, input_tokens, output_tokens, title, cost, send_user_message, max_iterations, working_dir, created_at, starred, permissions, agent_model_overrides, custom_models_used, thinking)
		 VALUES (?, '[]', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
		   thinking = excluded.thinking`,
		session.ID, session.ToolsApproved, session.InputTokens, session.OutputTokens,
		session.Title, session.Cost, session.SendUserMessage, session.MaxIterations, session.WorkingDir,
		session.CreatedAt.Format(time.RFC3339), session.Starred, permissionsJSON, agentModelOverridesJSON, customModelsUsedJSON, session.Thinking)
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

// AddMessage adds a message to the normalized session_messages table.
func (s *SQLiteSessionStore) AddMessage(ctx context.Context, sessionID string, msg *Message) (string, error) {
	if sessionID == "" {
		return "", ErrEmptyID
	}

	// Get the next position for this session
	var position int
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(position), -1) + 1 FROM (
			SELECT position FROM session_messages WHERE session_id = ?
			UNION ALL
			SELECT position FROM session_subsessions WHERE parent_session_id = ?
			UNION ALL
			SELECT position FROM session_summaries WHERE session_id = ?
		)`,
		sessionID, sessionID, sessionID).Scan(&position)
	if err != nil {
		return "", fmt.Errorf("getting next position: %w", err)
	}

	msgJSON, err := json.Marshal(msg.Message)
	if err != nil {
		return "", fmt.Errorf("marshaling message: %w", err)
	}

	msgID := generateID()
	createdAt := msg.Message.CreatedAt
	if createdAt == "" {
		createdAt = time.Now().Format(time.RFC3339)
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO session_messages (id, session_id, position, agent_name, message, implicit, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		msgID, sessionID, position, msg.AgentName, string(msgJSON), msg.Implicit, createdAt)
	if err != nil {
		return "", fmt.Errorf("inserting message: %w", err)
	}

	return msgID, nil
}

// UpdateMessage updates an existing message in the session_messages table.
func (s *SQLiteSessionStore) UpdateMessage(ctx context.Context, msgID string, msg *Message) error {
	if msgID == "" {
		return ErrEmptyID
	}

	msgJSON, err := json.Marshal(msg.Message)
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}

	result, err := s.db.ExecContext(ctx,
		`UPDATE session_messages SET agent_name = ?, message = ?, implicit = ? WHERE id = ?`,
		msg.AgentName, string(msgJSON), msg.Implicit, msgID)
	if err != nil {
		return fmt.Errorf("updating message: %w", err)
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

// AddSubSession adds a sub-session marker and stores the sub-session's data.
func (s *SQLiteSessionStore) AddSubSession(ctx context.Context, parentSessionID string, subSession *Session) error {
	if parentSessionID == "" || subSession.ID == "" {
		return ErrEmptyID
	}

	// Get the next position in the parent session
	var position int
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(position), -1) + 1 FROM (
			SELECT position FROM session_messages WHERE session_id = ?
			UNION ALL
			SELECT position FROM session_subsessions WHERE parent_session_id = ?
			UNION ALL
			SELECT position FROM session_summaries WHERE session_id = ?
		)`,
		parentSessionID, parentSessionID, parentSessionID).Scan(&position)
	if err != nil {
		return fmt.Errorf("getting next position: %w", err)
	}

	// Start a transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Insert the sub-session as a session record (metadata only, empty messages)
	permissionsJSON := ""
	if subSession.Permissions != nil {
		permBytes, err := json.Marshal(subSession.Permissions)
		if err != nil {
			return err
		}
		permissionsJSON = string(permBytes)
	}

	agentModelOverridesJSON := "{}"
	if len(subSession.AgentModelOverrides) > 0 {
		overridesBytes, err := json.Marshal(subSession.AgentModelOverrides)
		if err != nil {
			return err
		}
		agentModelOverridesJSON = string(overridesBytes)
	}

	customModelsUsedJSON := "[]"
	if len(subSession.CustomModelsUsed) > 0 {
		customBytes, err := json.Marshal(subSession.CustomModelsUsed)
		if err != nil {
			return err
		}
		customModelsUsedJSON = string(customBytes)
	}

	_, err = tx.ExecContext(ctx,
		`INSERT OR REPLACE INTO sessions (id, messages, tools_approved, input_tokens, output_tokens, title, cost, send_user_message, max_iterations, working_dir, created_at, starred, permissions, agent_model_overrides, custom_models_used, thinking)
		 VALUES (?, '[]', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		subSession.ID, subSession.ToolsApproved, subSession.InputTokens, subSession.OutputTokens,
		subSession.Title, subSession.Cost, subSession.SendUserMessage, subSession.MaxIterations, subSession.WorkingDir,
		subSession.CreatedAt.Format(time.RFC3339), subSession.Starred, permissionsJSON, agentModelOverridesJSON, customModelsUsedJSON, subSession.Thinking)
	if err != nil {
		return fmt.Errorf("inserting sub-session: %w", err)
	}

	// Insert the sub-session marker in the parent
	// Use INSERT OR IGNORE in case the sub-session marker already exists
	_, err = tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO session_subsessions (id, parent_session_id, position, created_at)
		 VALUES (?, ?, ?, ?)`,
		subSession.ID, parentSessionID, position, subSession.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("inserting sub-session marker: %w", err)
	}

	// Check if the sub-session already has messages in the database
	// (they may have been added during RunStream via AddMessage calls)
	var existingCount int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM session_messages WHERE session_id = ?`, subSession.ID).Scan(&existingCount)
	if err != nil {
		return fmt.Errorf("checking existing messages: %w", err)
	}

	// Only insert messages from the Messages slice if no messages exist yet
	// This handles cases where messages are passed in-memory (e.g., tests, offline storage)
	if existingCount == 0 {
		for i, item := range subSession.Messages {
			if item.IsMessage() && item.Message != nil {
				msgJSON, err := json.Marshal(item.Message.Message)
				if err != nil {
					return fmt.Errorf("marshaling sub-session message: %w", err)
				}

				msgID := generateID()
				createdAt := item.Message.Message.CreatedAt
				if createdAt == "" {
					createdAt = time.Now().Format(time.RFC3339)
				}

				_, err = tx.ExecContext(ctx,
					`INSERT INTO session_messages (id, session_id, position, agent_name, message, implicit, created_at)
					 VALUES (?, ?, ?, ?, ?, ?, ?)`,
					msgID, subSession.ID, i, item.Message.AgentName, string(msgJSON), item.Message.Implicit, createdAt)
				if err != nil {
					return fmt.Errorf("inserting sub-session message: %w", err)
				}
			} else if item.IsSummary() {
				summaryID := generateID()
				_, err = tx.ExecContext(ctx,
					`INSERT INTO session_summaries (id, session_id, position, summary, created_at)
					 VALUES (?, ?, ?, ?, ?)`,
					summaryID, subSession.ID, i, item.Summary, time.Now().Format(time.RFC3339))
				if err != nil {
					return fmt.Errorf("inserting sub-session summary: %w", err)
				}
			}
		}
	}

	return tx.Commit()
}

// AddSummary adds a summary to the session_summaries table.
func (s *SQLiteSessionStore) AddSummary(ctx context.Context, sessionID, summary string) error {
	if sessionID == "" {
		return ErrEmptyID
	}

	// Get the next position for this session
	var position int
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(position), -1) + 1 FROM (
			SELECT position FROM session_messages WHERE session_id = ?
			UNION ALL
			SELECT position FROM session_subsessions WHERE parent_session_id = ?
			UNION ALL
			SELECT position FROM session_summaries WHERE session_id = ?
		)`,
		sessionID, sessionID, sessionID).Scan(&position)
	if err != nil {
		return fmt.Errorf("getting next position: %w", err)
	}

	summaryID := generateID()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO session_summaries (id, session_id, position, summary, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		summaryID, sessionID, position, summary, time.Now().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("inserting summary: %w", err)
	}

	return nil
}
