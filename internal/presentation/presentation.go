// Package presentation produces complete public HTML before it reaches HTTP.
package presentation

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"strconv"

	"github.com/kokosx/stratumcms/internal/content"
	"github.com/kokosx/stratumcms/internal/documents"
	"github.com/kokosx/stratumcms/internal/menus"
	"github.com/kokosx/stratumcms/internal/renderer"
	"github.com/kokosx/stratumcms/internal/styles"
	"github.com/kokosx/stratumcms/internal/themes"
)

type Result struct {
	HTML          []byte
	ThemeID       string
	ThemeVersion  int
	StylesVersion int64
}
type Service struct {
	renderer *renderer.Renderer
	styles   *styles.Service
	themes   *themes.Registry
	menus    *menus.Service
}
type data struct {
	Title                          string
	Content                        template.HTML
	SiteStyles, ThemeStyles        string
	Navigation                     []menus.Item
	Description, Canonical, Robots string
}

func New(r *renderer.Renderer, s *styles.Service, themeRegistry *themes.Registry, menuServices ...*menus.Service) *Service {
	service := &Service{renderer: r, styles: s, themes: themeRegistry}
	if len(menuServices) > 0 {
		service.menus = menuServices[0]
	}
	return service
}
func (s *Service) Render(ctx context.Context, kind, title string, document documents.Document, seos ...content.SEO) (Result, error) {
	body, err := s.renderer.Render(document)
	if err != nil {
		return Result{}, fmt.Errorf("render document: %w", err)
	}
	return s.finish(ctx, kind, title, body, seos...)
}
func (s *Service) RenderDraft(ctx context.Context, kind, title string, document documents.Document, seos ...content.SEO) (Result, error) {
	body, err := s.renderer.RenderDraft(document)
	if err != nil {
		return Result{}, fmt.Errorf("render draft document: %w", err)
	}
	return s.finish(ctx, kind, title, body, seos...)
}
func (s *Service) finish(ctx context.Context, kind, title string, body template.HTML, seos ...content.SEO) (Result, error) {
	settings, err := s.styles.Get(ctx)
	if err != nil {
		return Result{}, err
	}
	theme, ok := s.themes.Resolve(settings.ActiveTheme)
	if !ok {
		return Result{}, fmt.Errorf("active theme %q not found", settings.ActiveTheme)
	}
	var out bytes.Buffer
	d := data{Title: title, Content: body, ThemeStyles: "/assets/themes/" + settings.ActiveTheme + "/theme.css?v=" + strconv.Itoa(theme.Manifest.Version), SiteStyles: "/assets/site.css?v=" + strconv.FormatInt(settings.Version, 10)}
	if len(seos) > 0 {
		seo := seos[0]
		if seo.Title != "" {
			d.Title = seo.Title
		}
		d.Description = seo.Description
		d.Canonical = seo.Canonical
		d.Robots = seo.Robots
	}
	if s.menus != nil {
		d.Navigation, _ = s.menus.Primary(ctx)
	}
	if err := theme.Execute(&out, kind, d); err != nil {
		return Result{}, fmt.Errorf("render theme: %w", err)
	}
	return Result{HTML: out.Bytes(), ThemeID: settings.ActiveTheme, ThemeVersion: theme.Manifest.Version, StylesVersion: settings.Version}, nil
}
