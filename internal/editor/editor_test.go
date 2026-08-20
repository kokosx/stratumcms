package editor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kokosx/stratumcms/internal/content"
	"github.com/kokosx/stratumcms/internal/migrations"
	store "github.com/kokosx/stratumcms/internal/storage/sqlc"
	"github.com/kokosx/stratumcms/internal/storage/turso"
)

func TestDraftMutationAndVersionConflict(t *testing.T) {
	ctx := context.Background()
	db, err := turso.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := migrations.Run(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := store.New(db).CreateUser(ctx, store.CreateUserParams{ID: "author", Email: "author@example.test", Username: "author", PasswordHash: "hash", DisplayName: "Author", Role: "administrator", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	c := content.New(db)
	entry, err := c.CreateEntry(ctx, "page", "author", content.Input{Title: "About"})
	if err != nil {
		t.Fatal(err)
	}
	s := New(db, c)
	draft, err := s.LoadDraft(ctx, entry.ID, "author")
	if err != nil || draft.Version != 1 {
		t.Fatalf("draft=%#v err=%v", draft, err)
	}
	stale := draft.Version
	draft, err = s.AddBlock(ctx, entry.ID, "author", draft.Version, "", "core.text", 1)
	if err != nil || len(draft.Document.Children) != 1 {
		t.Fatalf("add=%#v err=%v", draft, err)
	}
	if _, err = s.AddBlock(ctx, entry.ID, "author", stale, "", "core.text", 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("error=%v", err)
	}
	duplicated, err := s.DuplicateBlock(ctx, entry.ID, "author", draft.Version, draft.Document.Children[0].ID)
	if err != nil || duplicated.Document.Children[0].ID == duplicated.Document.Children[1].ID {
		t.Fatalf("duplicate=%#v err=%v", duplicated, err)
	}
}

func TestSaveDraftConflictCreatesNoRevision(t *testing.T) {
	ctx := context.Background()
	db, err := turso.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := migrations.Run(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	q := store.New(db)
	if err := q.CreateUser(ctx, store.CreateUserParams{ID: "author", Email: "author@example.test", Username: "author", PasswordHash: "hash", DisplayName: "Author", Role: "administrator", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	c := content.New(db)
	entry, err := c.CreateEntry(ctx, "page", "author", content.Input{Title: "About"})
	if err != nil {
		t.Fatal(err)
	}
	s := New(db, c)
	draft, err := s.LoadDraft(ctx, entry.ID, "author")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveDraft(ctx, entry.ID, "author", draft.Version); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveDraft(ctx, entry.ID, "author", draft.Version); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale save error=%v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM revisions WHERE entry_id=?", entry.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("revisions=%d, want 2", count)
	}
	current, err := s.LoadDraft(ctx, entry.ID, "author")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Publish(ctx, entry.ID, "author", current.Version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM revisions WHERE entry_id=?", entry.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("revisions after publish=%d, want 3", count)
	}
}
