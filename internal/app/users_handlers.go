package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/kokosx/stratumcms/internal/users"
)

type usersData struct {
	User             userView
	CSRFToken, Error string
	Users            []users.User
}

func (h *handler) userData(w http.ResponseWriter, r *http.Request) (usersData, bool) {
	items, err := h.users.List(r.Context())
	if err != nil {
		h.internalError(w, err)
		return usersData{}, false
	}
	return usersData{User: currentUser(r), CSRFToken: h.csrfToken(w, r), Users: items}, true
}
func (h *handler) userList(w http.ResponseWriter, r *http.Request) {
	d, ok := h.userData(w, r)
	if ok {
		h.render(w, "users", d)
	}
}
func (h *handler) userCreate(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	err := h.users.Create(r.Context(), users.Input{Email: r.FormValue("email"), Username: r.FormValue("username"), DisplayName: r.FormValue("display_name"), Role: r.FormValue("role"), Password: r.FormValue("password")})
	h.finishUserAction(w, r, err)
}
func (h *handler) userUpdate(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	err := h.users.Update(r.Context(), r.PathValue("id"), users.Input{Email: r.FormValue("email"), Username: r.FormValue("username"), DisplayName: r.FormValue("display_name"), Role: r.FormValue("role")})
	h.finishUserAction(w, r, err)
}
func (h *handler) userPassword(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	err := h.users.ResetPassword(r.Context(), r.PathValue("id"), r.FormValue("password"))
	h.finishUserAction(w, r, err)
}
func (h *handler) finishUserAction(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}
	d, ok := h.userData(w, r)
	if !ok {
		return
	}
	if errors.Is(err, users.ErrValidation) {
		d.Error = strings.TrimPrefix(err.Error(), users.ErrValidation.Error()+": ")
	} else {
		h.internalError(w, err)
		return
	}
	h.render(w, "users", d)
}
