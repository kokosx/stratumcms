// Package publishing coordinates cache work after a successful editor publish.
package publishing

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"

	"github.com/kokosx/stratumcms/internal/cache"
	"github.com/kokosx/stratumcms/internal/content"
	"github.com/kokosx/stratumcms/internal/editor"
	"github.com/kokosx/stratumcms/internal/presentation"
)

type Service struct {
	content      *content.Service
	pages        *cache.Pages
	presentation *presentation.Service
	logger       *slog.Logger
}

func New(c *content.Service, pages *cache.Pages, p *presentation.Service, logger *slog.Logger) *Service {
	return &Service{c, pages, p, logger}
}
func (s *Service) Publish(ctx context.Context, editorService *editor.Service, entryID, userID string, version int64) (editor.Draft, error) {
	draft, err := editorService.Publish(ctx, entryID, userID, version)
	if err != nil {
		return editor.Draft{}, err
	}
	if err := s.pages.InvalidateTag("entry:" + entryID); err != nil {
		s.logger.Error("page_cache_invalidate", "tag", "entry:"+entryID, "error", err)
	} else {
		s.logger.Debug("page_cache_invalidate", "tag", "entry:"+entryID)
	}
	path := "/" + content.Slug(draft.Slug)
	if draft.Kind == "post" {
		path = "/blog/" + content.Slug(draft.Slug)
	}
	published, err := s.content.ResolvePublished(ctx, path)
	if err != nil {
		s.logger.Error("warm published page: resolve", "entry_id", entryID, "error", err)
		return draft, nil
	}
	result, err := s.presentation.Render(ctx, published.Kind, published.Title, published.Document)
	if err != nil {
		s.logger.Error("warm published page: render", "entry_id", entryID, "error", err)
		return draft, nil
	}
	entry := cache.Entry{Path: published.Path, HTML: result.HTML, ETag: ETag(result.HTML), Dependencies: []string{"entry:" + published.EntryID, "revision:" + published.RevisionID, "theme:" + result.ThemeID, "presentation", "route:" + published.Path}}
	if err := s.pages.Put(entry); err != nil {
		s.logger.Error("warm published page: cache", "entry_id", entryID, "error", err)
	} else {
		s.logger.Debug("page_cache_write", "path", entry.Path)
	}
	return draft, nil
}
func ETag(html []byte) string         { return fmt.Sprintf("\"%x\"", cacheHash(html)) }
func cacheHash(value []byte) [32]byte { return sha256.Sum256(value) }
