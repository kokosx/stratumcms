package app

import (
	"net/http"
	"strconv"

	"github.com/kokosx/stratumcms/internal/content"
)

type seoData struct {
	User                 userView
	CSRFToken, Error     string
	EntryID, Kind, Title string
	Version              int64
	SEO                  content.SEO
}

func (h *handler) seoPage(w http.ResponseWriter, r *http.Request) {
	draft, err := h.editor.LoadDraft(r.Context(), r.PathValue("id"), currentUser(r).ID)
	if err != nil {
		h.editorError(w, r, err)
		return
	}
	h.render(w, "seo", seoData{User: currentUser(r), CSRFToken: h.csrfToken(w, r), EntryID: draft.EntryID, Kind: draft.Kind, Title: draft.Title, Version: draft.Version, SEO: draft.SEO})
}
func (h *handler) seoSave(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		http.Error(w, "Forbidden", 403)
		return
	}
	version, err := strconv.ParseInt(r.FormValue("version"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid version", 400)
		return
	}
	_, err = h.editor.UpdateSEO(r.Context(), r.PathValue("id"), currentUser(r).ID, version, content.SEO{Title: r.FormValue("seo_title"), Description: r.FormValue("description"), Canonical: r.FormValue("canonical"), Robots: r.FormValue("robots")})
	if err != nil {
		h.editorError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/editor/"+r.PathValue("id")+"/seo", 303)
}
