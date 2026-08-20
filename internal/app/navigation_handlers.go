package app

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/kokosx/stratumcms/internal/content"
	"github.com/kokosx/stratumcms/internal/menus"
	"github.com/kokosx/stratumcms/internal/redirects"
)

type menusData struct {
	User             userView
	CSRFToken, Error string
	Items            []menus.Item
	Entries          []content.Entry
}

func (h *handler) menusPage(w http.ResponseWriter, r *http.Request) {
	d := menusData{User: currentUser(r), CSRFToken: h.csrfToken(w, r)}
	d.Items, _ = h.menus.Primary(r.Context())
	pages, _ := h.content.ListEntries(r.Context(), "page")
	posts, _ := h.content.ListEntries(r.Context(), "post")
	d.Entries = append(pages, posts...)
	h.render(w, "menus", d)
}
func (h *handler) menuAdd(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		http.Error(w, "Forbidden", 403)
		return
	}
	err := h.menus.Add(r.Context(), r.FormValue("label"), r.FormValue("item_type"), r.FormValue("entry_id"), r.FormValue("url"))
	if err != nil {
		d := menusData{User: currentUser(r), CSRFToken: h.csrfToken(w, r), Error: strings.TrimPrefix(err.Error(), menus.ErrValidation.Error()+": ")}
		d.Items, _ = h.menus.Primary(r.Context())
		d.Entries, _ = h.content.ListEntries(r.Context(), "page")
		h.render(w, "menus", d)
		return
	}
	h.invalidatePresentation()
	http.Redirect(w, r, "/admin/appearance/menus", 303)
}
func (h *handler) menuDelete(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		http.Error(w, "Forbidden", 403)
		return
	}
	if err := h.menus.Delete(r.Context(), r.PathValue("id")); err != nil {
		h.internalError(w, err)
		return
	}
	h.invalidatePresentation()
	http.Redirect(w, r, "/admin/appearance/menus", 303)
}
func (h *handler) menuMove(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		http.Error(w, "Forbidden", 403)
		return
	}
	delta, _ := strconv.Atoi(r.FormValue("delta"))
	if delta != -1 && delta != 1 {
		http.Error(w, "Invalid move", 400)
		return
	}
	if err := h.menus.Move(r.Context(), r.PathValue("id"), delta); err != nil {
		h.internalError(w, err)
		return
	}
	h.invalidatePresentation()
	http.Redirect(w, r, "/admin/appearance/menus", 303)
}
func (h *handler) invalidatePresentation() {
	if err := h.pages.InvalidateTag("presentation"); err != nil {
		h.logger.Error("page_cache_invalidate", "tag", "presentation", "error", err)
	}
}

type redirectsData struct {
	User             userView
	CSRFToken, Error string
	Rules            []redirects.Rule
}

func (h *handler) redirectsPage(w http.ResponseWriter, r *http.Request) {
	rules, err := h.redirects.List(r.Context())
	if err != nil {
		h.internalError(w, err)
		return
	}
	h.render(w, "redirects", redirectsData{User: currentUser(r), CSRFToken: h.csrfToken(w, r), Rules: rules})
}
func (h *handler) redirectCreate(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		http.Error(w, "Forbidden", 403)
		return
	}
	status, _ := strconv.Atoi(r.FormValue("status_code"))
	_, err := h.redirects.Create(r.Context(), r.FormValue("from_path"), r.FormValue("to_path"), status)
	if err != nil {
		rules, _ := h.redirects.List(r.Context())
		message := "Unable to create redirect."
		if errors.Is(err, redirects.ErrValidation) {
			message = strings.TrimPrefix(err.Error(), redirects.ErrValidation.Error()+": ")
		}
		h.render(w, "redirects", redirectsData{User: currentUser(r), CSRFToken: h.csrfToken(w, r), Rules: rules, Error: message})
		return
	}
	_ = h.pages.InvalidatePath(r.FormValue("from_path"))
	http.Redirect(w, r, "/admin/redirects", 303)
}
func (h *handler) redirectDelete(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		http.Error(w, "Forbidden", 403)
		return
	}
	if err := h.redirects.Delete(r.Context(), r.PathValue("id")); err != nil {
		h.internalError(w, err)
		return
	}
	http.Redirect(w, r, "/admin/redirects", 303)
}
