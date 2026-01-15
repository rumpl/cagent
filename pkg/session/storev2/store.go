package storev2

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tools"
)

// Store implements session.Store using a normalized SQLite schema.
type Store struct {
	db *sql.DB
}

// New creates a new normalized session store.
func New(path string) (*Store, error) {
	db, err := openDB(path)
	if err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) CreateMessage(ctx context.Context, sess *session.Session, itemOrder int, msg *session.Message) error {
	if sess.ID == "" {
		return session.ErrEmptyID
	}
	if sess.IsSubSession() {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO session_items (session_id, item_order, item_type, sub_session_id, summary_text)
		VALUES (?, ?, 'message', NULL, NULL)`,
		sess.ID, itemOrder)
	if err != nil {
		return err
	}

	sessionItemID, err := result.LastInsertId()
	if err != nil {
		return err
	}

	if err := s.insertMessage(ctx, tx, sessionItemID, msg); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) UpdateMessage(ctx context.Context, sess *session.Session, itemOrder int, msg *session.Message) error {
	if sess.ID == "" {
		return session.ErrEmptyID
	}
	if sess.IsSubSession() {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := s.upsertSession(ctx, tx, sess); err != nil {
		return err
	}

	var messageID int64
	err = tx.QueryRowContext(ctx, `
		SELECT m.id
		FROM session_items si
		JOIN messages m ON m.session_item_id = si.id
		WHERE si.session_id = ? AND si.item_order = ? AND si.item_type = 'message'`,
		sess.ID, itemOrder).Scan(&messageID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return session.ErrNotFound
		}
		return err
	}

	createdAt := msg.Message.CreatedAt
	if createdAt == "" {
		createdAt = time.Now().Format(time.RFC3339)
	}

	var usageInput, usageOutput, usageCachedInput, usageCacheWrite, usageReasoning int64
	if msg.Message.Usage != nil {
		usageInput = msg.Message.Usage.InputTokens
		usageOutput = msg.Message.Usage.OutputTokens
		usageCachedInput = msg.Message.Usage.CachedInputTokens
		usageCacheWrite = msg.Message.Usage.CacheWriteTokens
		usageReasoning = msg.Message.Usage.ReasoningTokens
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE messages
		SET agent_name = ?, role = ?, content = ?, created_at = ?, implicit = ?,
			tool_call_id = ?, model = ?, reasoning_content = ?, thinking_signature = ?,
			thought_signature = ?, message_cost = ?,
			input_tokens = ?, output_tokens = ?, cached_input_tokens = ?, cache_write_tokens = ?, reasoning_tokens = ?
		WHERE id = ?`,
		nullString(msg.AgentName), string(msg.Message.Role), msg.Message.Content, createdAt,
		boolToInt(msg.Implicit), nullString(msg.Message.ToolCallID), nullString(msg.Message.Model),
		nullString(msg.Message.ReasoningContent), nullString(msg.Message.ThinkingSignature),
		msg.Message.ThoughtSignature, msg.Message.Cost,
		usageInput, usageOutput, usageCachedInput, usageCacheWrite, usageReasoning, messageID)
	if err != nil {
		return err
	}

	for i, part := range msg.Message.MultiContent {
		var imageURL, imageDetail *string
		if part.ImageURL != nil {
			imageURL = &part.ImageURL.URL
			if part.ImageURL.Detail != "" {
				detail := string(part.ImageURL.Detail)
				imageDetail = &detail
			}
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO message_parts (message_id, part_order, part_type, text_content, image_url, image_detail)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(message_id, part_order) DO UPDATE SET
				part_type = excluded.part_type,
				text_content = excluded.text_content,
				image_url = excluded.image_url,
				image_detail = excluded.image_detail`,
			messageID, i, string(part.Type), nullString(part.Text), imageURL, imageDetail)
		if err != nil {
			return err
		}
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE message_parts
		SET part_type = 'text', text_content = '', image_url = NULL, image_detail = NULL
		WHERE message_id = ? AND part_order >= ?`,
		messageID, len(msg.Message.MultiContent))
	if err != nil {
		return err
	}

	for i, tc := range msg.Message.ToolCalls {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO tool_calls (id, message_id, call_order, tool_type, function_name, function_arguments)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(message_id, call_order) DO UPDATE SET
				id = excluded.id,
				tool_type = excluded.tool_type,
				function_name = excluded.function_name,
				function_arguments = excluded.function_arguments`,
			tc.ID, messageID, i, string(tc.Type), tc.Function.Name, tc.Function.Arguments)
		if err != nil {
			return err
		}
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE tool_calls
		SET function_name = '', function_arguments = '', tool_type = 'function'
		WHERE message_id = ? AND call_order >= ?`,
		messageID, len(msg.Message.ToolCalls))
	if err != nil {
		return err
	}

	for _, td := range msg.Message.ToolDefinitions {
		toolDefID, err := s.getOrCreateToolDefinition(ctx, tx, &td)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO message_tool_definitions (message_id, tool_definition_id)
			VALUES (?, ?)`,
			messageID, toolDefID)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Store) upsertSession(ctx context.Context, tx *sql.Tx, sess *session.Session) error {
	permissionsJSON := ""
	if sess.Permissions != nil {
		permBytes, err := json.Marshal(sess.Permissions)
		if err != nil {
			return err
		}
		permissionsJSON = string(permBytes)
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO sessions (id, title, working_dir, created_at, starred, tools_approved, send_user_message,
			max_iterations, input_tokens, output_tokens, cost, parent_session_id, parent_item_order, permissions)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, ?)
		ON CONFLICT(id) DO UPDATE SET
			title = excluded.title,
			working_dir = excluded.working_dir,
			starred = excluded.starred,
			tools_approved = excluded.tools_approved,
			send_user_message = excluded.send_user_message,
			max_iterations = excluded.max_iterations,
			input_tokens = excluded.input_tokens,
			output_tokens = excluded.output_tokens,
			cost = excluded.cost,
			permissions = excluded.permissions`,
		sess.ID, sess.Title, sess.WorkingDir, sess.CreatedAt.Format(time.RFC3339),
		boolToInt(sess.Starred), boolToInt(sess.ToolsApproved), boolToInt(sess.SendUserMessage),
		sess.MaxIterations, sess.InputTokens, sess.OutputTokens, sess.Cost, permissionsJSON)
	return err
}

func (s *Store) upsertModelOverrides(ctx context.Context, tx *sql.Tx, sess *session.Session) error {
	// Upsert each model override
	for agentName, modelRef := range sess.AgentModelOverrides {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO session_model_overrides (session_id, agent_name, model_reference)
			VALUES (?, ?, ?)
			ON CONFLICT(session_id, agent_name) DO UPDATE SET
				model_reference = excluded.model_reference`,
			sess.ID, agentName, modelRef)
		if err != nil {
			return err
		}
	}

	// Delete model overrides that are no longer in the session
	if len(sess.AgentModelOverrides) == 0 {
		_, err := tx.ExecContext(ctx, `DELETE FROM session_model_overrides WHERE session_id = ?`, sess.ID)
		return err
	}

	// Build list of agent names to keep
	placeholders := make([]string, 0, len(sess.AgentModelOverrides))
	args := make([]any, 0, len(sess.AgentModelOverrides)+1)
	args = append(args, sess.ID)
	for agentName := range sess.AgentModelOverrides {
		placeholders = append(placeholders, "?")
		args = append(args, agentName)
	}

	query := `DELETE FROM session_model_overrides WHERE session_id = ? AND agent_name NOT IN (` + strings.Join(placeholders, ",") + `)`
	_, err := tx.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) upsertCustomModels(ctx context.Context, tx *sql.Tx, sess *session.Session) error {
	// Insert or ignore each custom model
	for _, modelRef := range sess.CustomModelsUsed {
		_, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO session_custom_models (session_id, model_reference)
			VALUES (?, ?)`,
			sess.ID, modelRef)
		if err != nil {
			return err
		}
	}

	// Delete custom models that are no longer in the session
	if len(sess.CustomModelsUsed) == 0 {
		_, err := tx.ExecContext(ctx, `DELETE FROM session_custom_models WHERE session_id = ?`, sess.ID)
		return err
	}

	// Build list of model references to keep
	placeholders := make([]string, 0, len(sess.CustomModelsUsed))
	args := make([]any, 0, len(sess.CustomModelsUsed)+1)
	args = append(args, sess.ID)
	for _, modelRef := range sess.CustomModelsUsed {
		placeholders = append(placeholders, "?")
		args = append(args, modelRef)
	}

	query := `DELETE FROM session_custom_models WHERE session_id = ? AND model_reference NOT IN (` + strings.Join(placeholders, ",") + `)`
	_, err := tx.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) upsertSessionItems(ctx context.Context, tx *sql.Tx, sess *session.Session) error {
	for i, item := range sess.Messages {
		if err := s.upsertSessionItem(ctx, tx, sess.ID, i, &item); err != nil {
			return err
		}
	}

	// Delete items beyond the current list length
	_, err := tx.ExecContext(ctx, `
		DELETE FROM session_items WHERE session_id = ? AND item_order >= ?`,
		sess.ID, len(sess.Messages))
	return err
}

