// Package blocks contains block definitions, validation, and the core registry.
package blocks

import (
	"fmt"
	"html/template"
	"net/url"
	"sort"
	"strings"

	"github.com/kokosx/stratumcms/internal/documents"
)

type Field struct {
	Type     string
	Required bool
	Default  any
	Options  []string
}
type Definition struct {
	ID, Name        string
	Version         int
	Category        string
	Props, Settings map[string]Field
	AllowsChildren  bool
}
type Renderer func(documents.Node, template.HTML) (template.HTML, error)
type Registry struct {
	definitions map[string]Definition
	renderers   map[string]Renderer
}

func NewRegistry() *Registry {
	return &Registry{definitions: map[string]Definition{}, renderers: map[string]Renderer{}}
}
func key(id string, version int) string { return fmt.Sprintf("%s@%d", id, version) }
func (r *Registry) Register(def Definition, render Renderer) error {
	if !strings.Contains(def.ID, ".") || def.Version < 1 {
		return fmt.Errorf("block ID must be namespaced and version must be positive")
	}
	k := key(def.ID, def.Version)
	if _, exists := r.definitions[k]; exists {
		return fmt.Errorf("block %s already registered", k)
	}
	if render == nil {
		return fmt.Errorf("block %s has no renderer", k)
	}
	r.definitions[k], r.renderers[k] = def, render
	return nil
}
func (r *Registry) Resolve(id string, version int) (Definition, Renderer, error) {
	k := key(id, version)
	def, ok := r.definitions[k]
	if !ok {
		return Definition{}, nil, fmt.Errorf("unknown block %s", k)
	}
	return def, r.renderers[k], nil
}
func (r *Registry) Definitions() []Definition {
	out := make([]Definition, 0, len(r.definitions))
	for _, d := range r.definitions {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return key(out[i].ID, out[i].Version) < key(out[j].ID, out[j].Version) })
	return out
}

func (r *Registry) Validate(document documents.Document) error {
	return r.validate(document, true)
}

// ValidateDraft accepts structurally safe documents while allowing empty
// user-facing required fields until a revision is saved or published.
func (r *Registry) ValidateDraft(document documents.Document) error {
	return r.validate(document, false)
}

func (r *Registry) validate(document documents.Document, requireFields bool) error {
	if document.Version != documents.Version {
		return fmt.Errorf("unsupported document version %d", document.Version)
	}
	seen := map[string]bool{}
	nodes := 0
	var visit func([]documents.Node, int, string) error
	visit = func(children []documents.Node, depth int, path string) error {
		if depth > 32 {
			return fmt.Errorf("%s: document exceeds maximum depth", path)
		}
		for i, node := range children {
			nodes++
			nodePath := fmt.Sprintf("%s.children[%d]", path, i)
			if nodes > 1000 {
				return fmt.Errorf("document exceeds maximum node count")
			}
			if node.ID == "" {
				return fmt.Errorf("%s.id: required", nodePath)
			}
			if seen[node.ID] {
				return fmt.Errorf("%s.id: duplicate node ID %q", nodePath, node.ID)
			}
			seen[node.ID] = true
			def, _, err := r.Resolve(node.Type, node.Version)
			if err != nil {
				return fmt.Errorf("%s: %w", nodePath, err)
			}
			if err := validateFields(nodePath+".props", node.Props, def.Props, requireFields); err != nil {
				return err
			}
			if err := validateFields(nodePath+".settings", node.Settings, def.Settings, requireFields); err != nil {
				return err
			}
			if len(node.Children) > 0 && !def.AllowsChildren {
				return fmt.Errorf("%s.children: block %s does not allow children", nodePath, node.Type)
			}
			if err := visit(node.Children, depth+1, nodePath); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(document.Children, 1, "document")
}
func validateFields(path string, values map[string]any, schema map[string]Field, requireFields bool) error {
	for name, field := range schema {
		value, ok := values[name]
		if field.Required && requireFields && !ok {
			return fmt.Errorf("%s.%s: required", path, name)
		}
		if ok {
			if field.Required && requireFields && (field.Type == "text" || field.Type == "textarea" || field.Type == "url") {
				if text, isText := value.(string); isText && strings.TrimSpace(text) == "" {
					return fmt.Errorf("%s.%s: required", path, name)
				}
			}
			if err := validateField(value, field); err != nil {
				return fmt.Errorf("%s.%s: %w", path, name, err)
			}
		}
	}
	for name := range values {
		if _, ok := schema[name]; !ok {
			return fmt.Errorf("%s.%s: unknown field", path, name)
		}
	}
	return nil
}
func validateField(value any, field Field) error {
	switch field.Type {
	case "text", "textarea", "url", "media":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("must be a string")
		}
		if field.Type == "url" {
			if err := validateURL(text); err != nil {
				return err
			}
		}
		if field.Type == "media" && strings.TrimSpace(text) == "" {
			return fmt.Errorf("must be a media ID")
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("must be a boolean")
		}
	case "integer":
		n, ok := value.(float64)
		if !ok || n != float64(int64(n)) {
			return fmt.Errorf("must be an integer")
		}
	case "enum":
		value, ok := value.(string)
		if !ok {
			return fmt.Errorf("must be a string")
		}
		for _, option := range field.Options {
			if value == option {
				return nil
			}
		}
		return fmt.Errorf("must be one of %s", strings.Join(field.Options, ", "))
	default:
		return fmt.Errorf("unsupported field type %q", field.Type)
	}
	return nil
}

// ValidateFieldValue validates one value against a registry field schema.
func ValidateFieldValue(value any, field Field) error { return validateField(value, field) }

func validateURL(value string) error {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("must not contain control characters")
		}
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("must be a valid URL")
	}
	if parsed.Scheme == "" {
		return nil
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "mailto", "tel":
		return nil
	default:
		return fmt.Errorf("uses unsafe URL scheme")
	}
}
