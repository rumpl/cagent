package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"

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

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) AddSession(ctx context.Context, sess *session.Session) error {
	if sess.ID == "" {
		return session.ErrEmptyID
	}

	slog.Debug("AddSession", "session_id", sess.ID, "title", sess.Title, "items_count", len(sess.Messages))

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		slog.Debug("AddSession: failed to begin transaction", "error", err)
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := s.insertSession(ctx, tx, sess); err != nil {
		slog.Debug("AddSession: failed to insert session", "error", err)
		return err
	}

	for i, item := range sess.Messages {
		if err := s.insertSessionItem(ctx, tx, sess.ID, i, &item); err != nil {
			slog.Debug("AddSession: failed to insert session item", "position", i, "error", err)
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		slog.Debug("AddSession: failed to commit transaction", "error", err)
		return err
	}

	slog.Debug("AddSession: success", "session_id", sess.ID)
	return nil
}

func (s *Store) insertSession(ctx context.Context, tx *sql.Tx, sess *session.Session) error {
	slog.Debug("insertSession", "session_id", sess.ID)

	permissionsJSON, err := marshalNullableJSON(sess.Permissions)
	if err != nil {
		return err
	}

	agentModelOverridesJSON, err := marshalMapJSON(sess.AgentModelOverrides)
	if err != nil {
		return err
	}

	customModelsUsedJSON, err := marshalSliceJSON(sess.CustomModelsUsed)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO sessions (
			id, title, created_at, tools_approved, thinking, hide_tool_results,
			working_dir, max_iterations, starred, input_tokens, output_tokens,
			cost, permissions, agent_model_overrides, custom_models_used
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.Title, sess.CreatedAt, boolToInt(sess.ToolsApproved),
		boolToInt(sess.Thinking), boolToInt(sess.HideToolResults), sess.WorkingDir,
		sess.MaxIterations, boolToInt(sess.Starred), sess.InputTokens, sess.OutputTokens,
		sess.Cost, permissionsJSON, agentModelOverridesJSON, customModelsUsedJSON,
	)
	return err
}

func (s *Store) insertSessionItem(ctx context.Context, tx *sql.Tx, sessionID string, position int, item *session.Item) error {
	var itemType string
	var subSessionID *string
	var summary *string

	switch {
	case item.Message != nil:
		itemType = "message"
		slog.Debug("insertSessionItem: message", "session_id", sessionID, "position", position, "role", item.Message.Message.Role)
	case item.SubSession != nil:
		itemType = "sub_session"
		subSessionID = &item.SubSession.ID
		slog.Debug("insertSessionItem: sub_session", "session_id", sessionID, "position", position, "sub_session_id", item.SubSession.ID)
		if err := s.insertSession(ctx, tx, item.SubSession); err != nil {
			return err
		}
		for i, subItem := range item.SubSession.Messages {
			if err := s.insertSessionItem(ctx, tx, item.SubSession.ID, i, &subItem); err != nil {
				return err
			}
		}
	case item.Summary != "":
		itemType = "summary"
		summary = &item.Summary
		slog.Debug("insertSessionItem: summary", "session_id", sessionID, "position", position)
	default:
		return fmt.Errorf("invalid session item: no message, sub_session, or summary")
	}

	result, err := tx.ExecContext(ctx,
		`INSERT INTO session_items (session_id, position, item_type, sub_session_id, summary)
		 VALUES (?, ?, ?, ?, ?)`,
		sessionID, position, itemType, subSessionID, summary,
	)
	if err != nil {
		return err
	}

	if item.Message != nil {
		itemID, err := result.LastInsertId()
		if err != nil {
			return err
		}
		messageID, err := s.insertMessage(ctx, tx, itemID, item.Message)
		if err != nil {
			slog.Debug("insertSessionItem: failed to insert message", "error", err)
			return err
		}
		item.Message.ID = fmt.Sprintf("%d", messageID)
		slog.Debug("insertSessionItem: message inserted", "message_id", item.Message.ID)
	}

	return nil
}

func (s *Store) insertMessage(ctx context.Context, tx *sql.Tx, sessionItemID int64, msg *session.Message) (int64, error) {
	slog.Debug("insertMessage", "session_item_id", sessionItemID, "role", msg.Message.Role, "content_len", len(msg.Message.Content))

	chatMsg := &msg.Message

	var functionCallName, functionCallArgs *string
	if chatMsg.FunctionCall != nil {
		functionCallName = &chatMsg.FunctionCall.Name
		functionCallArgs = &chatMsg.FunctionCall.Arguments
	}

	var usageInput, usageOutput, usageCachedInput, usageCacheWrite, usageReasoning *int64
	if chatMsg.Usage != nil {
		usageInput = &chatMsg.Usage.InputTokens
		usageOutput = &chatMsg.Usage.OutputTokens
		usageCachedInput = &chatMsg.Usage.CachedInputTokens
		usageCacheWrite = &chatMsg.Usage.CacheWriteTokens
		usageReasoning = &chatMsg.Usage.ReasoningTokens
	}

	result, err := tx.ExecContext(ctx,
		`INSERT INTO messages (
			session_item_id, agent_name, implicit, role, content, reasoning_content,
			thinking_signature, thought_signature, tool_call_id, created_at, model, cost, cache_control,
			usage_input_tokens, usage_output_tokens, usage_cached_input_tokens,
			usage_cache_write_tokens, usage_reasoning_tokens,
			function_call_name, function_call_arguments
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sessionItemID, msg.AgentName, boolToInt(msg.Implicit), string(chatMsg.Role),
		chatMsg.Content, nullString(chatMsg.ReasoningContent), nullString(chatMsg.ThinkingSignature),
		chatMsg.ThoughtSignature, nullString(chatMsg.ToolCallID), nullString(chatMsg.CreatedAt),
		nullString(chatMsg.Model), chatMsg.Cost, boolToInt(chatMsg.CacheControl),
		usageInput, usageOutput, usageCachedInput, usageCacheWrite, usageReasoning,
		functionCallName, functionCallArgs,
	)
	if err != nil {
		return 0, err
	}

	messageID, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	for i, part := range chatMsg.MultiContent {
		if err := s.insertMessagePart(ctx, tx, messageID, i, &part); err != nil {
			return 0, err
		}
	}

	for i, tc := range chatMsg.ToolCalls {
		if err := s.insertToolCall(ctx, tx, messageID, i, &tc); err != nil {
			return 0, err
		}
	}

	for i, td := range chatMsg.ToolDefinitions {
		if err := s.insertToolDefinition(ctx, tx, messageID, i, &td); err != nil {
			return 0, err
		}
	}

	return messageID, nil
}

func (s *Store) insertMessagePart(ctx context.Context, tx *sql.Tx, messageID int64, position int, part *chat.MessagePart) error {
	var imageURL, imageURLDetail *string
	if part.ImageURL != nil {
		imageURL = &part.ImageURL.URL
		if part.ImageURL.Detail != "" {
			detail := string(part.ImageURL.Detail)
			imageURLDetail = &detail
		}
	}

	_, err := tx.ExecContext(ctx,
		`INSERT INTO message_parts (message_id, position, part_type, text, image_url, image_url_detail)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		messageID, position, string(part.Type), nullString(part.Text), imageURL, imageURLDetail,
	)
	return err
}

func (s *Store) insertToolCall(ctx context.Context, tx *sql.Tx, messageID int64, position int, tc *tools.ToolCall) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO message_tool_calls (message_id, position, tool_call_id, tool_type, function_name, function_arguments)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		messageID, position, nullString(tc.ID), nullString(string(tc.Type)),
		nullString(tc.Function.Name), nullString(tc.Function.Arguments),
	)
	return err
}

