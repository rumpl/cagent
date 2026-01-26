package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/docker/cagent/pkg/concurrent"
	"github.com/docker/cagent/pkg/sqliteutil"
)

// generateItemID generates a unique ID for session items
func generateItemID() string {
	return uuid.New().String()
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

// ItemType represents the type of session item
type ItemType string

const (
	ItemTypeMessage    ItemType = "message"
	ItemTypeSubSession ItemType = "sub_session"
	ItemTypeSummary    ItemType = "summary"
)

// Store defines the interface for session storage
type Store interface {
	// Session CRUD operations
	AddSession(ctx context.Context, session *Session) error
	GetSession(ctx context.Context, id string) (*Session, error)
	GetSessions(ctx context.Context) ([]*Session, error)
	GetSessionSummaries(ctx context.Context) ([]Summary, error)
	DeleteSession(ctx context.Context, id string) error
	// UpdateSession updates only session metadata (not items/messages).
	// Use AddItem/UpdateItem for managing session content.
	UpdateSession(ctx context.Context, session *Session) error
	SetSessionStarred(ctx context.Context, id string, starred bool) error

	// Item operations for messages, sub-sessions, and summaries
	// AddItem adds a new item (message, sub-session reference, or summary) to the session.
	// Returns the generated item ID.
	AddItem(ctx context.Context, sessionID string, item *Item) (string, error)
	// UpdateItem updates an existing item by ID.
	UpdateItem(ctx context.Context, itemID string, item *Item) error
	// GetItems retrieves all items for a session, ordered by position.
	GetItems(ctx context.Context, sessionID string) ([]Item, error)
	// AddSubSession adds a sub-session and creates a reference item in the parent session.
	// Returns the sub-session ID.
	AddSubSession(ctx context.Context, parentSessionID string, subSession *Session) (string, error)
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

// AddItem adds a new item to the session.
func (s *InMemorySessionStore) AddItem(_ context.Context, sessionID string, item *Item) (string, error) {
	if sessionID == "" {
		return "", ErrEmptyID
	}
	session, exists := s.sessions.Load(sessionID)
	if !exists {
		return "", ErrNotFound
	}

	// Generate a unique ID for the item
	itemID := generateItemID()
	item.ID = itemID
	item.Position = len(session.Messages)

	session.Messages = append(session.Messages, *item)
	s.sessions.Store(sessionID, session)
	return itemID, nil
}

// UpdateItem updates an existing item by ID.
func (s *InMemorySessionStore) UpdateItem(_ context.Context, itemID string, item *Item) error {
	if itemID == "" {
		return ErrEmptyID
	}

	// Search through all sessions to find the item
	var foundSessionID string
	foundIndex := -1
	var foundPosition int
	s.sessions.Range(func(sessionID string, session *Session) bool {
		for i, existingItem := range session.Messages {
			if existingItem.ID == itemID {
				foundSessionID = sessionID
				foundIndex = i
				foundPosition = existingItem.Position
				return false // Stop iteration
			}
		}
		return true // Continue iteration
	})

	if foundIndex == -1 {
		return ErrNotFound
	}

	// Update outside the Range loop to avoid deadlock
	session, exists := s.sessions.Load(foundSessionID)
	if !exists {
		return ErrNotFound
	}
	item.ID = itemID
	item.Position = foundPosition
	session.Messages[foundIndex] = *item
	s.sessions.Store(foundSessionID, session)

	return nil
}

// GetItems retrieves all items for a session, ordered by position.
func (s *InMemorySessionStore) GetItems(_ context.Context, sessionID string) ([]Item, error) {
	if sessionID == "" {
		return nil, ErrEmptyID
	}
	session, exists := s.sessions.Load(sessionID)
	if !exists {
		return nil, ErrNotFound
	}
	// Return a copy to avoid external modifications
	items := make([]Item, len(session.Messages))
	copy(items, session.Messages)
	return items, nil
}

// AddSubSession adds a sub-session and creates a reference item in the parent session.
func (s *InMemorySessionStore) AddSubSession(_ context.Context, parentSessionID string, subSession *Session) (string, error) {
	if parentSessionID == "" {
		return "", ErrEmptyID
	}
	parentSession, exists := s.sessions.Load(parentSessionID)
	if !exists {
		return "", ErrNotFound
	}

	// Store the sub-session
	if subSession.ID == "" {
		subSession.ID = generateItemID()
	}
	subSession.ParentID = parentSessionID
	s.sessions.Store(subSession.ID, subSession)

	// Create a reference item in the parent session
	item := Item{
		ID:           generateItemID(),
		Position:     len(parentSession.Messages),
		SubSessionID: subSession.ID,
		SubSession:   subSession,
	}
	parentSession.Messages = append(parentSession.Messages, item)
	s.sessions.Store(parentSessionID, parentSession)

	return subSession.ID, nil
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

// AddSession adds a new session to the store.
// Note: This only stores session metadata. Messages should be added via AddItem.
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

	_, err := s.db.ExecContext(ctx,
		"INSERT INTO sessions (id, tools_approved, input_tokens, output_tokens, title, send_user_message, max_iterations, working_dir, created_at, permissions, agent_model_overrides, custom_models_used, thinking, parent_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		session.ID, session.ToolsApproved, session.InputTokens, session.OutputTokens, session.Title, session.SendUserMessage, session.MaxIterations, session.WorkingDir, session.CreatedAt.Format(time.RFC3339), permissionsJSON, agentModelOverridesJSON, customModelsUsedJSON, session.Thinking, session.ParentID)
	return err
}

// scanSession scans a single row into a Session struct
func scanSession(scanner interface {
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
		Messages:            nil, // Messages are loaded from session_items table separately
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

// GetSession retrieves a session by ID.
// It loads session metadata and items from the session_items table.
func (s *SQLiteSessionStore) GetSession(ctx context.Context, id string) (*Session, error) {
	if id == "" {
		return nil, ErrEmptyID
	}

	// Load session metadata
	row := s.db.QueryRowContext(ctx,
		"SELECT id, tools_approved, input_tokens, output_tokens, title, cost, send_user_message, max_iterations, working_dir, created_at, starred, permissions, agent_model_overrides, custom_models_used, thinking, parent_id FROM sessions WHERE id = ?", id)

	sess, err := scanSession(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	// Load items from session_items table
	items, err := s.GetItems(ctx, id)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("loading session items: %w", err)
	}
	sess.Messages = items

	return sess, nil
}

// GetSessions retrieves all sessions.
// For each session, it loads items from session_items table.
func (s *SQLiteSessionStore) GetSessions(ctx context.Context) ([]*Session, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, tools_approved, input_tokens, output_tokens, title, cost, send_user_message, max_iterations, working_dir, created_at, starred, permissions, agent_model_overrides, custom_models_used, thinking, parent_id FROM sessions WHERE parent_id = '' ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}

	// Load items from session_items table for each session
	for _, sess := range sessions {
		items, err := s.GetItems(ctx, sess.ID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("loading items for session %s: %w", sess.ID, err)
		}
		sess.Messages = items
	}

	return sessions, nil
}

// GetSessionSummaries retrieves lightweight session metadata for listing.
// This is much faster than GetSessions as it doesn't load message content.
func (s *SQLiteSessionStore) GetSessionSummaries(ctx context.Context) ([]Summary, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, title, created_at, starred FROM sessions WHERE parent_id = '' ORDER BY created_at DESC")
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
// This enables lazy session persistence - sessions are only stored when they have content.
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
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, tools_approved, input_tokens, output_tokens, title, cost, send_user_message, max_iterations, working_dir, created_at, starred, permissions, agent_model_overrides, custom_models_used, thinking, parent_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
		session.ID, session.ToolsApproved, session.InputTokens, session.OutputTokens,
		session.Title, session.Cost, session.SendUserMessage, session.MaxIterations, session.WorkingDir,
		session.CreatedAt.Format(time.RFC3339), session.Starred, permissionsJSON, agentModelOverridesJSON, customModelsUsedJSON, session.Thinking, session.ParentID)
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

// AddItem adds a new item to the session.
// If the session doesn't exist in the database, it creates a minimal session record first.
func (s *SQLiteSessionStore) AddItem(ctx context.Context, sessionID string, item *Item) (string, error) {
	if sessionID == "" {
		return "", ErrEmptyID
	}

	// Ensure the session exists in the database (lazy creation)
	// We use INSERT OR IGNORE to create a minimal session record if it doesn't exist
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO sessions (id, tools_approved, input_tokens, output_tokens, title, cost, send_user_message, max_iterations, working_dir, created_at, starred, permissions, agent_model_overrides, custom_models_used, thinking, parent_id)
		 VALUES (?, 0, 0, 0, '', 0, 1, 0, '', ?, 0, '', '{}', '[]', 0, '')`,
		sessionID, time.Now().Format(time.RFC3339))
	if err != nil {
		return "", fmt.Errorf("ensuring session exists: %w", err)
	}

	// Generate a unique ID for the item
	itemID := generateItemID()
	item.ID = itemID

	// Get the next position for this session
	var maxPos sql.NullInt64
	err = s.db.QueryRowContext(ctx, "SELECT MAX(position) FROM session_items WHERE session_id = ?", sessionID).Scan(&maxPos)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	position := 0
	if maxPos.Valid {
		position = int(maxPos.Int64) + 1
	}
	item.Position = position

	// Determine item type and prepare data
	itemType, messageData, subSessionID, summary := serializeItem(item)

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO session_items (id, session_id, position, item_type, message_data, sub_session_id, summary, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		itemID, sessionID, position, string(itemType), messageData, subSessionID, summary, time.Now().Format(time.RFC3339))
	if err != nil {
		return "", err
	}

	return itemID, nil
}

// UpdateItem updates an existing item by ID.
func (s *SQLiteSessionStore) UpdateItem(ctx context.Context, itemID string, item *Item) error {
	if itemID == "" {
		return ErrEmptyID
	}

	// Determine item type and prepare data
	itemType, messageData, subSessionID, summary := serializeItem(item)

	result, err := s.db.ExecContext(ctx,
		`UPDATE session_items SET item_type = ?, message_data = ?, sub_session_id = ?, summary = ?
		 WHERE id = ?`,
		string(itemType), messageData, subSessionID, summary, itemID)
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

// GetItems retrieves all items for a session, ordered by position.
func (s *SQLiteSessionStore) GetItems(ctx context.Context, sessionID string) ([]Item, error) {
	if sessionID == "" {
		return nil, ErrEmptyID
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, position, item_type, message_data, sub_session_id, summary
		 FROM session_items
		 WHERE session_id = ?
		 ORDER BY position ASC`, sessionID)
	if err != nil {
		return nil, err
	}

	// Collect items and sub-session IDs to load
	type itemWithSubSessID struct {
		item      Item
		subSessID string
		index     int
	}
	var items []Item
	var subSessItems []itemWithSubSessID

	for rows.Next() {
		var id string
		var position int
		var itemType string
		var messageData, subSessionID, summary sql.NullString

		if err := rows.Scan(&id, &position, &itemType, &messageData, &subSessionID, &summary); err != nil {
			rows.Close()
			return nil, err
		}

		item := Item{
			ID:       id,
			Position: position,
		}

		switch ItemType(itemType) {
		case ItemTypeMessage:
			if messageData.Valid && messageData.String != "" {
				var msg Message
				if err := json.Unmarshal([]byte(messageData.String), &msg); err != nil {
					rows.Close()
					return nil, err
				}
				item.Message = &msg
			}
		case ItemTypeSubSession:
			if subSessionID.Valid {
				item.SubSessionID = subSessionID.String
				// Store for later loading after rows are closed
				subSessItems = append(subSessItems, itemWithSubSessID{
					item:      item,
					subSessID: subSessionID.String,
					index:     len(items),
				})
			}
		case ItemTypeSummary:
			if summary.Valid {
				item.Summary = summary.String
			}
		}

		items = append(items, item)
	}
	rows.Close()

	// Now load sub-sessions after closing the rows
	for _, ssi := range subSessItems {
		subSess, err := s.GetSession(ctx, ssi.subSessID)
		if err == nil {
			items[ssi.index].SubSession = subSess
		}
	}

	return items, nil
}

// AddSubSession adds a sub-session and creates a reference item in the parent session.
func (s *SQLiteSessionStore) AddSubSession(ctx context.Context, parentSessionID string, subSession *Session) (string, error) {
	if parentSessionID == "" {
		return "", ErrEmptyID
	}

	// Ensure the sub-session has an ID
	if subSession.ID == "" {
		subSession.ID = generateItemID()
	}
	subSession.ParentID = parentSessionID

	// Start a transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	// Ensure the parent session exists in the database (lazy creation)
	_, err = tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO sessions (id, tools_approved, input_tokens, output_tokens, title, cost, send_user_message, max_iterations, working_dir, created_at, starred, permissions, agent_model_overrides, custom_models_used, thinking, parent_id)
		 VALUES (?, 0, 0, 0, '', 0, 1, 0, '', ?, 0, '', '{}', '[]', 0, '')`,
		parentSessionID, time.Now().Format(time.RFC3339))
	if err != nil {
		return "", fmt.Errorf("ensuring parent session exists: %w", err)
	}

	// Add the sub-session to the sessions table
	if err := s.addSessionInTx(ctx, tx, subSession); err != nil {
		return "", err
	}

	// Get the next position for the parent session
	var maxPos sql.NullInt64
	err = tx.QueryRowContext(ctx, "SELECT MAX(position) FROM session_items WHERE session_id = ?", parentSessionID).Scan(&maxPos)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	position := 0
	if maxPos.Valid {
		position = int(maxPos.Int64) + 1
	}

	// Create a reference item in the parent session
	itemID := generateItemID()
	_, err = tx.ExecContext(ctx,
		`INSERT INTO session_items (id, session_id, position, item_type, sub_session_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		itemID, parentSessionID, position, string(ItemTypeSubSession), subSession.ID, time.Now().Format(time.RFC3339))
	if err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}

	return subSession.ID, nil
}

// addSessionInTx adds a session within a transaction.
// This also adds all messages from sess.Messages to session_items.
func (s *SQLiteSessionStore) addSessionInTx(ctx context.Context, tx *sql.Tx, sess *Session) error {
	permissionsJSON := ""
	if sess.Permissions != nil {
		permBytes, err := json.Marshal(sess.Permissions)
		if err != nil {
			return err
		}
		permissionsJSON = string(permBytes)
	}

	agentModelOverridesJSON := "{}"
	if len(sess.AgentModelOverrides) > 0 {
		overridesBytes, err := json.Marshal(sess.AgentModelOverrides)
		if err != nil {
			return err
		}
		agentModelOverridesJSON = string(overridesBytes)
	}

	customModelsUsedJSON := "[]"
	if len(sess.CustomModelsUsed) > 0 {
		customBytes, err := json.Marshal(sess.CustomModelsUsed)
		if err != nil {
			return err
		}
		customModelsUsedJSON = string(customBytes)
	}

	_, err := tx.ExecContext(ctx,
		"INSERT INTO sessions (id, tools_approved, input_tokens, output_tokens, title, send_user_message, max_iterations, working_dir, created_at, permissions, agent_model_overrides, custom_models_used, thinking, parent_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		sess.ID, sess.ToolsApproved, sess.InputTokens, sess.OutputTokens, sess.Title, sess.SendUserMessage, sess.MaxIterations, sess.WorkingDir, sess.CreatedAt.Format(time.RFC3339), permissionsJSON, agentModelOverridesJSON, customModelsUsedJSON, sess.Thinking, sess.ParentID)
	if err != nil {
		return err
	}

	// Add items to the session_items table
	for i, item := range sess.Messages {
		itemID := item.ID
		if itemID == "" {
			itemID = generateItemID()
		}
		itemType, messageData, subSessionID, summary := serializeItem(&item)
		_, err = tx.ExecContext(ctx,
			`INSERT INTO session_items (id, session_id, position, item_type, message_data, sub_session_id, summary, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			itemID, sess.ID, i, string(itemType), messageData, subSessionID, summary, time.Now().Format(time.RFC3339))
		if err != nil {
			return fmt.Errorf("adding item to session_items: %w", err)
		}
	}

	return nil
}

// serializeItem extracts the item type and data for storage
func serializeItem(item *Item) (ItemType, string, string, string) {
	if item.Message != nil {
		messageData, _ := json.Marshal(item.Message)
		return ItemTypeMessage, string(messageData), "", ""
	}
	if item.SubSession != nil || item.SubSessionID != "" {
		subSessionID := item.SubSessionID
		if item.SubSession != nil {
			subSessionID = item.SubSession.ID
		}
		return ItemTypeSubSession, "", subSessionID, ""
	}
	if item.Summary != "" {
		return ItemTypeSummary, "", "", item.Summary
	}
	// Default to message type with empty data
	return ItemTypeMessage, "", "", ""
}
