package app

import (
	"bytes"
	"database/sql"
	"errors"
	"html/template"
	"net/http"
	"strconv"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/kokosx/stratumcms/internal/blocks"
	"github.com/kokosx/stratumcms/internal/documents"
	"github.com/kokosx/stratumcms/internal/editor"
)

type editorData struct {
	User      userView
	CSRFToken string
	Kind      string
	NodeID    string
	Props     map[string]any
	Draft     editor.Draft
	Blocks    []blocks.Definition
	Inspector []editorInspector
	Error     string
}
type editorInspector struct {
	NodeID          string
	Definition      blocks.Definition
	Props, Settings map[string]any
}

func (h *handler) editorPage(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entryID := r.PathValue("id")
		if kind != "" {
			if _, err := h.content.GetEntryByType(r.Context(), entryID, kind); err != nil {
				if errors.Is(err, sqlErrNoRows()) {
					http.NotFound(w, r)
				} else {
					h.internalError(w, err)
				}
				return
			}
		}
		draft, err := h.editor.LoadDraft(r.Context(), entryID, currentUser(r).ID)
		if err != nil {
			h.editorError(w, r, err)
			return
		}
		h.render(w, "editor", h.editorData(currentUser(r), h.csrfToken(w, r), kind, draft))
	}
}

func (h *handler) editorPreview(w http.ResponseWriter, r *http.Request) {
	draft, err := h.editor.LoadDraft(r.Context(), r.PathValue("id"), currentUser(r).ID)
	if err != nil {
		h.editorError(w, r, err)
		return
	}
	body, err := h.renderer.RenderDraft(draft.Document)
	if err != nil {
		h.internalError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.publicTemplate.Execute(w, struct {
		Title   string
		Content template.HTML
	}{draft.Title, body}); err != nil {
		h.internalError(w, err)
	}
}

func (h *handler) editorAddBlock(w http.ResponseWriter, r *http.Request) {
	h.editorMutation(w, r, func(v editorValues) (editor.Draft, error) {
		return h.editor.AddBlock(r.Context(), r.PathValue("id"), currentUser(r).ID, v.version, v.parentID, v.blockID)
	})
}
func (h *handler) editorUpdateBlock(w http.ResponseWriter, r *http.Request) {
	h.editorMutation(w, r, func(v editorValues) (editor.Draft, error) {
		return h.editor.UpdateBlock(r.Context(), r.PathValue("id"), currentUser(r).ID, v.version, r.PathValue("nodeID"), v.group, v.field, v.value)
	})
}
func (h *handler) editorDeleteBlock(w http.ResponseWriter, r *http.Request) {
	h.editorMutation(w, r, func(v editorValues) (editor.Draft, error) {
		return h.editor.DeleteBlock(r.Context(), r.PathValue("id"), currentUser(r).ID, v.version, r.PathValue("nodeID"))
	})
}
func (h *handler) editorDuplicateBlock(w http.ResponseWriter, r *http.Request) {
	h.editorMutation(w, r, func(v editorValues) (editor.Draft, error) {
		return h.editor.DuplicateBlock(r.Context(), r.PathValue("id"), currentUser(r).ID, v.version, r.PathValue("nodeID"))
	})
}
func (h *handler) editorMoveBlock(w http.ResponseWriter, r *http.Request) {
	h.editorMutation(w, r, func(v editorValues) (editor.Draft, error) {
		return h.editor.MoveBlock(r.Context(), r.PathValue("id"), currentUser(r).ID, v.version, r.PathValue("nodeID"), v.parentID, v.index)
	})
}
func (h *handler) editorMetadata(w http.ResponseWriter, r *http.Request) {
	h.editorMutation(w, r, func(v editorValues) (editor.Draft, error) {
		return h.editor.UpdateMetadata(r.Context(), r.PathValue("id"), currentUser(r).ID, v.version, v.title, v.slug)
	})
}
func (h *handler) editorSave(w http.ResponseWriter, r *http.Request) {
	h.editorMutation(w, r, func(v editorValues) (editor.Draft, error) {
		return h.editor.SaveDraft(r.Context(), r.PathValue("id"), currentUser(r).ID, v.version)
	})
}
func (h *handler) editorPublish(w http.ResponseWriter, r *http.Request) {
	h.editorMutation(w, r, func(v editorValues) (editor.Draft, error) {
		return h.editor.Publish(r.Context(), r.PathValue("id"), currentUser(r).ID, v.version)
	})
}

func (h *handler) editorMutation(w http.ResponseWriter, r *http.Request, mutate func(editorValues) (editor.Draft, error)) {
	values, err := readEditorValues(r)
	if err != nil || !h.validEditorCSRF(r, values.csrf) {
		http.Error(w, "Invalid editor request", http.StatusForbidden)
		return
	}
	draft, err := mutate(values)
	if err != nil {
		h.editorError(w, r, err)
		return
	}
	if r.Header.Get("Accept") != "text/event-stream" {
		http.Redirect(w, r, "/admin/editor/"+draft.EntryID, http.StatusSeeOther)
		return
	}
	h.patchEditor(w, r, h.editorData(currentUser(r), values.csrf, "", draft))
}
func (h *handler) editorData(user userView, csrf, kind string, draft editor.Draft) editorData {
	data := editorData{User: user, CSRFToken: csrf, Kind: kind, Draft: draft, Blocks: h.editor.Registry().Definitions()}
	var walk func([]documents.Node)
	walk = func(nodes []documents.Node) {
		for _, node := range nodes {
			def, _, err := h.editor.Registry().Resolve(node.Type, node.Version)
			if err == nil {
				data.Inspector = append(data.Inspector, editorInspector{NodeID: node.ID, Definition: def, Props: node.Props, Settings: node.Settings})
			}
			walk(node.Children)
		}
	}
	walk(draft.Document.Children)
	return data
}
func (h *handler) patchEditor(w http.ResponseWriter, r *http.Request, data editorData) {
	var b bytes.Buffer
	if err := h.templates.ExecuteTemplate(&b, "editor_workspace", data); err != nil {
		h.internalError(w, err)
		return
	}
	sse := datastar.NewSSE(w, r)
	if err := sse.PatchElements(b.String()); err != nil {
		h.logger.Error("patch editor", "error", err)
	}
}
func (h *handler) editorError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, editor.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, editor.ErrConflict) {
		http.Error(w, "This draft changed in another tab. Reload to continue.", http.StatusConflict)
		return
	}
	if errors.Is(err, editor.ErrValidation) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.internalError(w, err)
}

