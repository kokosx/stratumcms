// Package renderer converts validated SDT documents to safe HTML.
package renderer

import (
	"bytes"
	"fmt"
	"html/template"

	"github.com/kokosx/stratumcms/internal/blocks"
	"github.com/kokosx/stratumcms/internal/documents"
)

type Renderer struct{ registry *blocks.Registry }

func New(registry *blocks.Registry) *Renderer { return &Renderer{registry: registry} }
func (r *Renderer) Render(document documents.Document) (template.HTML, error) {
	if err := r.registry.Validate(document); err != nil {
		return "", err
	}
	var renderNodes func([]documents.Node) (template.HTML, error)
	renderNodes = func(nodes []documents.Node) (template.HTML, error) {
		var b bytes.Buffer
		for _, node := range nodes {
			children, err := renderNodes(node.Children)
			if err != nil {
				return "", err
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
