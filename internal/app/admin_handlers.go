package app

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/kokosx/stratumcms/internal/content"
)

type userView struct{ ID, DisplayName string }
type adminListData struct {
	User                      userView
	CSRFToken, Kind, Singular string
	Entries                   []content.Entry
}
type adminEditData struct {
	User                             userView
	CSRFToken, Kind, Singular, Error string
	Entry                            content.Entry
	Revisions                        []content.Revision
	IsNew                            bool
}
type userContextKey struct{}

func (h *handler) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := h.user(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userContextKey{}, userView{ID: u.ID, DisplayName: u.DisplayName})))
	})
}
func currentUser(r *http.Request) userView {
	u, _ := r.Context().Value(userContextKey{}).(userView)
	return u
}
func (h *handler) entries(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := h.content.ListEntries(r.Context(), kind)
		if err != nil {
			h.internalError(w, err)
			return
		}
		h.render(w, "admin_list", adminListData{User: currentUser(r), CSRFToken: h.csrfToken(w, r), Kind: kind, Singular: strings.Title(kind), Entries: items})
	}
}
func (h *handler) newEntry(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.render(w, "admin_edit", adminEditData{User: currentUser(r), CSRFToken: h.csrfToken(w, r), Kind: kind, Singular: strings.Title(kind), IsNew: true})
	}
}
func (h *handler) createEntry(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.validCSRF(r) {
			h.entryError(w, r, kind, true, content.Entry{}, "Your form expired. Please try again.")
			return
		}
		entry, err := h.content.CreateEntry(r.Context(), kind, currentUser(r).ID, content.Input{Title: r.FormValue("title"), Slug: r.FormValue("slug")})
		if err != nil {
			h.entryError(w, r, kind, true, content.Entry{Title: r.FormValue("title"), Slug: r.FormValue("slug")}, entryError(err))
			return
		}
		http.Redirect(w, r, "/admin/"+kind+"s/"+entry.ID+"/edit", http.StatusSeeOther)
	}
}
func (h *handler) editEntry(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entry, err := h.content.GetEntry(r.Context(), r.PathValue("id"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		revisions, err := h.content.ListRevisions(r.Context(), entry.ID)
		if err != nil {
			h.internalError(w, err)
			return
		}
		h.render(w, "admin_edit", adminEditData{User: currentUser(r), CSRFToken: h.csrfToken(w, r), Kind: kind, Singular: strings.Title(kind), Entry: entry, Revisions: revisions})
	}
}
func (h *handler) updateEntry(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entry, err := h.content.GetEntry(r.Context(), r.PathValue("id"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if !h.validCSRF(r) {
			h.entryError(w, r, kind, false, entry, "Your form expired. Please try again.")
			return
		}
		in := content.Input{Title: r.FormValue("title"), Slug: r.FormValue("slug")}
		if r.FormValue("action") == "publish" {
			_, err = h.content.PublishEntry(r.Context(), entry.ID, currentUser(r).ID, in)
		} else {
			_, err = h.content.SaveEntry(r.Context(), entry.ID, currentUser(r).ID, in)
		}
		if err != nil {
			entry.Title = in.Title
			entry.Slug = in.Slug
			h.entryError(w, r, kind, false, entry, entryError(err))
			return
		}
		http.Redirect(w, r, "/admin/"+kind+"s/"+entry.ID+"/edit", http.StatusSeeOther)
	}
}
func (h *handler) entryError(w http.ResponseWriter, r *http.Request, kind string, isNew bool, entry content.Entry, message string) {
	revisions, _ := h.content.ListRevisions(r.Context(), entry.ID)
	h.render(w, "admin_edit", adminEditData{User: currentUser(r), CSRFToken: h.csrfToken(w, r), Kind: kind, Singular: strings.Title(kind), Entry: entry, Revisions: revisions, IsNew: isNew, Error: message})
}
func entryError(err error) string {
	if errors.Is(err, content.ErrValidation) {
		return strings.TrimPrefix(err.Error(), content.ErrValidation.Error()+": ")
	}
	return "Unable to save this entry. Please try again."
}
