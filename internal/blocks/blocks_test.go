package blocks

import (
	"github.com/kokosx/stratumcms/internal/documents"
	"strings"
	"testing"
)

func textNode(id string, props map[string]any) documents.Node {
	return documents.Node{ID: id, Type: "core.text", Version: 1, Props: props, Settings: map[string]any{}, Children: []documents.Node{}}
}
func TestValidation(t *testing.T) {
	r := CoreRegistry()
	cases := []struct {
		name string
		doc  documents.Document
		want string
	}{
		{"duplicate", documents.Document{Version: 1, Children: []documents.Node{textNode("a", map[string]any{"text": "x"}), textNode("a", map[string]any{"text": "x"})}}, "duplicate"},
		{"unknown", documents.Document{Version: 1, Children: []documents.Node{{ID: "a", Type: "core.nope", Version: 1, Props: map[string]any{}, Settings: map[string]any{}}}}, "unknown block"},
		{"version", documents.Document{Version: 1, Children: []documents.Node{{ID: "a", Type: "core.text", Version: 2, Props: map[string]any{}, Settings: map[string]any{}}}}, "unknown block"},
		{"required", documents.Document{Version: 1, Children: []documents.Node{textNode("a", map[string]any{})}}, "required"},
		{"enum", documents.Document{Version: 1, Children: []documents.Node{{ID: "a", Type: "core.hero", Version: 1, Props: map[string]any{"heading": "x"}, Settings: map[string]any{"variant": "bad"}}}}, "must be one of"},
		{"children", documents.Document{Version: 1, Children: []documents.Node{{ID: "a", Type: "core.text", Version: 1, Props: map[string]any{"text": "x"}, Settings: map[string]any{}, Children: []documents.Node{textNode("b", map[string]any{"text": "y"})}}}}, "does not allow children"},
		{"unsafe url", documents.Document{Version: 1, Children: []documents.Node{{ID: "a", Type: "core.button", Version: 1, Props: map[string]any{"label": "Go", "url": "javascript:alert(1)"}, Settings: map[string]any{}}}}, "unsafe URL scheme"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := r.Validate(tc.doc); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
