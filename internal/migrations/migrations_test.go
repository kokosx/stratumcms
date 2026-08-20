package migrations

import (
	"context"
	"testing"

	"github.com/kokosx/stratumcms/internal/storage/turso"
)

func TestFailedMigrationRollsBackAndIsNotRecorded(t *testing.T) {
	ctx := context.Background()
	db, err := turso.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	original := all
	all = append(append([]Migration{}, all...), Migration{Version: 999, Name: "broken", SQL: "CREATE TABLE should_rollback(id); INSERT INTO missing_table VALUES(1);"})
	defer func() { all = original }()
	if Run(ctx, db) == nil {
		t.Fatal("broken migration succeeded")
	}
	var recorded, table int
	_ = db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=999`).Scan(&recorded)
	_ = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='should_rollback'`).Scan(&table)
	if recorded != 0 || table != 0 {
		t.Fatalf("recorded=%d table=%d", recorded, table)
	}
}
