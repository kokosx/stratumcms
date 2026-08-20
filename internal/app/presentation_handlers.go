package app

import (
	"fmt"
	"mime"
	"net/http"
	"path"
	"strings"

	"github.com/kokosx/stratumcms/internal/styles"
)

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
	if name == "" || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || path.Clean(name) != name || strings.HasPrefix(name, "../") {
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
	contentType := mime.TypeByExtension(path.Ext(name))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	_, _ = w.Write(data)
}
