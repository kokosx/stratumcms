package app

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"

	"github.com/kokosx/stratumcms/internal/media"
)

func (h *handler) publicMedia(w http.ResponseWriter, r *http.Request) {
	if !media.SafePublicID(r.PathValue("id")) {
		http.NotFound(w, r)
		return
	}
	i, err := h.media.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, media.ErrNotFound) || filepath.Base(i.StorageKey) != r.PathValue("filename") {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.internalError(w, err)
		return
	}
	f, err := http.Dir(filepath.Dir(h.media.File(i))).Open(filepath.Base(h.media.File(i)))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", i.MIMEType)
	w.Header().Set("Content-Length", fmt.Sprint(info.Size()))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, f)
}
func (h *handler) mediaList(w http.ResponseWriter, r *http.Request) {
	items, err := h.media.List(r.Context())
	if err != nil {
		h.internalError(w, err)
		return
	}
	h.render(w, "media", struct {
		User      userView
		CSRFToken string
		Items     []media.Item
	}{currentUser(r), h.csrfToken(w, r), items})
}
func (h *handler) mediaMetadata(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		http.Error(w, "Invalid request", 403)
		return
	}
	if err := h.media.UpdateMetadata(r.Context(), r.PathValue("id"), r.FormValue("alt_text"), r.FormValue("caption")); err != nil {
		h.internalError(w, err)
		return
	}
	_ = h.pages.InvalidateTag("media:" + r.PathValue("id"))
	http.Redirect(w, r, "/admin/media", 303)
}
func (h *handler) mediaUpload(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		http.Error(w, "Invalid request", 403)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, media.MaxUploadSize+1024)
	if err := r.ParseMultipartForm(media.MaxUploadSize); err != nil {
		http.Error(w, "Upload too large", 400)
		return
	}
	f, head, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Image file is required", 400)
		return
	}
	defer f.Close()
	if _, err = h.media.Upload(r.Context(), currentUser(r).ID, head.Filename, f); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	http.Redirect(w, r, "/admin/media", 303)
}
func (h *handler) mediaDelete(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		http.Error(w, "Invalid request", 403)
		return
	}
	if err := h.media.Delete(r.Context(), r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	http.Redirect(w, r, "/admin/media", 303)
}
