package turso

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"

	_ "turso.tech/database/tursogo"
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

	db, err = sql.Open("turso", filepath.Join(dataDir, "stratum.db"))
	if err != nil {
		return nil, fmt.Errorf("open Turso database: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize Turso database: %w", err)
	}
	return db, nil
}
