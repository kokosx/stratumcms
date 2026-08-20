// Package renderer converts validated SDT documents to safe HTML.
package renderer

import (
	"bytes"
	"fmt"
	"html"
	"html/template"

	"github.com/kokosx/stratumcms/internal/blocks"
	"github.com/kokosx/stratumcms/internal/documents"
)

type MediaResolver interface {
	ResolveMedia(id string) (url, alt string, err error)
}
type Renderer struct {
	registry *blocks.Registry
	media    MediaResolver
}

func New(registry *blocks.Registry, media ...MediaResolver) *Renderer {
	r := &Renderer{registry: registry}
	if len(media) > 0 {
		r.media = media[0]
	}
	return r
}
func (r *Renderer) Render(document documents.Document) (template.HTML, error) {
	if err := r.registry.Validate(document); err != nil {
		return "", err
	}
	return r.render(document)
}
func (r *Renderer) render(document documents.Document) (template.HTML, error) {
	var renderNodes func([]documents.Node) (template.HTML, error)
	renderNodes = func(nodes []documents.Node) (template.HTML, error) {
		var b bytes.Buffer
		for _, node := range nodes {
			children, err := renderNodes(node.Children)
			if err != nil {
				return "", err
			}
			if node.Type == "core.image" {
				if r.media == nil {
					return "", fmt.Errorf("render image: no media resolver")
				}
				id, _ := node.Props["media"].(string)
				url, fallback, err := r.media.ResolveMedia(id)
				if err != nil {
					return "", fmt.Errorf("resolve image media: %w", err)
				}
				alt, _ := node.Props["alt"].(string)
				if alt == "" {
					alt = fallback
				}
				caption, _ := node.Props["caption"].(string)
				size, _ := node.Settings["size"].(string)
				if size == "" {
					size = "default"
				}
				fragment := `<figure class="image image-` + html.EscapeString(size) + `"><img data-node-id="` + html.EscapeString(node.ID) + `" data-block-type="core.image" src="` + html.EscapeString(url) + `" alt="` + html.EscapeString(alt) + `">`
				if caption != "" {
					fragment += `<figcaption>` + html.EscapeString(caption) + `</figcaption>`
				}
				fragment += `</figure>`
				b.WriteString(fragment)
				continue
			}
			_, render, err := r.registry.Resolve(node.Type, node.Version)
			if err != nil {
				return "", err
			}
			fragment, err := render(node, children)
			if err != nil {
				return "", err
			}
			b.WriteString(string(fragment))
		}
		return template.HTML(b.String()), nil
	}
	result, err := renderNodes(document.Children)
	if err != nil {
		return "", fmt.Errorf("render document: %w", err)
	}
	return result, nil
}

// RenderDraft renders a structurally valid draft without allowing it to be
// published until required user-facing fields pass strict validation.
func (r *Renderer) RenderDraft(document documents.Document) (template.HTML, error) {
	if err := r.registry.ValidateDraft(document); err != nil {
		return "", err
	}
	return r.render(document)
}
