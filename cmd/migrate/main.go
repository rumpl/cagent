package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/session/storev2"
)

func main() {
	srcPath := flag.String("src", "", "Source database path (v1 store)")
	dstPath := flag.String("dst", "", "Destination database path (v2 store)")
	flag.Parse()

	if *srcPath == "" || *dstPath == "" {
		fmt.Fprintln(os.Stderr, "Usage: migrate -src <v1-database> -dst <v2-database>")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if err := migrate(*srcPath, *dstPath); err != nil {
		log.Fatal(err)
	}
}

func migrate(srcPath, dstPath string) error {
	ctx := context.Background()

	// Open source (v1) store
	srcStore, err := session.NewSQLiteSessionStore(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source store: %w", err)
	}
	defer srcStore.(*session.SQLiteSessionStore).Close()

	// Create destination (v2) store
	dstStore, err := storev2.New(dstPath)
	if err != nil {
		return fmt.Errorf("failed to create destination store: %w", err)
	}
	defer dstStore.Close()

	// Get all sessions from source
	sessions, err := srcStore.GetSessions(ctx)
	if err != nil {
		return fmt.Errorf("failed to get sessions from source: %w", err)
	}

	fmt.Printf("Migrating %d sessions...\n", len(sessions))

	// Migrate each session
	var migrated, skipped, failed int
	for _, sess := range sessions {
		// Check if session already exists (it might be a sub-session already migrated as part of a parent)
		existing, err := dstStore.GetSession(ctx, sess.ID)
		if err == nil {
			// Session already exists, skip it (likely a sub-session)
			skipped++
			fmt.Printf("  Skipped session (already exists): %s (%s)\n", sess.ID, sess.Title)
			continue
		}
		if !errors.Is(err, session.ErrNotFound) {
			fmt.Fprintf(os.Stderr, "  Failed to check session %s: %v (existing: %v)\n", sess.ID, err, existing)
			failed++
			continue
		}

		if err := dstStore.AddSession(ctx, sess); err != nil {
			// Check if this is a duplicate error - might be a sub-session
			if isUniqueConstraintError(err) {
				skipped++
				fmt.Printf("  Skipped session (sub-session of another): %s (%s)\n", sess.ID, sess.Title)
				continue
			}
			fmt.Fprintf(os.Stderr, "  Failed to migrate session %s: %v\n", sess.ID, err)
			failed++
			continue
		}
		migrated++
		fmt.Printf("  Migrated session: %s (%s)\n", sess.ID, sess.Title)
	}

	fmt.Printf("\nMigration complete: %d migrated, %d skipped (sub-sessions), %d failed\n", migrated, skipped, failed)

	if failed > 0 {
		return fmt.Errorf("%d sessions failed to migrate", failed)
	}

	return nil
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return contains(errStr, "UNIQUE constraint failed") || contains(errStr, "constraint failed")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || s != "" && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
