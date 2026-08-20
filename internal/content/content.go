// Package content owns entry, revision, and route persistence rules.
package content

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/kokosx/stratumcms/internal/id"
	store "github.com/kokosx/stratumcms/internal/storage/sqlc"
)

const emptyDocument = `{"version":1,"children":[]}`

var ErrValidation = errors.New("content validation")

type Service struct {
	db      *sql.DB
	queries *store.Queries
	now     func() time.Time
}

func New(db *sql.DB) *Service { return &Service{db: db, queries: store.New(db), now: time.Now} }

type Entry struct {
	ID, Title, Slug, Status, Route, Author, UpdatedAt string
	PublishedRevisionID                               string
}
type Revision struct {
	ID                string
	Number            int64
	CreatedAt, Author string
	Published         bool
}
type Input struct{ Title, Slug string }

func Slug(value string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
			dash = false
		} else if unicode.IsSpace(r) || r == '-' {
			if b.Len() > 0 && !dash {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
func (s *Service) CreateEntry(ctx context.Context, handle, authorID string, in Input) (Entry, error) {
	return s.write(ctx, "", handle, authorID, in, "draft", false, true)
}
func (s *Service) SaveEntry(ctx context.Context, entryID, authorID string, in Input) (Entry, error) {
	return s.write(ctx, entryID, "", authorID, in, "draft", false, false)
}
func (s *Service) PublishEntry(ctx context.Context, entryID, authorID string, in Input) (Entry, error) {
	return s.write(ctx, entryID, "", authorID, in, "published", true, false)
}

func (s *Service) write(ctx context.Context, entryID, handle, authorID string, in Input, status string, publish, create bool) (Entry, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Slug = Slug(in.Slug)
	if in.Slug == "" {
		in.Slug = Slug(in.Title)
	}
	if in.Title == "" || in.Slug == "" {
		return Entry{}, fmt.Errorf("%w: title and slug are required", ErrValidation)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Entry{}, fmt.Errorf("begin content write: %w", err)
	}
	defer tx.Rollback()
	q := s.queries.WithTx(tx)
	now := s.now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	var dbEntry store.Entry
	var typeID string
	if create {
		ct, e := q.GetContentTypeByHandle(ctx, handle)
		if e != nil {
			return Entry{}, fmt.Errorf("get content type: %w", e)
		}
		typeID = ct.ID
		entryID, e = id.New()
		if e != nil {
			return Entry{}, e
		}
		dbEntry = store.Entry{ID: entryID, ContentTypeID: typeID, AuthorID: authorID, CreatedAt: nowText}
	} else {
		dbEntry, err = q.GetEntry(ctx, entryID)
		if err != nil {
			return Entry{}, fmt.Errorf("get entry: %w", err)
		}
		typeID = dbEntry.ContentTypeID
	}
	path := routePath(handle, typeID, in.Slug)
	if !create {
		if typeID == "content_type_post" {
			path = "/blog/" + in.Slug
		}
	}
	if create {
		e := q.CreateEntry(ctx, store.CreateEntryParams{ID: entryID, ContentTypeID: typeID, Title: in.Title, Slug: in.Slug, Status: status, AuthorID: authorID, CreatedAt: nowText, UpdatedAt: nowText})
		if e != nil {
			return Entry{}, fmt.Errorf("create entry: %w", e)
		}
	}
	revisionID, e := id.New()
	if e != nil {
		return Entry{}, e
	}
	number, e := q.NextRevisionNumber(ctx, entryID)
	if e != nil {
		return Entry{}, fmt.Errorf("next revision: %w", e)
	}
	if e = q.CreateRevision(ctx, store.CreateRevisionParams{ID: revisionID, EntryID: entryID, Number: number, Title: in.Title, DocumentJson: emptyDocument, CreatedBy: authorID, CreatedAt: nowText}); e != nil {
		return Entry{}, fmt.Errorf("create revision: %w", e)
	}
	publishedID := dbEntry.PublishedRevisionID
	publishedAt := dbEntry.PublishedAt
	if publish {
		publishedID = sql.NullString{String: revisionID, Valid: true}
		publishedAt = sql.NullString{String: nowText, Valid: true}
	}
	if create {
		routeID, _ := id.New()
		e = q.CreateRoute(ctx, store.CreateRouteParams{ID: routeID, Path: path, ResourceType: "entry", ResourceID: entryID, Canonical: 1, CreatedAt: nowText, UpdatedAt: nowText})
	} else {
		e = q.UpdateEntry(ctx, store.UpdateEntryParams{ID: entryID, Title: in.Title, Slug: in.Slug, Status: status, UpdatedAt: nowText, PublishedAt: publishedAt, PublishedRevisionID: publishedID})
		if e == nil {
			e = q.UpdateEntryRoute(ctx, store.UpdateEntryRouteParams{Path: path, UpdatedAt: nowText, ResourceID: entryID})
		}
	}
	if e != nil {
		if strings.Contains(strings.ToLower(e.Error()), "routes.path") || strings.Contains(strings.ToLower(e.Error()), "unique") {
			return Entry{}, fmt.Errorf("%w: route already exists", ErrValidation)
		}
		return Entry{}, fmt.Errorf("write entry: %w", e)
	}
	if err = tx.Commit(); err != nil {
		return Entry{}, fmt.Errorf("commit content write: %w", err)
	}
	return Entry{ID: entryID, Title: in.Title, Slug: in.Slug, Status: status, Route: path, UpdatedAt: nowText, PublishedRevisionID: publishedID.String}, nil
}
func routePath(handle, typeID, slug string) string {
	if handle == "post" || typeID == "content_type_post" {
		return "/blog/" + slug
	}
	return "/" + slug
}
func (s *Service) GetEntry(ctx context.Context, id string) (Entry, error) {
	e, err := s.queries.GetEntry(ctx, id)
	if err != nil {
		return Entry{}, err
	}
	route, err := s.queries.GetEntryRoute(ctx, id)
	return Entry{ID: e.ID, Title: e.Title, Slug: e.Slug, Status: e.Status, Route: route.Path, UpdatedAt: e.UpdatedAt, PublishedRevisionID: e.PublishedRevisionID.String}, err
}
func (s *Service) ListEntries(ctx context.Context, handle string) ([]Entry, error) {
	ct, err := s.queries.GetContentTypeByHandle(ctx, handle)
	if err != nil {
		return nil, err
	}
	rows, err := s.queries.ListEntriesByType(ctx, ct.ID)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(rows))
	for _, r := range rows {
		out = append(out, Entry{ID: r.ID, Title: r.Title, Slug: r.Slug, Status: r.Status, Route: r.Path, Author: r.DisplayName, UpdatedAt: r.UpdatedAt, PublishedRevisionID: r.PublishedRevisionID.String})
	}
	return out, nil
}
func (s *Service) ListRevisions(ctx context.Context, entryID string) ([]Revision, error) {
	entry, err := s.queries.GetEntry(ctx, entryID)
	if err != nil {
		return nil, err
	}
	rows, err := s.queries.ListRevisions(ctx, entryID)
	if err != nil {
		return nil, err
	}
	out := make([]Revision, 0, len(rows))
	for _, r := range rows {
		out = append(out, Revision{ID: r.ID, Number: r.Number, CreatedAt: r.CreatedAt, Author: r.DisplayName, Published: r.ID == entry.PublishedRevisionID.String})
	}
	return out, nil
}