func (s *Store) upsertSessionItem(ctx context.Context, tx *sql.Tx, sessionID string, order int, item *session.Item) error {
	var itemType string
	var subSessionID *string
	var summaryText *string

	switch {
	case item.IsMessage():
		itemType = "message"
	case item.IsSubSession():
		itemType = "sub_session"
		// Recursively upsert sub-session
		if err := s.upsertSubSession(ctx, tx, item.SubSession, sessionID, order); err != nil {
			return err
		}
		subSessionID = &item.SubSession.ID
	case item.Summary != "":
		itemType = "summary"
		summaryText = &item.Summary
	default:
		return nil // Skip empty items
	}

	// Check if item exists
	var existingItemID int64
	err := tx.QueryRowContext(ctx, `
		SELECT id FROM session_items WHERE session_id = ? AND item_order = ?`,
		sessionID, order).Scan(&existingItemID)

	if errors.Is(err, sql.ErrNoRows) {
		// Insert new item
		result, err := tx.ExecContext(ctx, `
			INSERT INTO session_items (session_id, item_order, item_type, sub_session_id, summary_text)
			VALUES (?, ?, ?, ?, ?)`,
			sessionID, order, itemType, subSessionID, summaryText)
		if err != nil {
			return err
		}

		if item.IsMessage() {
			sessionItemID, err := result.LastInsertId()
			if err != nil {
				return err
			}
			return s.insertMessage(ctx, tx, sessionItemID, item.Message)
		}
		return nil
	}
	if err != nil {
		return err
	}

	// Update existing item
	_, err = tx.ExecContext(ctx, `
		UPDATE session_items SET item_type = ?, sub_session_id = ?, summary_text = ?
		WHERE id = ?`,
		itemType, subSessionID, summaryText, existingItemID)
	if err != nil {
		return err
	}

	if item.IsMessage() {
		return s.upsertMessage(ctx, tx, existingItemID, item.Message)
	}
	return nil
}