type editorValues struct {
	csrf, parentID, blockID, group, field, title, slug string
	version                                            int64
	index                                              int
	value                                              any
}

func readEditorValues(r *http.Request) (editorValues, error) {
	v := editorValues{}
	if r.Header.Get("Content-Type") == "application/json" {
		var signals map[string]any
		if err := datastar.ReadSignals(r, &signals); err != nil {
			return v, err
		}
		stringValue := func(key string) string {
			if value, ok := signals[key].(string); ok {
				return value
			}
			return ""
		}
		v.csrf, v.parentID, v.blockID, v.group, v.field, v.title, v.slug = stringValue("csrf_token"), stringValue("parent_id"), stringValue("block_id"), stringValue("group"), stringValue("field"), stringValue("title"), stringValue("slug")
		v.value = signals["value"]
		if n, ok := signals["version"].(float64); ok {
			v.version = int64(n)
		}
		if n, ok := signals["index"].(float64); ok {
			v.index = int(n)
		}
		return v, nil
	}
	if err := r.ParseForm(); err != nil {
		return v, err
	}
	v.csrf, v.parentID, v.blockID, v.group, v.field, v.title, v.slug = r.FormValue("csrf_token"), r.FormValue("parent_id"), r.FormValue("block_id"), r.FormValue("group"), r.FormValue("field"), r.FormValue("title"), r.FormValue("slug")
	var err error
	v.version, err = strconv.ParseInt(r.FormValue("version"), 10, 64)
	if err != nil {
		return v, err
	}
	v.index, _ = strconv.Atoi(r.FormValue("index"))
	v.value = r.FormValue("value")
	return v, nil
}
func (h *handler) validEditorCSRF(r *http.Request, token string) bool {
	r.Form = map[string][]string{"csrf_token": {token}}
	return h.validCSRF(r)
}
func sqlErrNoRows() error { return sql.ErrNoRows }
