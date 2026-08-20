// Package content owns entry, revision, and route persistence rules.
package content

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/kokosx/stratumcms/internal/blocks"
	"github.com/kokosx/stratumcms/internal/documents"
	"github.com/kokosx/stratumcms/internal/id"
	store "github.com/kokosx/stratumcms/internal/storage/sqlc"
)

var ErrValidation = errors.New("content validation")
var ErrConflict = errors.New("content conflict")

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
	ID, Kind, Title, Slug, Status, Route, Author, AuthorID, UpdatedAt string
	PublishedRevisionID                                               string
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
	SEO         *SEO
}
type DraftUpdate struct {
	Title, Slug, DocumentJSON, SEOJSON, UpdatedBy string
	Version                                       int64
}
type Published struct {
	EntryID, RevisionID, Kind, Title, Path string
	Document                               documents.Document
	SEO                                    SEO
}
type SEO struct{ Title, Description, Canonical, Robots string }

func ValidateSEO(seo SEO) error {
	if len(seo.Title) > 120 || len(seo.Description) > 320 {
		return fmt.Errorf("%w: SEO title or description is too long", ErrValidation)
	}
	if seo.Robots != "" && seo.Robots != "index,follow" && seo.Robots != "noindex,follow" {
		return fmt.Errorf("%w: invalid robots setting", ErrValidation)
	}
	if strings.ContainsAny(seo.Canonical, "\r\n\t") {
		return fmt.Errorf("%w: invalid canonical URL", ErrValidation)
	}
	if seo.Canonical != "" {
		u, err := url.Parse(seo.Canonical)
		if err != nil || (u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https") || (u.Scheme == "" && !strings.HasPrefix(seo.Canonical, "/")) {
			return fmt.Errorf("%w: canonical must be a local path or HTTP(S) URL", ErrValidation)
		}
	}
	return nil
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

// SaveEntryWithDraft writes a revision and advances the editor draft in one transaction.
func (s *Service) SaveEntryWithDraft(ctx context.Context, entryID, authorID string, in Input, draft DraftUpdate, publish bool) (Entry, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Entry{}, fmt.Errorf("begin editor save: %w", err)
	}
	defer tx.Rollback()
	q := s.queries.WithTx(tx)
	changedResult, err := tx.ExecContext(ctx, `UPDATE entry_drafts SET title=?,slug=?,document_json=?,seo_json=?,version=version+1,updated_by=?,updated_at=? WHERE entry_id=? AND version=?`, draft.Title, draft.Slug, draft.DocumentJSON, draft.SEOJSON, draft.UpdatedBy, s.now().UTC().Format(time.RFC3339Nano), entryID, draft.Version)
	if err != nil {
		return Entry{}, fmt.Errorf("advance editor draft: %w", err)
	}
	changed, _ := changedResult.RowsAffected()
	if changed != 1 {
		return Entry{}, ErrConflict
	}
	entry, err := s.writeTx(ctx, q, tx, entryID, "", authorID, in, publish, false)
	if err != nil {
		return Entry{}, err
	}
	if err := tx.Commit(); err != nil {
		return Entry{}, fmt.Errorf("commit editor save: %w", err)
	}
	return entry, nil
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
	entry, err := s.writeTx(ctx, q, tx, entryID, handle, authorID, in, publish, create)
	if err != nil {
		return Entry{}, err
	}
	if err = tx.Commit(); err != nil {
		return Entry{}, fmt.Errorf("commit content write: %w", err)
	}
	return entry, nil
}
func (s *Service) writeTx(ctx context.Context, q *store.Queries, tx *sql.Tx, entryID, handle, authorID string, in Input, publish, create bool) (Entry, error) {
	status := "draft"
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
		var e error
		dbEntry, e = q.GetEntry(ctx, entryID)
		if e != nil {
			return Entry{}, fmt.Errorf("get entry: %w", e)
		}
		typeID = dbEntry.ContentTypeID
	}
	path := routePath(handle, typeID, in.Slug)
	if !create {
		path = routePath("", typeID, in.Slug)
	}
	if reservedPath(path) {
		return Entry{}, fmt.Errorf("%w: slug uses a reserved route", ErrValidation)
	}
	var oldPath string
	if !create {
		if route, e := q.GetEntryRoute(ctx, entryID); e == nil {
			oldPath = route.Path
		}
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
	seoJSON, e := s.seoJSON(ctx, tx, entryID, create, in.SEO)
	if e != nil {
		return Entry{}, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO revisions(id,entry_id,number,title,document_json,seo_json,created_by,created_at) VALUES(?,?,?,?,?,?,?,?)`, revisionID, entryID, number, in.Title, documentJSON, seoJSON, authorID, nowText); e != nil {
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
			if e == nil && oldPath != "" && oldPath != path {
				if _, e = tx.ExecContext(ctx, `DELETE FROM redirects WHERE from_path=?`, path); e == nil {
					_, e = tx.ExecContext(ctx, `INSERT INTO redirects(id,from_path,to_path,status_code,created_at,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(from_path) DO UPDATE SET to_path=excluded.to_path,status_code=excluded.status_code,updated_at=excluded.updated_at`, revisionID, oldPath, path, 301, nowText, nowText)
				}
			}
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
	return Entry{ID: entryID, Title: in.Title, Slug: in.Slug, Status: entryStatus, Route: path, UpdatedAt: nowText, PublishedRevisionID: publishedID.String}, nil
}
func (s *Service) seoJSON(ctx context.Context, tx *sql.Tx, entryID string, create bool, seo *SEO) (string, error) {
	if seo != nil {
		if err := ValidateSEO(*seo); err != nil {
			return "", err
		}
		data, err := json.Marshal(seo)
		return string(data), err
	}
	if create {
		return "{}", nil
	}
	var raw string
	if err := tx.QueryRowContext(ctx, `SELECT seo_json FROM revisions WHERE entry_id=? ORDER BY number DESC LIMIT 1`, entryID).Scan(&raw); err != nil {
		return "", err
	}
	return raw, nil
}
func reservedPath(p string) bool {
	for _, prefix := range []string{"/admin", "/static", "/assets", "/media", "/setup", "/login", "/health", "/ready"} {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return false
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
	return Entry{ID: e.ID, Kind: entryKind(e.ContentTypeID), Title: e.Title, Slug: e.Slug, Status: e.Status, Route: route.Path, AuthorID: e.AuthorID, UpdatedAt: e.UpdatedAt, PublishedRevisionID: e.PublishedRevisionID.String}, err
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
	return Entry{ID: e.ID, Kind: handle, Title: e.Title, Slug: e.Slug, Status: e.Status, Route: route.Path, AuthorID: e.AuthorID, UpdatedAt: e.UpdatedAt, PublishedRevisionID: e.PublishedRevisionID.String}, nil
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
	var seo SEO
	var seoRaw string
	if err := s.db.QueryRowContext(ctx, `SELECT seo_json FROM revisions WHERE id=?`, row.ID_2).Scan(&seoRaw); err != nil {
		return Published{}, err
	}
	if seoRaw != "" && seoRaw != "{}" {
		if err := json.Unmarshal([]byte(seoRaw), &seo); err != nil {
			return Published{}, fmt.Errorf("parse published SEO: %w", err)
		}
	}
	return Published{EntryID: row.ID, RevisionID: row.ID_2, Title: row.Title_2, Kind: entryKind(row.ContentTypeID), Path: path, Document: document, SEO: seo}, nil
}

// GetPublishedByEntryID retrieves the canonical published route without callers deriving it from a slug.
func (s *Service) GetPublishedByEntryID(ctx context.Context, entryID string) (Published, error) {
	route, err := s.queries.GetEntryRoute(ctx, entryID)
	if err != nil {
		return Published{}, err
	}
	return s.ResolvePublished(ctx, route.Path)
}
func entryKind(typeID string) string {
	if typeID == "content_type_post" {
		return "post"
	}
	return "page"
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
		out = append(out, Entry{ID: r.ID, Kind: handle, Title: r.Title, Slug: r.Slug, Status: r.Status, Route: r.Path, Author: r.DisplayName, AuthorID: r.AuthorID, UpdatedAt: r.UpdatedAt, PublishedRevisionID: r.PublishedRevisionID.String})
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
