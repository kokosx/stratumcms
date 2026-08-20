package turso

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"

	turso "turso.tech/database/tursogo"
)

// Open opens the local Turso database stored in dataDir.
func Open(ctx context.Context, dataDir string) (db *sql.DB, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if db != nil {
				db.Close()
			}
			err = fmt.Errorf("initialize Turso database: %v", recovered)
		}
	}()

	connector, err := turso.NewConnector(filepath.Join(dataDir, "stratum.db"))
	if err != nil {
		return nil, fmt.Errorf("configure Turso database: %w", err)
	}
	db = sql.OpenDB(connector)
	// tursogo has no per-connection initialization callback. SQLite pragmas are
	// connection-local, therefore the pool is constrained to its initialized connection.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize Turso database: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	return db, nil
}
