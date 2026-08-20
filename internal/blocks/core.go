package blocks

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"

	"github.com/kokosx/stratumcms/internal/documents"
)

//go:embed templates/*.html
var coreTemplates embed.FS

func CoreRegistry() *Registry {
	r := NewRegistry()
	register := func(def Definition, name string) {
		if err := r.Register(def, templateRenderer(name, def)); err != nil {
			panic(err)
		}
	}
	register(Definition{ID: "core.heading", Name: "Heading", Version: 1, Category: "text", Props: map[string]Field{"text": {Type: "text", Required: true}}, Settings: map[string]Field{"level": {Type: "enum", Default: "2", Options: []string{"1", "2", "3", "4", "5", "6"}}}}, "heading")
	register(Definition{ID: "core.text", Name: "Text", Version: 1, Category: "text", Props: map[string]Field{"text": {Type: "textarea", Required: true}}, Settings: map[string]Field{}}, "text")
	register(Definition{ID: "core.button", Name: "Button", Version: 1, Category: "action", Props: map[string]Field{"label": {Type: "text", Required: true}, "url": {Type: "url", Required: true}}, Settings: map[string]Field{}}, "button")
	register(Definition{ID: "core.hero", Name: "Hero", Version: 1, Category: "layout", Props: map[string]Field{"heading": {Type: "text", Required: true}, "description": {Type: "textarea"}}, Settings: map[string]Field{"variant": {Type: "enum", Default: "default", Options: []string{"default", "brand"}}}}, "hero")
	register(Definition{ID: "core.container", Name: "Container", Version: 1, Category: "layout", Props: map[string]Field{}, Settings: map[string]Field{}, AllowsChildren: true}, "container")
	return r
}
func templateRenderer(name string, definition Definition) Renderer {
	return func(node documents.Node, children template.HTML) (template.HTML, error) {
		node = withDefaults(node, definition)
		tmpl, err := template.ParseFS(coreTemplates, "templates/*.html")
		if err != nil {
			return "", err
		}
		var b bytes.Buffer
		if err := tmpl.ExecuteTemplate(&b, name, struct {
			Node     documents.Node
			Children template.HTML
		}{node, children}); err != nil {
			return "", fmt.Errorf("render %s: %w", node.Type, err)
		}
		return template.HTML(b.String()), nil
	}
}

func withDefaults(node documents.Node, definition Definition) documents.Node {
	node.Props = mapWithDefaults(node.Props, definition.Props)
	node.Settings = mapWithDefaults(node.Settings, definition.Settings)
	return node
}
func mapWithDefaults(values map[string]any, schema map[string]Field) map[string]any {
	copy := make(map[string]any, len(values)+len(schema))
	for name, value := range values {
		copy[name] = value
	}
	for name, field := range schema {
		if _, exists := copy[name]; !exists && field.Default != nil {
			copy[name] = field.Default
		}
	}
	return copy
}
