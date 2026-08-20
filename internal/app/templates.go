package app

import (
	"embed"
	"html/template"
	"net/http"
)

//go:embed web/templates/*.html web/static/*.css
var webFiles embed.FS

func parseTemplates() (*template.Template, error) {
	return template.New("root").Funcs(template.FuncMap{"fontOptions": fontOptions}).ParseFS(webFiles, "web/templates/*.html")
}

type fontOption struct {
	Value    string
	Selected bool
}

func fontOptions(selected string) []fontOption {
	names := []string{"system", "sans", "serif", "mono"}
	out := make([]fontOption, 0, len(names))
	for _, name := range names {
		out = append(out, fontOption{name, name == selected})
	}
	return out
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