func (s *Store) upsertSubSession(ctx context.Context, tx *sql.Tx, sess *session.Session, parentID string, parentOrder int) error {
	permissionsJSON := ""
	if sess.Permissions != nil {
		permBytes, err := json.Marshal(sess.Permissions)
		if err != nil {
			return err
		}
		permissionsJSON = string(permBytes)
	}

	// Upsert the sub-session
	_, err := tx.ExecContext(ctx, `
		INSERT INTO sessions (id, title, working_dir, created_at, starred, tools_approved, send_user_message,
			max_iterations, input_tokens, output_tokens, cost, parent_session_id, parent_item_order, permissions)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title = excluded.title,
			working_dir = excluded.working_dir,
			starred = excluded.starred,
			tools_approved = excluded.tools_approved,
			send_user_message = excluded.send_user_message,
			max_iterations = excluded.max_iterations,
			input_tokens = excluded.input_tokens,
			output_tokens = excluded.output_tokens,
			cost = excluded.cost,
			parent_session_id = excluded.parent_session_id,
			parent_item_order = excluded.parent_item_order,
			permissions = excluded.permissions`,
		sess.ID, sess.Title, sess.WorkingDir, sess.CreatedAt.Format(time.RFC3339),
		boolToInt(sess.Starred), boolToInt(sess.ToolsApproved), boolToInt(sess.SendUserMessage),
		sess.MaxIterations, sess.InputTokens, sess.OutputTokens, sess.Cost,
		parentID, parentOrder, permissionsJSON)
	if err != nil {
		return err
	}

	// Upsert model overrides for sub-session
	if err := s.upsertModelOverrides(ctx, tx, sess); err != nil {
		return err
	}

	// Upsert custom models for sub-session
	if err := s.upsertCustomModels(ctx, tx, sess); err != nil {
		return err
	}

	// Upsert session items for sub-session
	return s.upsertSessionItems(ctx, tx, sess)
}

