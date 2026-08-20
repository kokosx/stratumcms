package operations

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/kokosx/stratumcms/internal/config"
	"github.com/kokosx/stratumcms/internal/content"
	"github.com/kokosx/stratumcms/internal/migrations"
	"github.com/kokosx/stratumcms/internal/platform"
	store "github.com/kokosx/stratumcms/internal/storage/sqlc"
	"github.com/kokosx/stratumcms/internal/storage/turso"
)

func TestBackupRestoreRoundTripAndTraversalRejection(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	if err := platform.EnsureDataDir(source); err != nil {
		t.Fatal(err)
	}
	db, err := turso.Open(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.Run(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := store.New(db).CreateUser(ctx, store.CreateUserParams{ID: "admin", Email: "admin@example.test", Username: "admin", PasswordHash: "hash", DisplayName: "Admin", Role: "administrator", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	entry, err := content.New(db).CreateEntry(ctx, "page", "admin", content.Input{Title: "About"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = content.New(db).PublishEntry(ctx, entry.ID, "admin", content.Input{Title: "About"}); err != nil {
		t.Fatal(err)
	}
	db.Close()
	archive := filepath.Join(t.TempDir(), "site.tar.gz")
	if _, err := Backup(ctx, config.Config{DataDir: source}, "test", archive); err != nil {
		t.Fatal(err)
	}
	manifest, err := ReadManifest(archive)
	if err != nil || manifest.SchemaVersion != migrations.LatestVersion() {
		t.Fatalf("manifest=%#v err=%v", manifest, err)
	}
	target := filepath.Join(t.TempDir(), "restored")
	if err := Restore(ctx, config.Config{DataDir: target}, archive); err != nil {
		t.Fatal(err)
	}
	restored, err := turso.Open(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var title string
	if err := restored.QueryRow(`SELECT title FROM entries WHERE id=?`, entry.ID).Scan(&title); err != nil || title != "About" {
		t.Fatalf("title=%q err=%v", title, err)
	}
	var sessions int
	_ = restored.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&sessions)
	if sessions != 0 {
		t.Fatal("sessions survived restore")
	}
}
