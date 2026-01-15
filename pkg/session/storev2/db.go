package storev2

import (
	"database/sql"
	"embed"

	"github.com/pressly/goose/v3"

	"github.com/docker/cagent/pkg/sqliteutil"
)

//go:embed migrations/*.sql
var embedMigrations embed.FS

func openDB(path string) (*sql.DB, error) {
	db, err := sqliteutil.OpenDB(path)
	if err != nil {
		return nil, err
	}

	goose.SetBaseFS(embedMigrations)

	if err := goose.SetDialect("sqlite3"); err != nil {
		db.Close()
		return nil, err
	}

	if err := goose.Up(db, "migrations"); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}