func (s *Store) insertToolDefinition(ctx context.Context, tx *sql.Tx, messageID int64, position int, td *tools.Tool) error {
	var paramsSchema, outputSchema *string
	if td.Parameters != nil {
		data, err := json.Marshal(td.Parameters)
		if err != nil {
			return err
		}
		str := string(data)
		paramsSchema = &str
	}
	if td.OutputSchema != nil {
		data, err := json.Marshal(td.OutputSchema)
		if err != nil {
			return err
		}
		str := string(data)
		outputSchema = &str
	}

	_, err := tx.ExecContext(ctx,
		`INSERT INTO message_tool_definitions (
			message_id, position, name, category, description, parameters_schema, output_schema,
			annotation_title, annotation_read_only_hint, annotation_destructive_hint,
			annotation_idempotent_hint, annotation_open_world_hint
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		messageID, position, td.Name, nullString(td.Category), nullString(td.Description),
		paramsSchema, outputSchema,
		nullString(td.Annotations.Title), td.Annotations.ReadOnlyHint,
		td.Annotations.DestructiveHint, td.Annotations.IdempotentHint,
		td.Annotations.OpenWorldHint,
	)
	return err
}

func (s *Store) GetSession(ctx context.Context, id string) (*session.Session, error) {
	if id == "" {
		return nil, session.ErrEmptyID
	}

	slog.Debug("GetSession", "session_id", id)

	sess, err := s.loadSession(ctx, id)
	if err != nil {
		slog.Debug("GetSession: failed", "session_id", id, "error", err)
		return nil, err
	}

	slog.Debug("GetSession: success", "session_id", id, "items_count", len(sess.Messages))
	return sess, nil
}

func (s *Store) loadSession(ctx context.Context, id string) (*session.Session, error) {
	slog.Debug("loadSession", "session_id", id)

	row := s.db.QueryRowContext(ctx,
		`SELECT id, title, created_at, tools_approved, thinking, hide_tool_results,
		        working_dir, max_iterations, starred, input_tokens, output_tokens,
		        cost, permissions, agent_model_overrides, custom_models_used
		 FROM sessions WHERE id = ?`, id)

	var sess session.Session
	var workingDir sql.NullString
	var permissionsJSON, agentModelOverridesJSON, customModelsUsedJSON sql.NullString
	var toolsApproved, thinking, hideToolResults, starred int

	err := row.Scan(
		&sess.ID, &sess.Title, &sess.CreatedAt, &toolsApproved, &thinking, &hideToolResults,
		&workingDir, &sess.MaxIterations, &starred, &sess.InputTokens, &sess.OutputTokens,
		&sess.Cost, &permissionsJSON, &agentModelOverridesJSON, &customModelsUsedJSON,
	)
	if err == sql.ErrNoRows {
		return nil, session.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	sess.ToolsApproved = toolsApproved != 0
	sess.Thinking = thinking != 0
	sess.HideToolResults = hideToolResults != 0
	sess.Starred = starred != 0
	sess.WorkingDir = workingDir.String

	if permissionsJSON.Valid && permissionsJSON.String != "" {
		if err := json.Unmarshal([]byte(permissionsJSON.String), &sess.Permissions); err != nil {
			return nil, err
		}
	}
	if agentModelOverridesJSON.Valid && agentModelOverridesJSON.String != "" && agentModelOverridesJSON.String != "{}" {
		if err := json.Unmarshal([]byte(agentModelOverridesJSON.String), &sess.AgentModelOverrides); err != nil {
			return nil, err
		}
	}
	if customModelsUsedJSON.Valid && customModelsUsedJSON.String != "" && customModelsUsedJSON.String != "[]" {
		if err := json.Unmarshal([]byte(customModelsUsedJSON.String), &sess.CustomModelsUsed); err != nil {
			return nil, err
		}
	}

	items, err := s.loadSessionItems(ctx, id)
	if err != nil {
		return nil, err
	}
	sess.Messages = items

	return &sess, nil
}

func (s *Store) loadSessionItems(ctx context.Context, sessionID string) ([]session.Item, error) {
	slog.Debug("loadSessionItems", "session_id", sessionID)

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, item_type, sub_session_id, summary
		 FROM session_items WHERE session_id = ? ORDER BY position`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []session.Item
	for rows.Next() {
		var itemID int64
		var itemType string
		var subSessionID, summary sql.NullString

		if err := rows.Scan(&itemID, &itemType, &subSessionID, &summary); err != nil {
			return nil, err
		}

		var item session.Item
		switch itemType {
		case "message":
			slog.Debug("loadSessionItems: loading message", "session_id", sessionID, "item_id", itemID)
			msg, err := s.loadMessage(ctx, itemID)
			if err != nil {
				slog.Debug("loadSessionItems: failed to load message", "item_id", itemID, "error", err)
				return nil, err
			}
			item.Message = msg
		case "sub_session":
			if subSessionID.Valid {
				slog.Debug("loadSessionItems: loading sub_session", "session_id", sessionID, "sub_session_id", subSessionID.String)
				subSess, err := s.loadSession(ctx, subSessionID.String)
				if err != nil {
					slog.Debug("loadSessionItems: failed to load sub_session", "sub_session_id", subSessionID.String, "error", err)
					return nil, err
				}
				item.SubSession = subSess
			}
		case "summary":
			slog.Debug("loadSessionItems: loading summary", "session_id", sessionID)
			if summary.Valid {
				item.Summary = summary.String
			}
		}
		items = append(items, item)
	}

	slog.Debug("loadSessionItems: loaded", "session_id", sessionID, "count", len(items))
	return items, rows.Err()
}

func (s *Store) loadMessage(ctx context.Context, sessionItemID int64) (*session.Message, error) {
	slog.Debug("loadMessage", "session_item_id", sessionItemID)

	row := s.db.QueryRowContext(ctx,
		`SELECT id, agent_name, implicit, role, content, reasoning_content,
		        thinking_signature, thought_signature, tool_call_id, created_at, model, cost, cache_control,
		        usage_input_tokens, usage_output_tokens, usage_cached_input_tokens,
		        usage_cache_write_tokens, usage_reasoning_tokens,
		        function_call_name, function_call_arguments
		 FROM messages WHERE session_item_id = ?`, sessionItemID)

	var msg session.Message
	var messageID int64
	var implicit, cacheControl int
	var role string
	var reasoningContent, thinkingSignature, toolCallID, createdAt, model sql.NullString
	var thoughtSignature []byte
	var usageInput, usageOutput, usageCachedInput, usageCacheWrite, usageReasoning sql.NullInt64
	var functionCallName, functionCallArgs sql.NullString

	err := row.Scan(
		&messageID, &msg.AgentName, &implicit, &role, &msg.Message.Content, &reasoningContent,
		&thinkingSignature, &thoughtSignature, &toolCallID, &createdAt, &model, &msg.Message.Cost, &cacheControl,
		&usageInput, &usageOutput, &usageCachedInput, &usageCacheWrite, &usageReasoning,
		&functionCallName, &functionCallArgs,
	)
	if err != nil {
		return nil, err
	}

	msg.ID = fmt.Sprintf("%d", messageID)
	msg.Implicit = implicit != 0
	msg.Message.Role = chat.MessageRole(role)
	msg.Message.ReasoningContent = reasoningContent.String
	msg.Message.ThinkingSignature = thinkingSignature.String
	msg.Message.ThoughtSignature = thoughtSignature
	msg.Message.ToolCallID = toolCallID.String
	msg.Message.CreatedAt = createdAt.String
	msg.Message.Model = model.String
	msg.Message.CacheControl = cacheControl != 0

	if usageInput.Valid {
		msg.Message.Usage = &chat.Usage{
			InputTokens:       usageInput.Int64,
			OutputTokens:      usageOutput.Int64,
			CachedInputTokens: usageCachedInput.Int64,
			CacheWriteTokens:  usageCacheWrite.Int64,
			ReasoningTokens:   usageReasoning.Int64,
		}
	}

	if functionCallName.Valid {
		msg.Message.FunctionCall = &tools.FunctionCall{
			Name:      functionCallName.String,
			Arguments: functionCallArgs.String,
		}
	}

	parts, err := s.loadMessageParts(ctx, messageID)
	if err != nil {
		return nil, err
	}
	msg.Message.MultiContent = parts

	toolCalls, err := s.loadToolCalls(ctx, messageID)
	if err != nil {
		return nil, err
	}
	msg.Message.ToolCalls = toolCalls

	toolDefs, err := s.loadToolDefinitions(ctx, messageID)
	if err != nil {
		return nil, err
	}
	msg.Message.ToolDefinitions = toolDefs

	return &msg, nil
}

func (s *Store) loadMessageParts(ctx context.Context, messageID int64) ([]chat.MessagePart, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT part_type, text, image_url, image_url_detail
		 FROM message_parts WHERE message_id = ? ORDER BY position`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var parts []chat.MessagePart
	for rows.Next() {
		var partType string
		var text, imageURL, imageURLDetail sql.NullString

		if err := rows.Scan(&partType, &text, &imageURL, &imageURLDetail); err != nil {
			return nil, err
		}

		part := chat.MessagePart{
			Type: chat.MessagePartType(partType),
			Text: text.String,
		}
		if imageURL.Valid {
			part.ImageURL = &chat.MessageImageURL{
				URL:    imageURL.String,
				Detail: chat.ImageURLDetail(imageURLDetail.String),
			}
		}
		parts = append(parts, part)
	}

	return parts, rows.Err()
}

func (s *Store) loadToolCalls(ctx context.Context, messageID int64) ([]tools.ToolCall, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT tool_call_id, tool_type, function_name, function_arguments
		 FROM message_tool_calls WHERE message_id = ? ORDER BY position`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var toolCalls []tools.ToolCall
	for rows.Next() {
		var tcID, tcType, fnName, fnArgs sql.NullString

		if err := rows.Scan(&tcID, &tcType, &fnName, &fnArgs); err != nil {
			return nil, err
		}

		toolCalls = append(toolCalls, tools.ToolCall{
			ID:   tcID.String,
			Type: tools.ToolType(tcType.String),
			Function: tools.FunctionCall{
				Name:      fnName.String,
				Arguments: fnArgs.String,
			},
		})
	}

	return toolCalls, rows.Err()
}

func (s *Store) loadToolDefinitions(ctx context.Context, messageID int64) ([]tools.Tool, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT name, category, description, parameters_schema, output_schema,
		        annotation_title, annotation_read_only_hint, annotation_destructive_hint,
		        annotation_idempotent_hint, annotation_open_world_hint
		 FROM message_tool_definitions WHERE message_id = ? ORDER BY position`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var toolDefs []tools.Tool
	for rows.Next() {
		var name string
		var category, description, paramsSchema, outputSchema sql.NullString
		var annTitle sql.NullString
		var annReadOnly, annDestructive, annIdempotent, annOpenWorld sql.NullBool

		if err := rows.Scan(
			&name, &category, &description, &paramsSchema, &outputSchema,
			&annTitle, &annReadOnly, &annDestructive, &annIdempotent, &annOpenWorld,
		); err != nil {
			return nil, err
		}

		td := tools.Tool{
			Name:        name,
			Category:    category.String,
			Description: description.String,
		}

		if paramsSchema.Valid && paramsSchema.String != "" {
			var params any
			if err := json.Unmarshal([]byte(paramsSchema.String), &params); err != nil {
				return nil, err
			}
			td.Parameters = params
		}
		if outputSchema.Valid && outputSchema.String != "" {
			var output any
			if err := json.Unmarshal([]byte(outputSchema.String), &output); err != nil {
				return nil, err
			}
			td.OutputSchema = output
		}

		td.Annotations.Title = annTitle.String
		if annReadOnly.Valid {
			td.Annotations.ReadOnlyHint = annReadOnly.Bool
		}
		if annDestructive.Valid {
			td.Annotations.DestructiveHint = &annDestructive.Bool
		}
		if annIdempotent.Valid {
			td.Annotations.IdempotentHint = annIdempotent.Bool
		}
		if annOpenWorld.Valid {
			td.Annotations.OpenWorldHint = &annOpenWorld.Bool
		}

		toolDefs = append(toolDefs, td)
	}

	return toolDefs, rows.Err()
}

func (s *Store) GetSessions(ctx context.Context) ([]*session.Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM sessions ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*session.Session
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}

		sess, err := s.loadSession(ctx, id)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}

	return sessions, rows.Err()
}

func (s *Store) GetSessionSummaries(ctx context.Context) ([]session.Summary, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, created_at, starred FROM sessions ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []session.Summary
	for rows.Next() {
		var summary session.Summary
		var starred int

		if err := rows.Scan(&summary.ID, &summary.Title, &summary.CreatedAt, &starred); err != nil {
			return nil, err
		}
		summary.Starred = starred != 0
		summaries = append(summaries, summary)
	}

	return summaries, rows.Err()
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	if id == "" {
		return session.ErrEmptyID
	}

	result, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
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

func (s *Store) UpdateSession(ctx context.Context, sess *session.Session) error {
	if sess.ID == "" {
		return session.ErrEmptyID
	}

	permissionsJSON, err := marshalNullableJSON(sess.Permissions)
	if err != nil {
		return err
	}

	agentModelOverridesJSON, err := marshalMapJSON(sess.AgentModelOverrides)
	if err != nil {
		return err
	}

	customModelsUsedJSON, err := marshalSliceJSON(sess.CustomModelsUsed)
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET
			title = ?,
			tools_approved = ?,
			thinking = ?,
			hide_tool_results = ?,
			working_dir = ?,
			max_iterations = ?,
			starred = ?,
			input_tokens = ?,
			output_tokens = ?,
			cost = ?,
			permissions = ?,
			agent_model_overrides = ?,
			custom_models_used = ?
		WHERE id = ?`,
		sess.Title, boolToInt(sess.ToolsApproved), boolToInt(sess.Thinking),
		boolToInt(sess.HideToolResults), sess.WorkingDir, sess.MaxIterations,
		boolToInt(sess.Starred), sess.InputTokens, sess.OutputTokens, sess.Cost,
		permissionsJSON, agentModelOverridesJSON, customModelsUsedJSON, sess.ID,
	)
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

func (s *Store) SetSessionStarred(ctx context.Context, id string, starred bool) error {
	if id == "" {
		return session.ErrEmptyID
	}

	result, err := s.db.ExecContext(ctx, `UPDATE sessions SET starred = ? WHERE id = ?`, boolToInt(starred), id)
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

func (s *Store) AddSummary(ctx context.Context, summary *string) error {
	// Note: This interface signature is incomplete - it doesn't specify which session
	// to add the summary to. This is a no-op until the interface is clarified.
	return nil
}

func (s *Store) AddSubSession(ctx context.Context, parent, child *session.Session) error {
	if parent.ID == "" || child.ID == "" {
		return session.ErrEmptyID
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Insert the child session
	if err := s.insertSession(ctx, tx, child); err != nil {
		return err
	}

	// Insert all items for the child session
	for i, item := range child.Messages {
		if err := s.insertSessionItem(ctx, tx, child.ID, i, &item); err != nil {
			return err
		}
	}

	// Get the next position for the parent session
	var maxPosition sql.NullInt64
	err = tx.QueryRowContext(ctx,
		`SELECT MAX(position) FROM session_items WHERE session_id = ?`, parent.ID).Scan(&maxPosition)
	if err != nil {
		return err
	}

	nextPosition := 0
	if maxPosition.Valid {
		nextPosition = int(maxPosition.Int64) + 1
	}

	// Insert the sub-session reference in the parent
	_, err = tx.ExecContext(ctx,
		`INSERT INTO session_items (session_id, position, item_type, sub_session_id)
		 VALUES (?, ?, 'sub_session', ?)`,
		parent.ID, nextPosition, child.ID,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) CreateMessage(ctx context.Context, sess *session.Session, msg *session.Message) error {
	if sess.ID == "" {
		return session.ErrEmptyID
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Get the next position for this session
	var maxPosition sql.NullInt64
	err = tx.QueryRowContext(ctx,
		`SELECT MAX(position) FROM session_items WHERE session_id = ?`, sess.ID).Scan(&maxPosition)
	if err != nil {
		return err
	}

	nextPosition := 0
	if maxPosition.Valid {
		nextPosition = int(maxPosition.Int64) + 1
	}

	item := session.Item{Message: msg}
	if err := s.insertSessionItem(ctx, tx, sess.ID, nextPosition, &item); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) UpdateMessage(ctx context.Context, sess *session.Session, msg *session.Message) error {
	if sess.ID == "" {
		return session.ErrEmptyID
	}
	if msg.ID == "" {
		return fmt.Errorf("message ID cannot be empty")
	}

	messageID, err := parseMessageID(msg.ID)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	chatMsg := &msg.Message

	var functionCallName, functionCallArgs *string
	if chatMsg.FunctionCall != nil {
		functionCallName = &chatMsg.FunctionCall.Name
		functionCallArgs = &chatMsg.FunctionCall.Arguments
	}

	var usageInput, usageOutput, usageCachedInput, usageCacheWrite, usageReasoning *int64
	if chatMsg.Usage != nil {
		usageInput = &chatMsg.Usage.InputTokens
		usageOutput = &chatMsg.Usage.OutputTokens
		usageCachedInput = &chatMsg.Usage.CachedInputTokens
		usageCacheWrite = &chatMsg.Usage.CacheWriteTokens
		usageReasoning = &chatMsg.Usage.ReasoningTokens
	}

	result, err := tx.ExecContext(ctx,
		`UPDATE messages SET
			agent_name = ?,
			implicit = ?,
			role = ?,
			content = ?,
			reasoning_content = ?,
			thinking_signature = ?,
			thought_signature = ?,
			tool_call_id = ?,
			created_at = ?,
			model = ?,
			cost = ?,
			cache_control = ?,
			usage_input_tokens = ?,
			usage_output_tokens = ?,
			usage_cached_input_tokens = ?,
			usage_cache_write_tokens = ?,
			usage_reasoning_tokens = ?,
			function_call_name = ?,
			function_call_arguments = ?
		WHERE id = ?`,
		msg.AgentName, boolToInt(msg.Implicit), string(chatMsg.Role),
		chatMsg.Content, nullString(chatMsg.ReasoningContent), nullString(chatMsg.ThinkingSignature),
		chatMsg.ThoughtSignature, nullString(chatMsg.ToolCallID), nullString(chatMsg.CreatedAt),
		nullString(chatMsg.Model), chatMsg.Cost, boolToInt(chatMsg.CacheControl),
		usageInput, usageOutput, usageCachedInput, usageCacheWrite, usageReasoning,
		functionCallName, functionCallArgs,
		msg.ID,
	)
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

	if err := s.upsertMessageParts(ctx, tx, messageID, chatMsg.MultiContent); err != nil {
		return err
	}

	if err := s.upsertToolCalls(ctx, tx, messageID, chatMsg.ToolCalls); err != nil {
		return err
	}

	if err := s.upsertToolDefinitions(ctx, tx, messageID, chatMsg.ToolDefinitions); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) upsertMessageParts(ctx context.Context, tx *sql.Tx, messageID int64, parts []chat.MessagePart) error {
	for i, part := range parts {
		var imageURL, imageURLDetail *string
		if part.ImageURL != nil {
			imageURL = &part.ImageURL.URL
			if part.ImageURL.Detail != "" {
				detail := string(part.ImageURL.Detail)
				imageURLDetail = &detail
			}
		}

		_, err := tx.ExecContext(ctx,
			`INSERT INTO message_parts (message_id, position, part_type, text, image_url, image_url_detail)
			 VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT(message_id, position) DO UPDATE SET
				part_type = excluded.part_type,
				text = excluded.text,
				image_url = excluded.image_url,
				image_url_detail = excluded.image_url_detail`,
			messageID, i, string(part.Type), nullString(part.Text), imageURL, imageURLDetail,
		)
		if err != nil {
			return err
		}
	}

	// Remove any extra parts beyond current length
	_, err := tx.ExecContext(ctx,
		`DELETE FROM message_parts WHERE message_id = ? AND position >= ?`,
		messageID, len(parts),
	)
	return err
}

func (s *Store) upsertToolCalls(ctx context.Context, tx *sql.Tx, messageID int64, toolCalls []tools.ToolCall) error {
	for i, tc := range toolCalls {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO message_tool_calls (message_id, position, tool_call_id, tool_type, function_name, function_arguments)
			 VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT(message_id, position) DO UPDATE SET
				tool_call_id = excluded.tool_call_id,
				tool_type = excluded.tool_type,
				function_name = excluded.function_name,
				function_arguments = excluded.function_arguments`,
			messageID, i, nullString(tc.ID), nullString(string(tc.Type)),
			nullString(tc.Function.Name), nullString(tc.Function.Arguments),
		)
		if err != nil {
			return err
		}
	}

	// Remove any extra tool calls beyond current length
	_, err := tx.ExecContext(ctx,
		`DELETE FROM message_tool_calls WHERE message_id = ? AND position >= ?`,
		messageID, len(toolCalls),
	)
	return err
}

func (s *Store) upsertToolDefinitions(ctx context.Context, tx *sql.Tx, messageID int64, toolDefs []tools.Tool) error {
	for i, td := range toolDefs {
		var paramsSchema, outputSchema *string
		if td.Parameters != nil {
			data, err := json.Marshal(td.Parameters)
			if err != nil {
				return err
			}
			str := string(data)
			paramsSchema = &str
		}
		if td.OutputSchema != nil {
			data, err := json.Marshal(td.OutputSchema)
			if err != nil {
				return err
			}
			str := string(data)
			outputSchema = &str
		}

		_, err := tx.ExecContext(ctx,
			`INSERT INTO message_tool_definitions (
				message_id, position, name, category, description, parameters_schema, output_schema,
				annotation_title, annotation_read_only_hint, annotation_destructive_hint,
				annotation_idempotent_hint, annotation_open_world_hint
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(message_id, position) DO UPDATE SET
				name = excluded.name,
				category = excluded.category,
				description = excluded.description,
				parameters_schema = excluded.parameters_schema,
				output_schema = excluded.output_schema,
				annotation_title = excluded.annotation_title,
				annotation_read_only_hint = excluded.annotation_read_only_hint,
				annotation_destructive_hint = excluded.annotation_destructive_hint,
				annotation_idempotent_hint = excluded.annotation_idempotent_hint,
				annotation_open_world_hint = excluded.annotation_open_world_hint`,
			messageID, i, td.Name, nullString(td.Category), nullString(td.Description),
			paramsSchema, outputSchema,
			nullString(td.Annotations.Title), td.Annotations.ReadOnlyHint,
			td.Annotations.DestructiveHint, td.Annotations.IdempotentHint,
			td.Annotations.OpenWorldHint,
		)
		if err != nil {
			return err
		}
	}

	// Remove any extra tool definitions beyond current length
	_, err := tx.ExecContext(ctx,
		`DELETE FROM message_tool_definitions WHERE message_id = ? AND position >= ?`,
		messageID, len(toolDefs),
	)
	return err
}

func parseMessageID(id string) (int64, error) {
	var messageID int64
	_, err := fmt.Sscanf(id, "%d", &messageID)
	if err != nil {
		return 0, fmt.Errorf("invalid message ID: %s", id)
	}
	return messageID, nil
}

// Helper functions

func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func marshalNullableJSON(v any) (*string, error) {
	if v == nil {
		return nil, nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	s := string(data)
	return &s, nil
}

func marshalMapJSON(m map[string]string) (string, error) {
	if len(m) == 0 {
		return "{}", nil
	}
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func marshalSliceJSON(s []string) (string, error) {
	if len(s) == 0 {
		return "[]", nil
	}
	data, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
