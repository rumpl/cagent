package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// MigrationFunc is a custom migration function for complex migrations
type MigrationFunc func(ctx context.Context, tx *sql.Tx) error

// Migration represents a database migration
type Migration struct {
	ID          int
	Name        string
	Description string
	UpSQL       string
	DownSQL     string
	UpFunc      MigrationFunc // Custom function for complex migrations (runs after UpSQL if both present)
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
		applied, err := m.isMigrationApplied(ctx, migration.ID, migration.Name)
		if err != nil {
			return fmt.Errorf("failed to check if migration %s is applied: %w", migration.Name, err)
		}

		if !applied {
			err = m.applyMigration(ctx, &migration)
			if err != nil {
				return fmt.Errorf("failed to apply migration %s: %w", migration.Name, err)
			}
		}
	}

	return nil
}

// isMigrationApplied checks if a migration has already been applied (by name or ID)
func (m *MigrationManager) isMigrationApplied(ctx context.Context, id int, name string) (bool, error) {
	var count int
	err := m.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM migrations WHERE name = ? OR id = ?", name, id).Scan(&count)
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

	// Execute SQL migration if present
	if migration.UpSQL != "" {
		_, err = tx.ExecContext(ctx, migration.UpSQL)
		if err != nil {
			return fmt.Errorf("failed to execute migration SQL: %w", err)
		}
	}

	// Execute custom function if present
	if migration.UpFunc != nil {
		if err = migration.UpFunc(ctx, tx); err != nil {
			return fmt.Errorf("failed to execute migration function: %w", err)
		}
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
			Name:        "014_create_session_items_table",
			Description: "Create session_items table for storing messages, sub-sessions, and summaries separately",
			UpSQL: `CREATE TABLE IF NOT EXISTS session_items (
				id TEXT PRIMARY KEY,
				session_id TEXT NOT NULL,
				position INTEGER NOT NULL,
				item_type TEXT NOT NULL CHECK (item_type IN ('message', 'sub_session', 'summary')),
				message_data TEXT,
				sub_session_id TEXT,
				summary TEXT,
				created_at TEXT NOT NULL,
				FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
			);
			CREATE INDEX IF NOT EXISTS idx_session_items_session_id ON session_items(session_id);
			CREATE INDEX IF NOT EXISTS idx_session_items_position ON session_items(session_id, position);`,
			DownSQL: `DROP INDEX IF EXISTS idx_session_items_position;
			DROP INDEX IF EXISTS idx_session_items_session_id;
			DROP TABLE IF EXISTS session_items;`,
		},
		{
			ID:          15,
			Name:        "015_migrate_messages_to_session_items",
			Description: "Migrate data from legacy messages column to session_items table",
			UpFunc:      migrateMessagesToSessionItems,
		},
		{
			ID:          16,
			Name:        "016_drop_messages_column",
			Description: "Drop the legacy messages column from sessions table (data now in session_items)",
			UpSQL:       `ALTER TABLE sessions DROP COLUMN messages`,
			DownSQL:     `ALTER TABLE sessions ADD COLUMN messages TEXT DEFAULT '[]'`,
		},
		{
			ID:          17,
			Name:        "017_add_parent_id_column",
			Description: "Add parent_id column to sessions table for sub-session hierarchy",
			UpSQL:       `ALTER TABLE sessions ADD COLUMN parent_id TEXT DEFAULT ''`,
			DownSQL:     `ALTER TABLE sessions DROP COLUMN parent_id`,
		},
	}
}

// migrateMessagesToSessionItems migrates data from the legacy messages JSON column
// to the new session_items table for all existing sessions.
func migrateMessagesToSessionItems(ctx context.Context, tx *sql.Tx) error {
	// Get all sessions with their messages
	rows, err := tx.QueryContext(ctx, "SELECT id, messages FROM sessions WHERE messages IS NOT NULL AND messages != '[]'")
	if err != nil {
		return fmt.Errorf("querying sessions: %w", err)
	}
	defer rows.Close()

	type sessionData struct {
		id       string
		messages string
	}
	var sessions []sessionData

	for rows.Next() {
		var sd sessionData
		if err := rows.Scan(&sd.id, &sd.messages); err != nil {
			return fmt.Errorf("scanning session: %w", err)
		}
		sessions = append(sessions, sd)
	}
	rows.Close()

	for _, sd := range sessions {
		// Check if this session already has items in session_items
		var count int
		err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM session_items WHERE session_id = ?", sd.id).Scan(&count)
		if err != nil {
			return fmt.Errorf("checking existing items for session %s: %w", sd.id, err)
		}
		if count > 0 {
			// Session already has items, skip
			slog.Debug("Skipping session migration, already has items", "session_id", sd.id, "item_count", count)
			continue
		}

		// Parse the messages JSON - try new Item format first, then legacy Message format
		var items []Item
		if err := json.Unmarshal([]byte(sd.messages), &items); err != nil {
			return fmt.Errorf("parsing messages for session %s: %w", sd.id, err)
		}

		// Check if this is legacy format (Message structs directly) or new format (Item wrappers)
		// Legacy format will result in items with nil Message pointers
		isLegacyFormat := len(items) > 0 && items[0].Message == nil
		if isLegacyFormat {
			// Try parsing as legacy Message format
			var messages []Message
			if err := json.Unmarshal([]byte(sd.messages), &messages); err != nil {
				return fmt.Errorf("parsing legacy messages for session %s: %w", sd.id, err)
			}
			items = convertMessagesToItems(messages)
		}

		// Insert items into session_items
		for i, item := range items {
			itemID := item.ID
			if itemID == "" {
				itemID = generateItemID()
			}

			itemType, messageData, subSessionID, summary := serializeItem(&item)

			_, err = tx.ExecContext(ctx,
				`INSERT INTO session_items (id, session_id, position, item_type, message_data, sub_session_id, summary, created_at)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				itemID, sd.id, i, string(itemType), messageData, subSessionID, summary, time.Now().Format(time.RFC3339))
			if err != nil {
				return fmt.Errorf("inserting item for session %s: %w", sd.id, err)
			}
		}

		slog.Debug("Migrated session messages to session_items", "session_id", sd.id, "item_count", len(items))
	}

	return nil
}
