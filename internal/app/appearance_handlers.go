package app

import (
	"net/http"
	"strconv"

	"github.com/kokosx/stratumcms/internal/styles"
)

type appearanceData struct {
	User             userView
	CSRFToken, Error string
	Settings         styles.Settings
	Themes           []themeView
}
type themeView struct {
	ID, Name string
	Version  int
	Active   bool
}

func (h *handler) appearanceThemes(w http.ResponseWriter, r *http.Request) {
	d, ok := h.appearanceData(w, r)
	if !ok {
		return
	}
	h.render(w, "appearance_themes", d)
}
func (h *handler) appearanceStyles(w http.ResponseWriter, r *http.Request) {
	d, ok := h.appearanceData(w, r)
	if !ok {
		return
	}
	h.render(w, "appearance_styles", d)
}
func (h *handler) appearanceData(w http.ResponseWriter, r *http.Request) (appearanceData, bool) {
	s, err := h.styles.Get(r.Context())
	if err != nil {
		h.internalError(w, err)
		return appearanceData{}, false
	}
	d := appearanceData{User: currentUser(r), CSRFToken: h.csrfToken(w, r), Settings: s}
	for _, t := range h.themes.Definitions() {
		d.Themes = append(d.Themes, themeView{t.ID, t.Name, t.Version, t.ID == s.ActiveTheme})
	}
	return d, true
}
func (h *handler) saveTheme(w http.ResponseWriter, r *http.Request) {
	d, ok := h.appearanceData(w, r)
	if !ok {
		return
	}
	if !h.validCSRF(r) {
		d.Error = "Your form expired. Please try again."
		h.render(w, "appearance_themes", d)
		return
	}
	id := r.FormValue("active_theme")
	if _, exists := h.themes.Resolve(id); !exists {
		d.Error = "Unknown theme."
		h.render(w, "appearance_themes", d)
		return
	}
	if _, err := h.styles.Update(r.Context(), d.Settings.Version, id, d.Settings.Tokens, d.Settings.CustomCSS); err != nil {
		d.Error = err.Error()
		h.render(w, "appearance_themes", d)
		return
	}
	if err := h.pages.InvalidateTag("presentation"); err != nil {
		h.logger.Error("page_cache_invalidate", "tag", "presentation", "error", err)
	}
	http.Redirect(w, r, "/admin/appearance/themes", http.StatusSeeOther)
}
func (h *handler) saveStyles(w http.ResponseWriter, r *http.Request) {
	d, ok := h.appearanceData(w, r)
	if !ok {
		return
	}
	if !h.validCSRF(r) {
		d.Error = "Your form expired. Please try again."
		h.render(w, "appearance_styles", d)
		return
	}
	version, err := strconv.ParseInt(r.FormValue("version"), 10, 64)
	if err != nil || version < 1 {
		d.Error = "Invalid settings version."
		h.render(w, "appearance_styles", d)
		return
	}
	t := styles.Tokens{Background: r.FormValue("background"), Surface: r.FormValue("surface"), Text: r.FormValue("text"), Muted: r.FormValue("muted"), Brand: r.FormValue("brand"), Border: r.FormValue("border"), Body: r.FormValue("body"), Heading: r.FormValue("heading"), RadiusSmall: r.FormValue("radius_small"), RadiusMedium: r.FormValue("radius_medium"), RadiusLarge: r.FormValue("radius_large"), ContainerWidth: r.FormValue("container_width")}
	if _, err := h.styles.Update(r.Context(), version, d.Settings.ActiveTheme, t, r.FormValue("custom_css")); err != nil {
		d.Error = err.Error()
		d.Settings.Tokens = t
		d.Settings.CustomCSS = r.FormValue("custom_css")
		h.render(w, "appearance_styles", d)
		return
	}
	if err := h.pages.InvalidateTag("presentation"); err != nil {
		h.logger.Error("page_cache_invalidate", "tag", "presentation", "error", err)
	}
	http.Redirect(w, r, "/admin/appearance/styles", http.StatusSeeOther)
}
