// Package editor owns server-side working copies of entry documents.
package editor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kokosx/stratumcms/internal/blocks"
	"github.com/kokosx/stratumcms/internal/content"
	"github.com/kokosx/stratumcms/internal/documents"
	"github.com/kokosx/stratumcms/internal/id"
	store "github.com/kokosx/stratumcms/internal/storage/sqlc"
)

var (
	ErrNotFound   = errors.New("editor draft not found")
	ErrConflict   = errors.New("editor draft changed in another tab")
	ErrValidation = errors.New("editor validation")
)

type Draft struct {
	EntryID, Kind, Title, Slug, UpdatedBy, UpdatedAt string
	Document                                         documents.Document
	Version                                          int64
}

type Service struct {
	queries  *store.Queries
	content  *content.Service
	registry *blocks.Registry
	now      func() time.Time
	newID    func() (string, error)
}

func New(db *sql.DB, contentService *content.Service) *Service {
	return &Service{queries: store.New(db), content: contentService, registry: contentService.Registry(), now: time.Now, newID: id.New}
}

func (s *Service) Registry() *blocks.Registry { return s.registry }

func (s *Service) LoadDraft(ctx context.Context, entryID, userID string) (Draft, error) {
	draft, err := s.queries.GetEntryDraft(ctx, entryID)
	if err == nil {
		return s.fromRow(ctx, draft)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Draft{}, fmt.Errorf("get editor draft: %w", err)
	}
	entry, err := s.queries.GetEntry(ctx, entryID)
	if errors.Is(err, sql.ErrNoRows) {
		return Draft{}, ErrNotFound
	}
	if err != nil {
		return Draft{}, fmt.Errorf("get entry: %w", err)
	}
	latest, err := s.queries.GetLatestRevision(ctx, entryID)
	if err != nil {
		return Draft{}, fmt.Errorf("get latest revision: %w", err)
	}
	doc, err := documents.Parse([]byte(latest.DocumentJson))
	if err != nil {
		return Draft{}, fmt.Errorf("parse latest revision document: %w", err)
	}
	if err := s.registry.Validate(doc); err != nil {
		return Draft{}, fmt.Errorf("validate latest revision document: %w", err)
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	create := store.CreateEntryDraftParams{EntryID: entryID, Title: entry.Title, Slug: entry.Slug, DocumentJson: latest.DocumentJson, Version: 1, UpdatedBy: userID, UpdatedAt: now}
	if err := s.queries.CreateEntryDraft(ctx, create); err != nil {
		// Another request may have created the lazy draft first.
		if loaded, loadErr := s.queries.GetEntryDraft(ctx, entryID); loadErr == nil {
			return s.fromRow(ctx, loaded)
		}
		return Draft{}, fmt.Errorf("create editor draft: %w", err)
	}
	return Draft{EntryID: entryID, Kind: entryKind(entry.ContentTypeID), Title: entry.Title, Slug: entry.Slug, Document: doc, Version: 1, UpdatedBy: userID, UpdatedAt: now}, nil
}

func (s *Service) AddBlock(ctx context.Context, entryID, userID string, version int64, parentID, blockID string, blockVersion int) (Draft, error) {
	draft, err := s.loadVersion(ctx, entryID, userID, version)
	if err != nil {
		return Draft{}, err
	}
	def, _, err := s.registry.Resolve(blockID, blockVersion)
	if err != nil {
		return Draft{}, fmt.Errorf("%w: unknown block", ErrValidation)
	}
	if parentID != "" {
		parent := documents.Find(draft.Document.Children, parentID)
		if parent == nil {
			return Draft{}, fmt.Errorf("%w: parent not found", ErrValidation)
		}
		parentDef, _, _ := s.registry.Resolve(parent.Type, parent.Version)
		if !parentDef.AllowsChildren {
			return Draft{}, fmt.Errorf("%w: parent does not allow children", ErrValidation)
		}
	}
	nodeID, err := s.newID()
	if err != nil {
		return Draft{}, err
	}
	node := documents.Node{ID: nodeID, Type: def.ID, Version: def.Version, Props: defaults(def.Props), Settings: defaults(def.Settings), Children: []documents.Node{}}
	if err := documents.InsertNode(&draft.Document, parentID, node); err != nil {
		return Draft{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return s.persist(ctx, draft, userID)
}

func (s *Service) UpdateBlock(ctx context.Context, entryID, userID string, version int64, nodeID, group, field string, value any) (Draft, error) {
	draft, err := s.loadVersion(ctx, entryID, userID, version)
	if err != nil {
		return Draft{}, err
	}
	node := documents.Find(draft.Document.Children, nodeID)
	if node == nil {
		return Draft{}, fmt.Errorf("%w: node not found", ErrValidation)
	}
	def, _, _ := s.registry.Resolve(node.Type, node.Version)
	var schema map[string]blocks.Field
	var values map[string]any
	switch group {
	case "props":
		schema, values = def.Props, node.Props
	case "settings":
		schema, values = def.Settings, node.Settings
	default:
		return Draft{}, fmt.Errorf("%w: unknown field group", ErrValidation)
	}
	fieldDef, ok := schema[field]
	if !ok {
		return Draft{}, fmt.Errorf("%w: unknown field", ErrValidation)
	}
	if err := validateEditorValue(value, fieldDef); err != nil {
		return Draft{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	values[field] = value
	return s.persist(ctx, draft, userID)
}

func (s *Service) DeleteBlock(ctx context.Context, entryID, userID string, version int64, nodeID string) (Draft, error) {
	draft, err := s.loadVersion(ctx, entryID, userID, version)
	if err != nil {
		return Draft{}, err
	}
	if err := documents.DeleteNode(&draft.Document, nodeID); err != nil {
		return Draft{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return s.persist(ctx, draft, userID)
}
func (s *Service) DuplicateBlock(ctx context.Context, entryID, userID string, version int64, nodeID string) (Draft, error) {
	draft, err := s.loadVersion(ctx, entryID, userID, version)
	if err != nil {
		return Draft{}, err
	}
	if _, err := documents.DuplicateNode(&draft.Document, nodeID, s.newID); err != nil {
		return Draft{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return s.persist(ctx, draft, userID)
}
func (s *Service) MoveBlock(ctx context.Context, entryID, userID string, version int64, nodeID, parentID string, index int) (Draft, error) {
	draft, err := s.loadVersion(ctx, entryID, userID, version)
	if err != nil {
		return Draft{}, err
	}
	if parentID != "" {
		parent := documents.Find(draft.Document.Children, parentID)
		if parent == nil {
			return Draft{}, fmt.Errorf("%w: parent not found", ErrValidation)
		}
		def, _, _ := s.registry.Resolve(parent.Type, parent.Version)
		if !def.AllowsChildren {
			return Draft{}, fmt.Errorf("%w: parent does not allow children", ErrValidation)
		}
	}
	if err := documents.MoveNode(&draft.Document, nodeID, parentID, index); err != nil {
		return Draft{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return s.persist(ctx, draft, userID)
}
func (s *Service) UpdateMetadata(ctx context.Context, entryID, userID string, version int64, title, slug string) (Draft, error) {
	draft, err := s.loadVersion(ctx, entryID, userID, version)
	if err != nil {
		return Draft{}, err
	}
	draft.Title, draft.Slug = strings.TrimSpace(title), strings.TrimSpace(slug)
	return s.persist(ctx, draft, userID)
}
func (s *Service) SaveDraft(ctx context.Context, entryID, userID string, version int64) (Draft, error) {
	return s.save(ctx, entryID, userID, version, false)
}
func (s *Service) Publish(ctx context.Context, entryID, userID string, version int64) (Draft, error) {
	return s.save(ctx, entryID, userID, version, true)
}
func (s *Service) save(ctx context.Context, entryID, userID string, version int64, publish bool) (Draft, error) {
	draft, err := s.loadVersion(ctx, entryID, userID, version)
	if err != nil {
		return Draft{}, err
	}
	in := content.Input{Title: draft.Title, Slug: draft.Slug, Document: &draft.Document}
	if publish {
		_, err = s.content.PublishEntry(ctx, entryID, userID, in)
	} else {
		_, err = s.content.SaveEntry(ctx, entryID, userID, in)
	}
	if err != nil {
		return Draft{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return s.persist(ctx, draft, userID)
}

func (s *Service) loadVersion(ctx context.Context, entryID, userID string, version int64) (Draft, error) {
	draft, err := s.LoadDraft(ctx, entryID, userID)
	if err != nil {
		return Draft{}, err
	}
	if draft.Version != version {
		return Draft{}, ErrConflict
	}
	return draft, nil
}
func (s *Service) persist(ctx context.Context, draft Draft, userID string) (Draft, error) {
	if err := s.registry.ValidateDraft(draft.Document); err != nil {
		return Draft{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	json, err := documents.Marshal(draft.Document)
	if err != nil {
		return Draft{}, err
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	changed, err := s.queries.UpdateEntryDraft(ctx, store.UpdateEntryDraftParams{Title: draft.Title, Slug: draft.Slug, DocumentJson: string(json), UpdatedBy: userID, UpdatedAt: now, EntryID: draft.EntryID, Version: draft.Version})
	if err != nil {
		return Draft{}, fmt.Errorf("update editor draft: %w", err)
	}
	if changed != 1 {
		return Draft{}, ErrConflict
	}
	draft.Version++
	draft.UpdatedBy, draft.UpdatedAt = userID, now
	return draft, nil
}
func (s *Service) fromRow(ctx context.Context, row store.EntryDraft) (Draft, error) {
	doc, err := documents.Parse([]byte(row.DocumentJson))
	if err != nil {
		return Draft{}, fmt.Errorf("parse editor draft: %w", err)
	}
	if err := s.registry.ValidateDraft(doc); err != nil {
		return Draft{}, fmt.Errorf("validate editor draft: %w", err)
	}
	entry, err := s.queries.GetEntry(ctx, row.EntryID)
	if err != nil {
		return Draft{}, fmt.Errorf("get draft entry: %w", err)
	}
	return Draft{EntryID: row.EntryID, Kind: entryKind(entry.ContentTypeID), Title: row.Title, Slug: row.Slug, Document: doc, Version: row.Version, UpdatedBy: row.UpdatedBy, UpdatedAt: row.UpdatedAt}, nil
}
func defaults(schema map[string]blocks.Field) map[string]any {
	values := make(map[string]any, len(schema))
	for name, field := range schema {
		if field.Default != nil {
			values[name] = field.Default
		} else if field.Required {
			switch field.Type {
			case "boolean":
				values[name] = false
			case "integer":
				values[name] = float64(0)
			default:
				values[name] = ""
			}
		}
	}
	return values
}
func entryKind(contentTypeID string) string {
	switch contentTypeID {
	case "content_type_page":
		return "page"
	case "content_type_post":
		return "post"
	default:
		return ""
	}
}
func validateEditorValue(value any, field blocks.Field) error {
	return blocks.ValidateFieldValue(value, field)
}
