package content

import (
	"context"
	"testing"
	"time"

	"github.com/kokosx/stratumcms/internal/migrations"
	store "github.com/kokosx/stratumcms/internal/storage/sqlc"
	"github.com/kokosx/stratumcms/internal/storage/turso"
)

func TestEntryRevisionsAndRoutes(t *testing.T) {
	ctx := context.Background()
	db, err := turso.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := migrations.Run(ctx, db); err != nil {
		t.Fatal(err)
	}
	q := store.New(db)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := q.CreateUser(ctx, store.CreateUserParams{ID: "author", Email: "author@example.test", Username: "author", PasswordHash: "hash", DisplayName: "Author", Role: "administrator", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	s := New(db)
	page, err := s.CreateEntry(ctx, "page", "author", Input{Title: "About us"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Route != "/about-us" {
		t.Fatalf("page route = %q", page.Route)
	}
	post, err := s.CreateEntry(ctx, "post", "author", Input{Title: "Hello world"})
	if err != nil {
		t.Fatal(err)
	}
	if post.Route != "/blog/hello-world" {
		t.Fatalf("post route = %q", post.Route)
	}
	if _, err := s.CreateEntry(ctx, "page", "author", Input{Title: "Another", Slug: "about-us"}); err == nil {
		t.Fatal("duplicate route was accepted")
	}
	if _, err := s.SaveEntry(ctx, page.ID, "author", Input{Title: "About Stratum", Slug: "about"}); err != nil {
		t.Fatal(err)
	}
	revisions, err := s.ListRevisions(ctx, page.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 2 || revisions[0].Number != 2 {
		t.Fatalf("revisions = %#v", revisions)
	}
	published, err := s.PublishEntry(ctx, page.ID, "author", Input{Title: "About Stratum", Slug: "about"})
	if err != nil {
		t.Fatal(err)
	}
	if published.PublishedRevisionID == "" {
		t.Fatal("publish did not point to revision")
	}
	if _, err := s.SaveEntry(ctx, page.ID, "author", Input{Title: "Draft", Slug: "about"}); err != nil {
		t.Fatal(err)
	}
	entry, err := s.GetEntry(ctx, page.ID)
	if err != nil {
		t.Fatal(err)
	}
	if entry.PublishedRevisionID != published.PublishedRevisionID {
		t.Fatal("draft replaced published revision")
	}
}
