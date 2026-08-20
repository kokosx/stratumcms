package app

import (
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/kokosx/stratumcms/internal/styles"
)

type presentationData struct {
	Title                   string
	Content                 template.HTML
	SiteStyles, ThemeStyles string
}

func (h *handler) renderPresentation(w http.ResponseWriter, r *http.Request, kind, title string, body template.HTML) {
	settings, err := h.styles.Get(r.Context())
	if err != nil {
		h.internalError(w, err)
		return
	}
	theme, ok := h.themes.Resolve(settings.ActiveTheme)
	if !ok {
		h.internalError(w, fmt.Errorf("active theme %q not found", settings.ActiveTheme))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := presentationData{Title: title, Content: body, ThemeStyles: "/assets/themes/" + settings.ActiveTheme + "/theme.css?v=" + strconv.Itoa(theme.Manifest.Version), SiteStyles: "/assets/site.css?v=" + strconv.FormatInt(settings.Version, 10)}
	if err := theme.Execute(w, kind, data); err != nil {
		h.logger.Error("render theme", "error", err)
		h.internalError(w, err)
	}
}
func (h *handler) siteCSS(w http.ResponseWriter, r *http.Request) {
	settings, err := h.styles.Get(r.Context())
	if err != nil {
		h.internalError(w, err)
		return
	}
	etag := fmt.Sprintf("\"site-%d\"", settings.Version)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = w.Write([]byte(styles.CSS(settings)))
}
func (h *handler) themeAsset(w http.ResponseWriter, r *http.Request) {
	id, name := r.PathValue("theme"), r.PathValue("asset")
	if strings.Contains(name, "/") || name == "" {
		http.NotFound(w, r)
		return
	}
	theme, ok := h.themes.Resolve(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	data, err := theme.Asset(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	_, _ = w.Write(data)
}
