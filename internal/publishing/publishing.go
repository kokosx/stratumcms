// Package publishing coordinates cache work after a successful editor publish.
package publishing

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"

	"github.com/kokosx/stratumcms/internal/cache"
	"github.com/kokosx/stratumcms/internal/content"
	"github.com/kokosx/stratumcms/internal/documents"
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
	before, _ := s.content.GetEntry(ctx, entryID)
	draft, err := editorService.Publish(ctx, entryID, userID, version)
	if err != nil {
		return editor.Draft{}, err
	}
	if before.Route != "" {
		if err := s.pages.InvalidatePath(before.Route); err != nil {
			s.logger.Error("page_cache_invalidate", "path", before.Route, "error", err)
		}
	}
	if err := s.pages.InvalidateTag("entry:" + entryID); err != nil {
		s.logger.Error("page_cache_invalidate", "tag", "entry:"+entryID, "error", err)
	} else {
		s.logger.Debug("page_cache_invalidate", "tag", "entry:"+entryID)
	}
	published, err := s.content.GetPublishedByEntryID(ctx, entryID)
	if err != nil {
		s.logger.Error("warm published page: resolve", "entry_id", entryID, "error", err)
		return draft, nil
	}
	if published.SEO.Canonical == "" {
		published.SEO.Canonical = published.Path
	}
	if published.SEO.Robots == "" {
		published.SEO.Robots = "index,follow"
	}
	result, err := s.presentation.Render(ctx, published.Kind, published.Title, published.Document, published.SEO)
	if err != nil {
		s.logger.Error("warm published page: render", "entry_id", entryID, "error", err)
		return draft, nil
	}
	entry := cache.Entry{Path: published.Path, HTML: result.HTML, ETag: ETag(result.HTML), Dependencies: dependencies(published.Document, published.EntryID, published.RevisionID, published.Path, result.ThemeID)}
	if err := s.pages.Put(entry); err != nil {
		s.logger.Error("warm published page: cache", "entry_id", entryID, "error", err)
	} else {
		s.logger.Debug("page_cache_write", "path", entry.Path)
	}
	return draft, nil
}
func ETag(html []byte) string         { return fmt.Sprintf("\"%x\"", cacheHash(html)) }
func cacheHash(value []byte) [32]byte { return sha256.Sum256(value) }

func dependencies(document documents.Document, entryID, revisionID, route, themeID string) []string {
	deps := []string{"entry:" + entryID, "revision:" + revisionID, "theme:" + themeID, "presentation", "route:" + route}
	seen := map[string]bool{}
	var walk func([]documents.Node)
	walk = func(nodes []documents.Node) {
		for _, n := range nodes {
			if n.Type == "core.image" {
				if mediaID, ok := n.Props["media"].(string); ok && mediaID != "" && !seen[mediaID] {
					deps = append(deps, "media:"+mediaID)
					seen[mediaID] = true
				}
			}
			walk(n.Children)
		}
	}
	walk(document.Children)
	return deps
}