func (s *Store) upsertMessage(ctx context.Context, tx *sql.Tx, sessionItemID int64, msg *session.Message) error {
	createdAt := msg.Message.CreatedAt
	if createdAt == "" {
		createdAt = time.Now().Format(time.RFC3339)
	}

	// Get existing message ID
	var messageID int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM messages WHERE session_item_id = ?`, sessionItemID).Scan(&messageID)
	if errors.Is(err, sql.ErrNoRows) {
		// Insert new message
		return s.insertMessage(ctx, tx, sessionItemID, msg)
	}
	if err != nil {
		return err
	}

	var usageInput, usageOutput, usageCachedInput, usageCacheWrite, usageReasoning int64
	if msg.Message.Usage != nil {
		usageInput = msg.Message.Usage.InputTokens
		usageOutput = msg.Message.Usage.OutputTokens
		usageCachedInput = msg.Message.Usage.CachedInputTokens
		usageCacheWrite = msg.Message.Usage.CacheWriteTokens
		usageReasoning = msg.Message.Usage.ReasoningTokens
	}

	// Update existing message
	_, err = tx.ExecContext(ctx, `
		UPDATE messages
		SET agent_name = ?, role = ?, content = ?, created_at = ?, implicit = ?,
			tool_call_id = ?, model = ?, reasoning_content = ?, thinking_signature = ?,
			thought_signature = ?, message_cost = ?,
			input_tokens = ?, output_tokens = ?, cached_input_tokens = ?, cache_write_tokens = ?, reasoning_tokens = ?
		WHERE id = ?`,
		nullString(msg.AgentName), string(msg.Message.Role), msg.Message.Content, createdAt,
		boolToInt(msg.Implicit), nullString(msg.Message.ToolCallID), nullString(msg.Message.Model),
		nullString(msg.Message.ReasoningContent), nullString(msg.Message.ThinkingSignature),
		msg.Message.ThoughtSignature, msg.Message.Cost,
		usageInput, usageOutput, usageCachedInput, usageCacheWrite, usageReasoning, messageID)
	if err != nil {
		return err
	}

	// Upsert multi-content parts
	if err := s.upsertMessageParts(ctx, tx, messageID, msg); err != nil {
		return err
	}

	// Upsert tool calls
	if err := s.upsertToolCalls(ctx, tx, messageID, msg); err != nil {
		return err
	}

	// Upsert tool definitions
	return s.upsertToolDefinitions(ctx, tx, messageID, msg)
}

func (s *Store) upsertMessageParts(ctx context.Context, tx *sql.Tx, messageID int64, msg *session.Message) error {
	for i, part := range msg.Message.MultiContent {
		var imageURL, imageDetail *string
		if part.ImageURL != nil {
			imageURL = &part.ImageURL.URL
			if part.ImageURL.Detail != "" {
				detail := string(part.ImageURL.Detail)
				imageDetail = &detail
			}
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO message_parts (message_id, part_order, part_type, text_content, image_url, image_detail)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(message_id, part_order) DO UPDATE SET
				part_type = excluded.part_type,
				text_content = excluded.text_content,
				image_url = excluded.image_url,
				image_detail = excluded.image_detail`,
			messageID, i, string(part.Type), nullString(part.Text), imageURL, imageDetail)
		if err != nil {
			return err
		}
	}

	// Delete parts beyond the current list
	_, err := tx.ExecContext(ctx, `
		DELETE FROM message_parts WHERE message_id = ? AND part_order >= ?`,
		messageID, len(msg.Message.MultiContent))
	return err
}

func (s *Store) upsertToolCalls(ctx context.Context, tx *sql.Tx, messageID int64, msg *session.Message) error {
	for i, tc := range msg.Message.ToolCalls {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO tool_calls (id, message_id, call_order, tool_type, function_name, function_arguments)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(message_id, call_order) DO UPDATE SET
				id = excluded.id,
				tool_type = excluded.tool_type,
				function_name = excluded.function_name,
				function_arguments = excluded.function_arguments`,
			tc.ID, messageID, i, string(tc.Type), tc.Function.Name, tc.Function.Arguments)
		if err != nil {
			return err
		}
	}

	// Delete tool calls beyond the current list
	_, err := tx.ExecContext(ctx, `
		DELETE FROM tool_calls WHERE message_id = ? AND call_order >= ?`,
		messageID, len(msg.Message.ToolCalls))
	return err
}

func (s *Store) upsertToolDefinitions(ctx context.Context, tx *sql.Tx, messageID int64, msg *session.Message) error {
	// For tool definitions, we use INSERT OR IGNORE since they're deduplicated by hash
	for _, td := range msg.Message.ToolDefinitions {
		toolDefID, err := s.getOrCreateToolDefinition(ctx, tx, &td)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO message_tool_definitions (message_id, tool_definition_id)
			VALUES (?, ?)`,
			messageID, toolDefID)
		if err != nil {
			return err
		}
	}
	return nil
}

// AddSession adds a new session to the store.
func (s *Store) AddSession(ctx context.Context, sess *session.Session) error {
	if sess.ID == "" {
		return session.ErrEmptyID
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := s.insertSession(ctx, tx, sess, "", 0); err != nil {
		return err
	}

	return tx.Commit()
}

// GetSession retrieves a session by ID.
func (s *Store) GetSession(ctx context.Context, id string) (*session.Session, error) {
	if id == "" {
		return nil, session.ErrEmptyID
	}

	sess, err := s.loadSession(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, session.ErrNotFound
		}
		return nil, err
	}

	return sess, nil
}

