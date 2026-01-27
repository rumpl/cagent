package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Migration represents a database migration
type Migration struct {
	ID          int
	Name        string
	Description string
	UpSQL       string
	DownSQL     string
	AppliedAt   time.Time
}

// MigrationManager handles database migrations
type MigrationManager struct {
	db *sql.DB
}

// NewMigrationManager creates a new migration manager
func NewMigrationManager(db *sql.DB) *MigrationManager {
	return &MigrationManager{db: db}
}

// InitializeMigrations sets up the migrations table and runs pending migrations
func (m *MigrationManager) InitializeMigrations(ctx context.Context) error {
	// Create migrations table if it doesn't exist
	err := m.createMigrationsTable(ctx)
	if err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Run all pending migrations
	err = m.RunPendingMigrations(ctx)
	if err != nil {
		return fmt.Errorf("failed to run pending migrations: %w", err)
	}

	return nil
}

// createMigrationsTable creates the migrations tracking table
func (m *MigrationManager) createMigrationsTable(ctx context.Context) error {
	_, err := m.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS migrations (
			id INTEGER PRIMARY KEY,
			name TEXT UNIQUE NOT NULL,
			description TEXT,
			applied_at TEXT NOT NULL
		)
	`)
	return err
}

// RunPendingMigrations executes all migrations that haven't been applied yet
func (m *MigrationManager) RunPendingMigrations(ctx context.Context) error {
	migrations := getAllMigrations()

	for _, migration := range migrations {
		applied, err := m.isMigrationApplied(ctx, migration.Name)
		if err != nil {
			return fmt.Errorf("failed to check if migration %s is applied: %w", migration.Name, err)
		}

		if !applied {
			err = m.applyMigration(ctx, &migration)
			if err != nil {
				return fmt.Errorf("failed to apply migration %s: %w", migration.Name, err)
			}

			// Run data migration after the normalized tables migration
			if migration.Name == "014_create_normalized_session_tables" {
				if err := migrateSessionData(m.db); err != nil {
					return fmt.Errorf("failed to migrate session data: %w", err)
				}
			}
		}
	}

	return nil
}

// isMigrationApplied checks if a migration has already been applied
func (m *MigrationManager) isMigrationApplied(ctx context.Context, name string) (bool, error) {
	var count int
	err := m.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM migrations WHERE name = ?", name).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// applyMigration applies a single migration
func (m *MigrationManager) applyMigration(ctx context.Context, migration *Migration) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		// TODO: handle error
		_ = tx.Rollback()
	}()

	_, err = tx.ExecContext(ctx, migration.UpSQL)
	if err != nil {
		return fmt.Errorf("failed to execute migration SQL: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		"INSERT INTO migrations (id, name, description, applied_at) VALUES (?, ?, ?, ?)",
		migration.ID, migration.Name, migration.Description, time.Now().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("failed to commit migration transaction: %w", err)
	}

	return nil
}

// GetAppliedMigrations returns a list of applied migrations
func (m *MigrationManager) GetAppliedMigrations(ctx context.Context) ([]Migration, error) {
	rows, err := m.db.QueryContext(ctx, "SELECT id, name, description, applied_at FROM migrations ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var migrations []Migration
	for rows.Next() {
		var migration Migration
		var appliedAtStr string

		err := rows.Scan(&migration.ID, &migration.Name, &migration.Description, &appliedAtStr)
		if err != nil {
			return nil, err
		}

		migration.AppliedAt, err = time.Parse(time.RFC3339, appliedAtStr)
		if err != nil {
			return nil, err
		}

		migrations = append(migrations, migration)
	}

	return migrations, nil
}

// getAllMigrations returns all available migrations in order
func getAllMigrations() []Migration {
	return []Migration{
		{
			ID:          1,
			Name:        "001_add_tools_approved_column",
			Description: "Add tools_approved column to sessions table",
			UpSQL:       `ALTER TABLE sessions ADD COLUMN tools_approved BOOLEAN DEFAULT 0`,
			DownSQL:     `ALTER TABLE sessions DROP COLUMN tools_approved`,
		},
		{
			ID:          2,
			Name:        "002_add_usage_column",
			Description: "Add usage column to sessions table",
			UpSQL:       `ALTER TABLE sessions ADD COLUMN input_tokens INTEGER DEFAULT 0`,
			DownSQL:     `ALTER TABLE sessions DROP COLUMN input_tokens`,
		},
		{
			ID:          3,
			Name:        "003_add_output_tokens_column",
			Description: "Add output_tokens column to sessions table",
			UpSQL:       `ALTER TABLE sessions ADD COLUMN output_tokens INTEGER DEFAULT 0`,
			DownSQL:     `ALTER TABLE sessions DROP COLUMN output_tokens`,
		},
		{
			ID:          4,
			Name:        "004_add_title_column",
			Description: "Add title column to sessions table",
			UpSQL:       `ALTER TABLE sessions ADD COLUMN title TEXT DEFAULT ''`,
			DownSQL:     `ALTER TABLE sessions DROP COLUMN title`,
		},
		{
			ID:          5,
			Name:        "005_add_cost_column",
			Description: "Add cost column to sessions table",
			UpSQL:       `ALTER TABLE sessions ADD COLUMN cost REAL DEFAULT 0`,
			DownSQL:     `ALTER TABLE sessions DROP COLUMN cost`,
		},
		{
			ID:          6,
			Name:        "006_add_send_user_message_column",
			Description: "Add send_user_message column to sessions table",
			UpSQL:       `ALTER TABLE sessions ADD COLUMN send_user_message BOOLEAN DEFAULT 1`,
			DownSQL:     `ALTER TABLE sessions DROP COLUMN send_user_message`,
		},
		{
			ID:          7,
			Name:        "007_add_max_iterations_column",
			Description: "Add max_iterations column to sessions table",
			UpSQL:       `ALTER TABLE sessions ADD COLUMN max_iterations INTEGER DEFAULT 0`,
			DownSQL:     `ALTER TABLE sessions DROP COLUMN max_iterations`,
		},
		{
			ID:          8,
			Name:        "008_add_working_dir_column",
			Description: "Add working_dir column to sessions table",
			UpSQL:       `ALTER TABLE sessions ADD COLUMN working_dir TEXT DEFAULT ''`,
			DownSQL:     `ALTER TABLE sessions DROP COLUMN working_dir`,
		},
		{
			ID:          9,
			Name:        "009_add_starred_column",
			Description: "Add starred column to sessions table",
			UpSQL:       `ALTER TABLE sessions ADD COLUMN starred BOOLEAN DEFAULT 0`,
			DownSQL:     `ALTER TABLE sessions DROP COLUMN starred`,
		},
		{
			ID:          10,
			Name:        "010_add_permissions_column",
			Description: "Add permissions column to sessions table for session-level permission overrides",
			UpSQL:       `ALTER TABLE sessions ADD COLUMN permissions TEXT DEFAULT ''`,
			DownSQL:     `ALTER TABLE sessions DROP COLUMN permissions`,
		},
		{
			ID:          11,
			Name:        "011_add_agent_model_overrides_column",
			Description: "Add agent_model_overrides column to sessions table for per-session model switching",
			UpSQL:       `ALTER TABLE sessions ADD COLUMN agent_model_overrides TEXT DEFAULT '{}'`,
			DownSQL:     `ALTER TABLE sessions DROP COLUMN agent_model_overrides`,
		},
		{
			ID:          12,
			Name:        "012_add_custom_models_used_column",
			Description: "Add custom_models_used column to sessions table for tracking custom models used in session",
			UpSQL:       `ALTER TABLE sessions ADD COLUMN custom_models_used TEXT DEFAULT '[]'`,
			DownSQL:     `ALTER TABLE sessions DROP COLUMN custom_models_used`,
		},
		{
			ID:          13,
			Name:        "013_add_thinking_column",
			Description: "Add thinking column to sessions table for session-level thinking toggle (default enabled)",
			UpSQL:       `ALTER TABLE sessions ADD COLUMN thinking BOOLEAN DEFAULT 1`,
			DownSQL:     `ALTER TABLE sessions DROP COLUMN thinking`,
		},
		{
			ID:          14,
			Name:        "014_create_normalized_session_tables",
			Description: "Create normalized tables for messages, sub-sessions, and summaries",
			UpSQL: `
				CREATE TABLE IF NOT EXISTS session_messages (
					id TEXT PRIMARY KEY,
					session_id TEXT NOT NULL,
					position INTEGER NOT NULL,
					agent_name TEXT,
					message TEXT NOT NULL,
					implicit BOOLEAN DEFAULT 0,
					created_at TEXT,
					FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
				);
				CREATE INDEX IF NOT EXISTS idx_session_messages_session_id ON session_messages(session_id);
				CREATE INDEX IF NOT EXISTS idx_session_messages_position ON session_messages(session_id, position);

				CREATE TABLE IF NOT EXISTS session_subsessions (
					id TEXT PRIMARY KEY,
					parent_session_id TEXT NOT NULL,
					position INTEGER NOT NULL,
					created_at TEXT,
					FOREIGN KEY (parent_session_id) REFERENCES sessions(id) ON DELETE CASCADE
				);
				CREATE INDEX IF NOT EXISTS idx_session_subsessions_parent ON session_subsessions(parent_session_id);

				CREATE TABLE IF NOT EXISTS session_summaries (
					id TEXT PRIMARY KEY,
					session_id TEXT NOT NULL,
					position INTEGER NOT NULL,
					summary TEXT NOT NULL,
					created_at TEXT,
					FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
				);
				CREATE INDEX IF NOT EXISTS idx_session_summaries_session ON session_summaries(session_id);
			`,
			DownSQL: `
				DROP TABLE IF EXISTS session_summaries;
				DROP TABLE IF EXISTS session_subsessions;
				DROP TABLE IF EXISTS session_messages;
			`,
		},
	}
}

// migrateSessionData migrates existing session data from JSON blob to normalized tables.
// This is called after the schema migration creates the new tables.
func migrateSessionData(db *sql.DB) error {
	ctx := context.Background()

	// Check if migration is needed by seeing if there are sessions with messages in JSON but not in normalized table
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sessions s 
		WHERE s.messages != '[]' AND s.messages != '' AND s.messages IS NOT NULL
		AND NOT EXISTS (SELECT 1 FROM session_messages sm WHERE sm.session_id = s.id)
	`).Scan(&count)
	if err != nil {
		return fmt.Errorf("checking for sessions to migrate: %w", err)
	}

	if count == 0 {
		return nil // No migration needed
	}

	// Get all sessions that need migration
	rows, err := db.QueryContext(ctx, `
		SELECT id, messages FROM sessions 
		WHERE messages != '[]' AND messages != '' AND messages IS NOT NULL
		AND NOT EXISTS (SELECT 1 FROM session_messages sm WHERE sm.session_id = id)
	`)
	if err != nil {
		return fmt.Errorf("querying sessions to migrate: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var sessionID, messagesJSON string
		if err := rows.Scan(&sessionID, &messagesJSON); err != nil {
			return fmt.Errorf("scanning session row: %w", err)
		}

		if err := migrateSessionMessages(ctx, db, sessionID, messagesJSON); err != nil {
			return fmt.Errorf("migrating session %s: %w", sessionID, err)
		}
	}

	return rows.Err()
}

