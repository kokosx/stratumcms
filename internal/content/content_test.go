package content

import (
	"context"
	"testing"
	"time"

	"github.com/kokosx/stratumcms/internal/documents"
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

func TestDraftDoesNotReplacePublishedDocumentOrRoute(t *testing.T) {
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
	s := New(db)
	old := documents.Document{Version: 1, Children: []documents.Node{{ID: "old", Type: "core.text", Version: 1, Props: map[string]any{"text": "Old"}, Settings: map[string]any{}}}}
	page, err := s.CreateEntry(ctx, "page", "author", Input{Title: "About", Document: &old})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublishEntry(ctx, page.ID, "author", Input{Title: "About", Slug: "old", Document: &old}); err != nil {
		t.Fatal(err)
	}
	newDoc := documents.Document{Version: 1, Children: []documents.Node{{ID: "new", Type: "core.text", Version: 1, Props: map[string]any{"text": "New"}, Settings: map[string]any{}}}}
	if _, err = s.SaveEntry(ctx, page.ID, "author", Input{Title: "New about", Slug: "new", Document: &newDoc}); err != nil {
		t.Fatal(err)
	}
	published, err := s.ResolvePublished(ctx, "/old")
	if err != nil {
		t.Fatal(err)
	}
	if published.Title != "About" || published.Document.Children[0].ID != "old" {
		t.Fatalf("published=%#v", published)
	}
	if _, err = s.ResolvePublished(ctx, "/new"); err == nil {
		t.Fatal("draft route resolved publicly")
	}
	if _, err = s.PublishEntry(ctx, page.ID, "author", Input{Title: "New about", Slug: "new", Document: &newDoc}); err != nil {
		t.Fatal(err)
	}
	published, err = s.ResolvePublished(ctx, "/new")
	if err != nil || published.Document.Children[0].ID != "new" {
		t.Fatalf("published after update=%#v err=%v", published, err)
	}
}

func TestMetadataOnlySavePreservesDocument(t *testing.T) {
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
	s := New(db)
	doc := documents.Document{Version: 1, Children: []documents.Node{{ID: "text", Type: "core.text", Version: 1, Props: map[string]any{"text": "Persist"}, Settings: map[string]any{}}}}
	page, err := s.CreateEntry(ctx, "page", "author", Input{Title: "A", Document: &doc})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveEntry(ctx, page.ID, "author", Input{Title: "B"}); err != nil {
		t.Fatal(err)
	}
	latest, err := store.New(db).GetLatestRevision(ctx, page.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := documents.Parse([]byte(latest.DocumentJson))
	if err != nil || got.Children[0].ID != "text" {
		t.Fatalf("document=%#v err=%v", got, err)
	}
}

func TestSEORemainsDraftUntilPublish(t *testing.T) {
	ctx := context.Background()
	db, err := turso.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Run(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := store.New(db).CreateUser(ctx, store.CreateUserParams{ID: "author", Email: "author@example.test", Username: "author", PasswordHash: "hash", DisplayName: "Author", Role: "administrator", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	s := New(db)
	entry, err := s.CreateEntry(ctx, "page", "author", Input{Title: "SEO"})
	if err != nil {
		t.Fatal(err)
	}
	old := SEO{Title: "Old search title", Description: "Old", Robots: "index,follow"}
	if _, err = s.PublishEntry(ctx, entry.ID, "author", Input{Title: "SEO", SEO: &old}); err != nil {
		t.Fatal(err)
	}
	next := SEO{Title: "New search title", Description: "New", Canonical: "/seo", Robots: "noindex,follow"}
	if _, err = s.SaveEntry(ctx, entry.ID, "author", Input{Title: "SEO", SEO: &next}); err != nil {
		t.Fatal(err)
	}
	published, err := s.ResolvePublished(ctx, "/seo")
	if err != nil || published.SEO.Title != old.Title {
		t.Fatalf("draft leaked: %#v err=%v", published.SEO, err)
	}
	if _, err = s.PublishEntry(ctx, entry.ID, "author", Input{Title: "SEO", SEO: &next}); err != nil {
		t.Fatal(err)
	}
	published, err = s.ResolvePublished(ctx, "/seo")
	if err != nil || published.SEO.Title != next.Title || published.SEO.Robots != "noindex,follow" {
		t.Fatalf("SEO not published: %#v err=%v", published.SEO, err)
	}
}
