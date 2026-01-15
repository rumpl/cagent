package storev2

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/sqliteutil"
)

// NewWithAutoMigration opens a v2 store at path.
// If the existing database at path uses the legacy (v1) schema, it is
// automatically migrated to v2 in-place.
func NewWithAutoMigration(ctx context.Context, path string) (*Store, error) {
	store, err := New(path)
	if err == nil {
		return store, nil
	}

	isV1, detectErr := isV1Store(ctx, path)
	if detectErr != nil {
		return nil, err
	}
	if !isV1 {
		return nil, err
	}

	if migrateErr := migrateV1ToV2(ctx, path); migrateErr != nil {
		return nil, fmt.Errorf("migrating legacy session store: %w", migrateErr)
	}

	return New(path)
}

func isV1Store(ctx context.Context, path string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	db, err := sqliteutil.OpenDB(path)
	if err != nil {
		return false, err
	}
	defer db.Close()

	var tableCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name='sessions'`).Scan(&tableCount); err != nil {
		return false, err
	}
	if tableCount == 0 {
		return false, nil
	}

	rows, err := db.QueryContext(ctx, `PRAGMA table_info(sessions)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid     int
			name    string
			typeStr string
			notNull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &typeStr, &notNull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == "messages" {
			return true, nil
		}
	}

	return false, rows.Err()
}

func migrateV1ToV2(ctx context.Context, path string) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	backupPath := filepath.Join(dir, base+".v1.bak")
	if _, err := os.Stat(backupPath); err == nil {
		backupPath = filepath.Join(dir, fmt.Sprintf("%s.v1.bak.%s", base, time.Now().UTC().Format("20060102-150405")))
	}

	if err := os.Rename(path, backupPath); err != nil {
		return fmt.Errorf("renaming legacy db: %w", err)
	}

	srcStore, err := session.NewSQLiteSessionStore(backupPath)
	if err != nil {
		_ = os.Rename(backupPath, path)
		return fmt.Errorf("opening legacy store: %w", err)
	}
	srcSQLite, ok := srcStore.(*session.SQLiteSessionStore)
	if ok {
		defer srcSQLite.Close()
	}

	dstStore, err := New(path)
	if err != nil {
		_ = os.Rename(backupPath, path)
		return fmt.Errorf("creating v2 store: %w", err)
	}
	defer dstStore.Close()

	sessions, err := srcStore.GetSessions(ctx)
	if err != nil {
		return fmt.Errorf("listing legacy sessions: %w", err)
	}

	for _, sess := range sessions {
		if _, err := dstStore.GetSession(ctx, sess.ID); err == nil {
			continue
		}

		if err := dstStore.AddSession(ctx, sess); err != nil {
			if isUniqueConstraintError(err) {
				continue
			}
			return fmt.Errorf("migrating session %s: %w", sess.ID, err)
		}
	}

	return nil
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") || strings.Contains(msg, "constraint failed")
}
