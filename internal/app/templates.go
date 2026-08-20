package app

import (
	"embed"
	"html/template"
	"net/http"
)

//go:embed web/templates/*.html web/static/*.css
var webFiles embed.FS

func parseTemplates() (*template.Template, error) {
	return template.ParseFS(webFiles, "web/templates/*.html")
}
func css(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	contents, _ := webFiles.ReadFile("web/static/app.css")
	_, _ = w.Write(contents)
}
func datastarJS(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	contents, _ := webFiles.ReadFile("web/static/datastar-1.0.0.js")
	_, _ = w.Write(contents)
}
func editorCSS(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	contents, _ := webFiles.ReadFile("web/static/editor.css")
	_, _ = w.Write(contents)
}