// GetSessions retrieves all root sessions (not sub-sessions).
func (s *Store) GetSessions(ctx context.Context) ([]*session.Session, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM sessions WHERE parent_session_id IS NULL ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}

	// Collect all IDs first to avoid nested queries
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Now load each session
	var sessions []*session.Session
	for _, id := range ids {
		sess, err := s.loadSession(ctx, id)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}

	return sessions, nil
}

// GetSessionSummaries retrieves lightweight session metadata for listing.
func (s *Store) GetSessionSummaries(ctx context.Context) ([]session.Summary, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, created_at, starred FROM sessions WHERE parent_session_id IS NULL ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []session.Summary
	for rows.Next() {
		var id, title, createdAtStr string
		var starred int
		if err := rows.Scan(&id, &title, &createdAtStr, &starred); err != nil {
			return nil, err
		}
		createdAt, err := time.Parse(time.RFC3339, createdAtStr)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, session.Summary{
			ID:        id,
			Title:     title,
			CreatedAt: createdAt,
			Starred:   starred != 0,
		})
	}

	return summaries, rows.Err()
}

// DeleteSession deletes a session and all related data.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	if id == "" {
		return session.ErrEmptyID
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
		return session.ErrNotFound
	}

	return nil
}

// UpdateSession updates an existing session or creates it if it doesn't exist (upsert).
// This method uses proper upsert semantics to avoid deleting and re-inserting data.
func (s *Store) UpdateSession(ctx context.Context, sess *session.Session) error {
	if sess.ID == "" {
		return session.ErrEmptyID
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Upsert session metadata
	if err := s.upsertSession(ctx, tx, sess); err != nil {
		return err
	}

	// Upsert model overrides - delete ones not in the new set, upsert the rest
	if err := s.upsertModelOverrides(ctx, tx, sess); err != nil {
		return err
	}

	// Upsert custom models - just insert or ignore
	if err := s.upsertCustomModels(ctx, tx, sess); err != nil {
		return err
	}

	// Upsert session items (messages, sub-sessions, summaries)
	if err := s.upsertSessionItems(ctx, tx, sess); err != nil {
		return err
	}

	return tx.Commit()
}

// SetSessionStarred sets the starred status of a session.
func (s *Store) SetSessionStarred(ctx context.Context, id string, starred bool) error {
	if id == "" {
		return session.ErrEmptyID
	}

	starredInt := 0
	if starred {
		starredInt = 1
	}

	result, err := s.db.ExecContext(ctx, "UPDATE sessions SET starred = ? WHERE id = ?", starredInt, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return session.ErrNotFound
	}

	return nil
}

func (s *Store) insertSession(ctx context.Context, tx *sql.Tx, sess *session.Session, parentID string, parentOrder int) error {
	permissionsJSON := ""
	if sess.Permissions != nil {
		permBytes, err := json.Marshal(sess.Permissions)
		if err != nil {
			return err
		}
		permissionsJSON = string(permBytes)
	}

	var parentIDPtr *string
	var parentOrderPtr *int
	if parentID != "" {
		parentIDPtr = &parentID
		parentOrderPtr = &parentOrder
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO sessions (id, title, working_dir, created_at, starred, tools_approved, send_user_message, 
			max_iterations, input_tokens, output_tokens, cost, parent_session_id, parent_item_order, permissions)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.Title, sess.WorkingDir, sess.CreatedAt.Format(time.RFC3339),
		boolToInt(sess.Starred), boolToInt(sess.ToolsApproved), boolToInt(sess.SendUserMessage),
		sess.MaxIterations, sess.InputTokens, sess.OutputTokens, sess.Cost,
		parentIDPtr, parentOrderPtr, permissionsJSON)
	if err != nil {
		return err
	}

	// Insert model overrides
	for agentName, modelRef := range sess.AgentModelOverrides {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO session_model_overrides (session_id, agent_name, model_reference)
			VALUES (?, ?, ?)`,
			sess.ID, agentName, modelRef)
		if err != nil {
			return err
		}
	}

	// Insert custom models
	for _, modelRef := range sess.CustomModelsUsed {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO session_custom_models (session_id, model_reference)
			VALUES (?, ?)`,
			sess.ID, modelRef)
		if err != nil {
			return err
		}
	}

	// Insert session items (messages, sub-sessions, summaries)
	for i, item := range sess.Messages {
		if err := s.insertSessionItem(ctx, tx, sess.ID, i, &item); err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) insertSessionItem(ctx context.Context, tx *sql.Tx, sessionID string, order int, item *session.Item) error {
	var itemType string
	var subSessionID *string
	var summaryText *string

	switch {
	case item.IsMessage():
		itemType = "message"
	case item.IsSubSession():
		itemType = "sub_session"
		// Insert sub-session first (before referencing it)
		if err := s.insertSession(ctx, tx, item.SubSession, sessionID, order); err != nil {
			return err
		}
		subSessionID = &item.SubSession.ID
	case item.Summary != "":
		itemType = "summary"
		summaryText = &item.Summary
	default:
		return nil // Skip empty items
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO session_items (session_id, item_order, item_type, sub_session_id, summary_text)
		VALUES (?, ?, ?, ?, ?)`,
		sessionID, order, itemType, subSessionID, summaryText)
	if err != nil {
		return err
	}

	if item.IsMessage() {
		sessionItemID, err := result.LastInsertId()
		if err != nil {
			return err
		}
		if err := s.insertMessage(ctx, tx, sessionItemID, item.Message); err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) insertMessage(ctx context.Context, tx *sql.Tx, sessionItemID int64, msg *session.Message) error {
	createdAt := msg.Message.CreatedAt
	if createdAt == "" {
		createdAt = time.Now().Format(time.RFC3339)
	}

	var usageInput, usageOutput, usageCachedInput, usageCacheWrite, usageReasoning int64
	if msg.Message.Usage != nil {
		usageInput = msg.Message.Usage.InputTokens
		usageOutput = msg.Message.Usage.OutputTokens
		usageCachedInput = msg.Message.Usage.CachedInputTokens
		usageCacheWrite = msg.Message.Usage.CacheWriteTokens
		usageReasoning = msg.Message.Usage.ReasoningTokens
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO messages (session_item_id, agent_name, role, content, created_at, implicit, 
			tool_call_id, model, reasoning_content, thinking_signature, thought_signature, message_cost,
			input_tokens, output_tokens, cached_input_tokens, cache_write_tokens, reasoning_tokens)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sessionItemID, nullString(msg.AgentName), string(msg.Message.Role), msg.Message.Content, createdAt,
		boolToInt(msg.Implicit), nullString(msg.Message.ToolCallID), nullString(msg.Message.Model),
		nullString(msg.Message.ReasoningContent), nullString(msg.Message.ThinkingSignature),
		msg.Message.ThoughtSignature, msg.Message.Cost,
		usageInput, usageOutput, usageCachedInput, usageCacheWrite, usageReasoning)
	if err != nil {
		return err
	}

	messageID, err := result.LastInsertId()
	if err != nil {
		return err
	}

	// Insert multi-content parts
	for i, part := range msg.Message.MultiContent {
		var imageURL, imageDetail *string
		if part.ImageURL != nil {
			imageURL = &part.ImageURL.URL
			if part.ImageURL.Detail != "" {
				detail := string(part.ImageURL.Detail)
				imageDetail = &detail
			}
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO message_parts (message_id, part_order, part_type, text_content, image_url, image_detail)
			VALUES (?, ?, ?, ?, ?, ?)`,
			messageID, i, string(part.Type), nullString(part.Text), imageURL, imageDetail)
		if err != nil {
			return err
		}
	}

	// Insert tool calls
	for i, tc := range msg.Message.ToolCalls {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO tool_calls (id, message_id, call_order, tool_type, function_name, function_arguments)
			VALUES (?, ?, ?, ?, ?, ?)`,
			tc.ID, messageID, i, string(tc.Type), tc.Function.Name, tc.Function.Arguments)
		if err != nil {
			return err
		}
	}

	// Insert tool definitions (with deduplication)
	for _, td := range msg.Message.ToolDefinitions {
		toolDefID, err := s.getOrCreateToolDefinition(ctx, tx, &td)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO message_tool_definitions (message_id, tool_definition_id)
			VALUES (?, ?)`,
			messageID, toolDefID)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) getOrCreateToolDefinition(ctx context.Context, tx *sql.Tx, td *tools.Tool) (int64, error) {
	parametersJSON, _ := json.Marshal(td.Parameters)
	annotationsJSON, _ := json.Marshal(td.Annotations)
	outputSchemaJSON, _ := json.Marshal(td.OutputSchema)

	// Compute hash for deduplication
	hashData := td.Name + td.Category + td.Description + string(parametersJSON) + string(annotationsJSON) + string(outputSchemaJSON)
	hash := sha256.Sum256([]byte(hashData))
	contentHash := hex.EncodeToString(hash[:])

	// Try to find existing
	var existingID int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM tool_definitions WHERE content_hash = ?`, contentHash).Scan(&existingID)
	if err == nil {
		return existingID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}

	// Insert new
	result, err := tx.ExecContext(ctx, `
		INSERT INTO tool_definitions (name, category, description, parameters, annotations, output_schema, content_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		td.Name, nullString(td.Category), nullString(td.Description),
		string(parametersJSON), string(annotationsJSON), string(outputSchemaJSON), contentHash)
	if err != nil {
		return 0, err
	}

	return result.LastInsertId()
}

