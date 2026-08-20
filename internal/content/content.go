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

	"github.com/kokosx/stratumcms/internal/blocks"
	"github.com/kokosx/stratumcms/internal/documents"
	"github.com/kokosx/stratumcms/internal/id"
	store "github.com/kokosx/stratumcms/internal/storage/sqlc"
)

var ErrValidation = errors.New("content validation")

type Service struct {
	db       *sql.DB
	queries  *store.Queries
	now      func() time.Time
	registry *blocks.Registry
}

func New(db *sql.DB) *Service {
	return &Service{db: db, queries: store.New(db), now: time.Now, registry: blocks.CoreRegistry()}
}
func (s *Service) Registry() *blocks.Registry { return s.registry }

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
type Input struct {
	Title, Slug string
	Document    *documents.Document
}
type Published struct {
	Title    string
	Document documents.Document
}

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
		path = routePath("", typeID, in.Slug)
	}
	if create {
		e := q.CreateEntry(ctx, store.CreateEntryParams{ID: entryID, ContentTypeID: typeID, Title: in.Title, Slug: in.Slug, Status: status, AuthorID: authorID, CreatedAt: nowText, UpdatedAt: nowText})
		if e != nil {
			return Entry{}, fmt.Errorf("create entry: %w", e)
		}
	}
	documentJSON, e := s.documentJSON(ctx, q, entryID, create, in.Document)
	if e != nil {
		return Entry{}, e
	}
	revisionID, e := id.New()
	if e != nil {
		return Entry{}, e
	}
	number, e := q.NextRevisionNumber(ctx, entryID)
	if e != nil {
		return Entry{}, fmt.Errorf("next revision: %w", e)
	}
	if e = q.CreateRevision(ctx, store.CreateRevisionParams{ID: revisionID, EntryID: entryID, Number: number, Title: in.Title, DocumentJson: documentJSON, CreatedBy: authorID, CreatedAt: nowText}); e != nil {
		return Entry{}, fmt.Errorf("create revision: %w", e)
	}
	publishedID, publishedAt := dbEntry.PublishedRevisionID, dbEntry.PublishedAt
	entryStatus := dbEntry.Status
	if publish {
		publishedID = sql.NullString{String: revisionID, Valid: true}
		publishedAt = sql.NullString{String: nowText, Valid: true}
		entryStatus = "published"
	} else if create {
		entryStatus = "draft"
	}
	if create {
		routeID, idErr := id.New()
		if idErr != nil {
			return Entry{}, fmt.Errorf("generate route id: %w", idErr)
		}
		e = q.CreateRoute(ctx, store.CreateRouteParams{ID: routeID, Path: path, ResourceType: "entry", ResourceID: entryID, Canonical: 1, CreatedAt: nowText, UpdatedAt: nowText})
	} else {
		e = q.UpdateEntry(ctx, store.UpdateEntryParams{ID: entryID, Title: in.Title, Slug: in.Slug, Status: entryStatus, UpdatedAt: nowText, PublishedAt: publishedAt, PublishedRevisionID: publishedID})
		if e == nil && publish {
			e = q.UpdateEntryRoute(ctx, store.UpdateEntryRouteParams{Path: path, UpdatedAt: nowText, ResourceID: entryID})
		}
	}
	if e != nil {
		if strings.Contains(strings.ToLower(e.Error()), "routes.path") || strings.Contains(strings.ToLower(e.Error()), "unique") {
			return Entry{}, fmt.Errorf("%w: route already exists", ErrValidation)
		}
		return Entry{}, fmt.Errorf("write entry: %w", e)
	}
	if !create && !publish {
		route, routeErr := q.GetEntryRoute(ctx, entryID)
		if routeErr != nil {
			return Entry{}, fmt.Errorf("get entry route: %w", routeErr)
		}
		path = route.Path
	}
	if err = tx.Commit(); err != nil {
		return Entry{}, fmt.Errorf("commit content write: %w", err)
	}
	return Entry{ID: entryID, Title: in.Title, Slug: in.Slug, Status: entryStatus, Route: path, UpdatedAt: nowText, PublishedRevisionID: publishedID.String}, nil
}
func (s *Service) documentJSON(ctx context.Context, q *store.Queries, entryID string, create bool, document *documents.Document) (string, error) {
	if document != nil {
		if err := s.registry.Validate(*document); err != nil {
			return "", fmt.Errorf("%w: invalid document: %v", ErrValidation, err)
		}
		data, err := documents.Marshal(*document)
		if err != nil {
			return "", fmt.Errorf("%w: marshal document: %v", ErrValidation, err)
		}
		return string(data), nil
	}
	if create {
		data, err := documents.Marshal(documents.Empty())
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	latest, err := q.GetLatestRevision(ctx, entryID)
	if err != nil {
		return "", fmt.Errorf("get latest revision: %w", err)
	}
	doc, err := documents.Parse([]byte(latest.DocumentJson))
	if err != nil {
		return "", fmt.Errorf("parse latest document: %w", err)
	}
	if err := s.registry.Validate(doc); err != nil {
		return "", fmt.Errorf("validate latest document: %w", err)
	}
	return latest.DocumentJson, nil
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
func (s *Service) GetEntryByType(ctx context.Context, entryID, handle string) (Entry, error) {
	e, err := s.queries.GetEntryByType(ctx, store.GetEntryByTypeParams{ID: entryID, Handle: handle})
	if err != nil {
		return Entry{}, err
	}
	route, err := s.queries.GetEntryRoute(ctx, entryID)
	if err != nil {
		return Entry{}, err
	}
	return Entry{ID: e.ID, Title: e.Title, Slug: e.Slug, Status: e.Status, Route: route.Path, UpdatedAt: e.UpdatedAt, PublishedRevisionID: e.PublishedRevisionID.String}, nil
}
func (s *Service) ResolvePublished(ctx context.Context, path string) (Published, error) {
	row, err := s.queries.ResolvePublishedRoute(ctx, path)
	if err != nil {
		return Published{}, err
	}
	document, err := documents.Parse([]byte(row.DocumentJson))
	if err != nil {
		return Published{}, fmt.Errorf("parse published document: %w", err)
	}
	if err := s.registry.Validate(document); err != nil {
		return Published{}, fmt.Errorf("validate published document: %w", err)
	}
	return Published{Title: row.Title_2, Document: document}, nil
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
