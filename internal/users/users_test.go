package users

import (
	"context"
	"testing"
	"time"

	"github.com/kokosx/stratumcms/internal/migrations"
	"github.com/kokosx/stratumcms/internal/storage/turso"
)

func TestLastAdminAndSessionInvalidation(t *testing.T) {
	ctx := context.Background()
	db, err := turso.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Run(ctx, db); err != nil {
		t.Fatal(err)
	}
	s := New(db)
	if err := s.Create(ctx, Input{Email: "admin@example.test", Username: "admin", DisplayName: "Admin", Role: "administrator", Password: "long test password"}); err != nil {
		t.Fatal(err)
	}
	items, _ := s.List(ctx)
	admin := items[0]
	if err := s.Update(ctx, admin.ID, Input{Email: admin.Email, Username: admin.Username, DisplayName: admin.DisplayName, Role: "editor"}); err == nil {
		t.Fatal("last administrator demoted")
	}
	_, _ = db.Exec(`INSERT INTO sessions(id,user_id,token_hash,expires_at,created_at,last_seen_at) VALUES('s',?,'h',?,?,?)`, admin.ID, time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano))
	if err := s.ResetPassword(ctx, admin.ID, "another long password"); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id=?`, admin.ID).Scan(&n)
	if n != 0 {
		t.Fatal("password reset retained sessions")
	}
}