func (s *Store) loadSession(ctx context.Context, id string) (*session.Session, error) {
	var sess session.Session
	var createdAtStr, permissionsJSON string
	var starred, toolsApproved, sendUserMessage int

	err := s.db.QueryRowContext(ctx, `
		SELECT id, title, working_dir, created_at, starred, tools_approved, send_user_message,
			max_iterations, input_tokens, output_tokens, cost, permissions
		FROM sessions WHERE id = ?`, id).Scan(
		&sess.ID, &sess.Title, &sess.WorkingDir, &createdAtStr, &starred, &toolsApproved, &sendUserMessage,
		&sess.MaxIterations, &sess.InputTokens, &sess.OutputTokens, &sess.Cost, &permissionsJSON)
	if err != nil {
		return nil, err
	}

	sess.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
	sess.Starred = starred != 0
	sess.ToolsApproved = toolsApproved != 0
	sess.SendUserMessage = sendUserMessage != 0

	if permissionsJSON != "" {
		sess.Permissions = &session.PermissionsConfig{}
		if err := json.Unmarshal([]byte(permissionsJSON), sess.Permissions); err != nil {
			return nil, err
		}
	}

	// Load model overrides
	sess.AgentModelOverrides = make(map[string]string)
	rows, err := s.db.QueryContext(ctx, `SELECT agent_name, model_reference FROM session_model_overrides WHERE session_id = ?`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var agentName, modelRef string
		if err := rows.Scan(&agentName, &modelRef); err != nil {
			rows.Close()
			return nil, err
		}
		sess.AgentModelOverrides[agentName] = modelRef
	}
	rows.Close()

	// Load custom models
	rows, err = s.db.QueryContext(ctx, `SELECT model_reference FROM session_custom_models WHERE session_id = ?`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var modelRef string
		if err := rows.Scan(&modelRef); err != nil {
			rows.Close()
			return nil, err
		}
		sess.CustomModelsUsed = append(sess.CustomModelsUsed, modelRef)
	}
	rows.Close()

	// Load session items
	items, err := s.loadSessionItems(ctx, id)
	if err != nil {
		return nil, err
	}
	sess.Messages = items

	return &sess, nil
}

