package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"
)

//go:embed sql/*.sql
var files embed.FS

// Migration is one versioned database change.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

var all = []Migration{
	{Version: 1, Name: "schema_migrations", SQL: mustRead("sql/001_schema_migrations.sql")},
	{Version: 2, Name: "users_and_sessions", SQL: mustRead("sql/002_users_and_sessions.sql")},
	{Version: 3, Name: "content", SQL: mustRead("sql/003_content.sql")},
	{Version: 4, Name: "entry_drafts", SQL: mustRead("sql/004_entry_drafts.sql")},
	{Version: 5, Name: "site_presentation", SQL: mustRead("sql/005_site_presentation.sql")},
}

// Run applies migrations not yet recorded in schema_migrations.
func Run(ctx context.Context, db *sql.DB) error {
	if err := ensureTable(ctx, db); err != nil {
		return err
	}

	migrations := append([]Migration(nil), all...)
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Version < migrations[j].Version })
	for _, migration := range migrations {
		var applied bool
		err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)", migration.Version).Scan(&applied)
		if err != nil {
			return fmt.Errorf("check migration %d: %w", migration.Version, err)
		}
		if applied {
			continue
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", migration.Version, err)
		}
		if _, err := tx.ExecContext(ctx, migration.SQL); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %d (%s): %w", migration.Version, migration.Name, err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES (?)", migration.Version); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %d: %w", migration.Version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", migration.Version, err)
		}
	}
	return nil
}

func ensureTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}
	return nil
}

func mustRead(path string) string {
	contents, err := files.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return string(contents)
}