func migrateSessionMessages(ctx context.Context, db *sql.DB, sessionID, messagesJSON string) error {
	// Parse the messages JSON - handle both legacy and new formats
	var items []itemForMigration
	if err := json.Unmarshal([]byte(messagesJSON), &items); err != nil {
		return fmt.Errorf("parsing messages JSON: %w", err)
	}

	// Check if this is legacy format (direct Message structs)
	var legacyMessages []messageForMigration
	if err := json.Unmarshal([]byte(messagesJSON), &legacyMessages); err == nil {
		if len(legacyMessages) > 0 && (legacyMessages[0].AgentName != "" || legacyMessages[0].Message.Content != "" || legacyMessages[0].Message.Role != "") {
			// Convert legacy format to items
			items = make([]itemForMigration, len(legacyMessages))
			for i, msg := range legacyMessages {
				items[i] = itemForMigration{Message: &msg}
			}
		}
	}

	return migrateItems(ctx, db, sessionID, items)
}

func migrateItems(ctx context.Context, db *sql.DB, sessionID string, items []itemForMigration) error {
	for position, item := range items {
		switch {
		case item.Message != nil:
			msgJSON, err := json.Marshal(item.Message.Message)
			if err != nil {
				return fmt.Errorf("marshaling message: %w", err)
			}

			msgID := generateMigrationID()
			_, err = db.ExecContext(ctx, `
				INSERT INTO session_messages (id, session_id, position, agent_name, message, implicit, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?)
			`, msgID, sessionID, position, item.Message.AgentName, string(msgJSON), item.Message.Implicit, item.Message.Message.CreatedAt)
			if err != nil {
				return fmt.Errorf("inserting message: %w", err)
			}
		case item.SubSession != nil:
			// Create the sub-session record in sessions table first (if it doesn't exist)
			subSessionJSON, err := json.Marshal(item.SubSession.Messages)
			if err != nil {
				return fmt.Errorf("marshaling sub-session messages: %w", err)
			}

			// Insert sub-session as a session record
			_, err = db.ExecContext(ctx, `
				INSERT OR IGNORE INTO sessions (id, messages, created_at, title, tools_approved, thinking, input_tokens, output_tokens, cost, send_user_message, max_iterations, working_dir, starred, permissions, agent_model_overrides, custom_models_used)
				VALUES (?, '[]', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', '{}', '[]')
			`, item.SubSession.ID, item.SubSession.CreatedAt.Format(time.RFC3339), item.SubSession.Title, item.SubSession.ToolsApproved, item.SubSession.Thinking, item.SubSession.InputTokens, item.SubSession.OutputTokens, item.SubSession.Cost, item.SubSession.SendUserMessage, item.SubSession.MaxIterations, item.SubSession.WorkingDir, item.SubSession.Starred)
			if err != nil {
				return fmt.Errorf("inserting sub-session: %w", err)
			}

			// Add the sub-session marker
			_, err = db.ExecContext(ctx, `
				INSERT INTO session_subsessions (id, parent_session_id, position, created_at)
				VALUES (?, ?, ?, ?)
			`, item.SubSession.ID, sessionID, position, item.SubSession.CreatedAt.Format(time.RFC3339))
			if err != nil {
				return fmt.Errorf("inserting sub-session marker: %w", err)
			}

			// Recursively migrate sub-session's messages
			var subItems []itemForMigration
			if err := json.Unmarshal(subSessionJSON, &subItems); err != nil {
				return fmt.Errorf("parsing sub-session messages: %w", err)
			}
			if err := migrateItems(ctx, db, item.SubSession.ID, subItems); err != nil {
				return fmt.Errorf("migrating sub-session messages: %w", err)
			}
		case item.Summary != "":
			summaryID := generateMigrationID()
			_, err := db.ExecContext(ctx, `
				INSERT INTO session_summaries (id, session_id, position, summary, created_at)
				VALUES (?, ?, ?, ?, ?)
			`, summaryID, sessionID, position, item.Summary, time.Now().Format(time.RFC3339))
			if err != nil {
				return fmt.Errorf("inserting summary: %w", err)
			}
		}
	}
	return nil
}

// Types for migration parsing
type itemForMigration struct {
	Message    *messageForMigration `json:"message,omitempty"`
	SubSession *sessionForMigration `json:"sub_session,omitempty"`
	Summary    string               `json:"summary,omitempty"`
}

type messageForMigration struct {
	AgentName string             `json:"agentName"`
	Message   chatMessageCompact `json:"message"`
	Implicit  bool               `json:"implicit,omitempty"`
}

type chatMessageCompact struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at,omitempty"`
}

type sessionForMigration struct {
	ID              string             `json:"id"`
	Title           string             `json:"title"`
	Messages        []itemForMigration `json:"messages"`
	CreatedAt       time.Time          `json:"created_at"`
	ToolsApproved   bool               `json:"tools_approved"`
	Thinking        bool               `json:"thinking"`
	InputTokens     int64              `json:"input_tokens"`
	OutputTokens    int64              `json:"output_tokens"`
	Cost            float64            `json:"cost"`
	SendUserMessage bool               `json:"send_user_message"`
	MaxIterations   int                `json:"max_iterations"`
	WorkingDir      string             `json:"working_dir,omitempty"`
	Starred         bool               `json:"starred"`
}

func generateMigrationID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