type sessionItemRow struct {
	itemID       int64
	itemType     string
	subSessionID sql.NullString
	summaryText  sql.NullString
}

func (s *Store) loadSessionItems(ctx context.Context, sessionID string) ([]session.Item, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, item_type, sub_session_id, summary_text
		FROM session_items WHERE session_id = ? ORDER BY item_order`, sessionID)
	if err != nil {
		return nil, err
	}

	// Collect all rows first to avoid nested queries while rows are open
	var itemRows []sessionItemRow
	for rows.Next() {
		var row sessionItemRow
		if err := rows.Scan(&row.itemID, &row.itemType, &row.subSessionID, &row.summaryText); err != nil {
			rows.Close()
			return nil, err
		}
		itemRows = append(itemRows, row)
	}
	rows.Close()

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Now process the collected rows
	var items []session.Item
	for _, row := range itemRows {
		var item session.Item
		switch row.itemType {
		case "message":
			msg, err := s.loadMessage(ctx, row.itemID)
			if err != nil {
				return nil, err
			}
			item = session.Item{Message: msg}
		case "sub_session":
			if row.subSessionID.Valid {
				subSess, err := s.loadSession(ctx, row.subSessionID.String)
				if err != nil {
					return nil, err
				}
				item = session.Item{SubSession: subSess}
			}
		case "summary":
			if row.summaryText.Valid {
				item = session.Item{Summary: row.summaryText.String}
			}
		}
		items = append(items, item)
	}

	return items, nil
}

func (s *Store) loadMessage(ctx context.Context, sessionItemID int64) (*session.Message, error) {
	var msg session.Message
	var agentName, role, createdAt sql.NullString
	var toolCallID, model, reasoningContent, thinkingSignature sql.NullString
	var thoughtSignature []byte
	var implicit int
	var messageCost float64
	var messageID int64
	var usage chat.Usage

	err := s.db.QueryRowContext(ctx, `
		SELECT id, agent_name, role, content, created_at, implicit, tool_call_id, model,
			reasoning_content, thinking_signature, thought_signature, message_cost,
			input_tokens, output_tokens, cached_input_tokens, cache_write_tokens, reasoning_tokens
		FROM messages WHERE session_item_id = ?`, sessionItemID).Scan(
		&messageID, &agentName, &role, &msg.Message.Content, &createdAt, &implicit, &toolCallID, &model,
		&reasoningContent, &thinkingSignature, &thoughtSignature, &messageCost,
		&usage.InputTokens, &usage.OutputTokens, &usage.CachedInputTokens, &usage.CacheWriteTokens, &usage.ReasoningTokens)
	if err != nil {
		return nil, err
	}

	msg.AgentName = agentName.String
	msg.Message.Role = chat.MessageRole(role.String)
	msg.Message.CreatedAt = createdAt.String
	msg.Implicit = implicit != 0
	msg.Message.ToolCallID = toolCallID.String
	msg.Message.Model = model.String
	msg.Message.ReasoningContent = reasoningContent.String
	msg.Message.ThinkingSignature = thinkingSignature.String
	msg.Message.ThoughtSignature = thoughtSignature
	msg.Message.Cost = messageCost

	if usage.InputTokens > 0 || usage.OutputTokens > 0 || usage.CachedInputTokens > 0 || usage.CacheWriteTokens > 0 || usage.ReasoningTokens > 0 {
		msg.Message.Usage = &usage
	}

	// Load multi-content parts
	partRows, err := s.db.QueryContext(ctx, `
		SELECT part_type, text_content, image_url, image_detail
		FROM message_parts WHERE message_id = ? ORDER BY part_order`, messageID)
	if err != nil {
		return nil, err
	}
	defer partRows.Close()

	for partRows.Next() {
		var partType string
		var textContent, imageURL, imageDetail sql.NullString
		if err := partRows.Scan(&partType, &textContent, &imageURL, &imageDetail); err != nil {
			return nil, err
		}
		if !imageURL.Valid && textContent.String == "" {
			continue
		}
		part := chat.MessagePart{
			Type: chat.MessagePartType(partType),
			Text: textContent.String,
		}
		if imageURL.Valid {
			part.ImageURL = &chat.MessageImageURL{
				URL:    imageURL.String,
				Detail: chat.ImageURLDetail(imageDetail.String),
			}
		}
		msg.Message.MultiContent = append(msg.Message.MultiContent, part)
	}

	// Load tool calls
	tcRows, err := s.db.QueryContext(ctx, `
		SELECT id, tool_type, function_name, function_arguments
		FROM tool_calls WHERE message_id = ? ORDER BY call_order`, messageID)
	if err != nil {
		return nil, err
	}
	defer tcRows.Close()

	for tcRows.Next() {
		var tc tools.ToolCall
		var toolType string
		if err := tcRows.Scan(&tc.ID, &toolType, &tc.Function.Name, &tc.Function.Arguments); err != nil {
			return nil, err
		}
		if tc.Function.Name == "" {
			continue
		}
		tc.Type = tools.ToolType(toolType)
		msg.Message.ToolCalls = append(msg.Message.ToolCalls, tc)
	}

	// Load tool definitions
	tdRows, err := s.db.QueryContext(ctx, `
		SELECT td.name, td.category, td.description, td.parameters, td.annotations, td.output_schema
		FROM tool_definitions td
		JOIN message_tool_definitions mtd ON td.id = mtd.tool_definition_id
		WHERE mtd.message_id = ?`, messageID)
	if err != nil {
		return nil, err
	}
	defer tdRows.Close()

	for tdRows.Next() {
		var td tools.Tool
		var category, description, parametersJSON, annotationsJSON, outputSchemaJSON sql.NullString
		if err := tdRows.Scan(&td.Name, &category, &description, &parametersJSON, &annotationsJSON, &outputSchemaJSON); err != nil {
			return nil, err
		}
		td.Category = category.String
		td.Description = description.String
		if parametersJSON.Valid {
			_ = json.Unmarshal([]byte(parametersJSON.String), &td.Parameters)
		}
		if annotationsJSON.Valid {
			_ = json.Unmarshal([]byte(annotationsJSON.String), &td.Annotations)
		}
		if outputSchemaJSON.Valid {
			_ = json.Unmarshal([]byte(outputSchemaJSON.String), &td.OutputSchema)
		}
		msg.Message.ToolDefinitions = append(msg.Message.ToolDefinitions, td)
	}

	return &msg, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
