package turso

import (
	"context"
	"testing"

	"github.com/kokosx/stratumcms/internal/migrations"
)

func TestForeignKeysAreEnforced(t *testing.T) {
	db, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := migrations.Run(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sessions (id,user_id,token_hash,expires_at,created_at,last_seen_at) VALUES ('s','missing','t','x','x','x')`); err == nil {
		t.Fatal("session for missing user was accepted")
	}
}
