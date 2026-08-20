// Package themes provides trusted, embedded presentation themes.
package themes

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
)

//go:embed themes/*
var files embed.FS

type Manifest struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version int    `json:"version"`
}
type Theme struct {
	Manifest  Manifest
	templates map[string]*template.Template
	assets    fs.FS
}
type Registry struct{ themes map[string]Theme }

func NewRegistry() (*Registry, error) {
	r := &Registry{themes: map[string]Theme{}}
	if err := r.RegisterEmbedded("starter"); err != nil {
		return nil, err
	}
	return r, nil
}
func (r *Registry) Register(theme Theme) error {
	if theme.Manifest.ID == "" || theme.Manifest.Name == "" || theme.Manifest.Version < 1 {
		return fmt.Errorf("invalid theme manifest")
	}
	if _, ok := r.themes[theme.Manifest.ID]; ok {
		return fmt.Errorf("theme %q already registered", theme.Manifest.ID)
	}
	r.themes[theme.Manifest.ID] = theme
	return nil
}
func (r *Registry) RegisterEmbedded(id string) error {
	root := "themes/" + id
	data, err := files.ReadFile(root + "/theme.json")
	if err != nil {
		return err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parse theme manifest: %w", err)
	}
	parse := func(name string) (*template.Template, error) {
		return template.ParseFS(files, root+"/templates/layout.html", root+"/templates/"+name+".html")
	}
	page, err := parse("page")
	if err != nil {
		return err
	}
	post, err := parse("post")
	if err != nil {
		return err
	}
	assets, err := fs.Sub(files, root+"/static")
	if err != nil {
		return err
	}
	return r.Register(Theme{Manifest: manifest, templates: map[string]*template.Template{"page": page, "post": post}, assets: assets})
}
func (r *Registry) Resolve(id string) (Theme, bool) { t, ok := r.themes[id]; return t, ok }
func (r *Registry) Definitions() []Manifest {
	out := make([]Manifest, 0, len(r.themes))
	for _, t := range r.themes {
		out = append(out, t.Manifest)
	}
	return out
}
func (t Theme) Execute(w interface{ Write([]byte) (int, error) }, kind string, data any) error {
	tpl, ok := t.templates[kind]
	if !ok {
		tpl = t.templates["page"]
	}
	return tpl.ExecuteTemplate(w, "layout", data)
}
func (t Theme) Asset(name string) ([]byte, error) { return fs.ReadFile(t.assets, name) }
