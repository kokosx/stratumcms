package renderer

import (
	"github.com/kokosx/stratumcms/internal/blocks"
	"github.com/kokosx/stratumcms/internal/documents"
	"strings"
	"testing"
)

func TestRenderNestedAndEscapesProps(t *testing.T) {
	doc := documents.Document{Version: 1, Children: []documents.Node{{ID: "container", Type: "core.container", Version: 1, Props: map[string]any{}, Settings: map[string]any{}, Children: []documents.Node{{ID: "text", Type: "core.text", Version: 1, Props: map[string]any{"text": "<script>alert(1)</script>"}, Settings: map[string]any{}}}}}}
	html, err := New(blocks.CoreRegistry()).Render(doc)
	if err != nil {
		t.Fatal(err)
	}
	got := string(html)
	if !strings.Contains(got, `data-node-id="container"`) || !strings.Contains(got, "&lt;script&gt;") || strings.Contains(got, "<script>") {
		t.Fatalf("unsafe or incomplete render: %s", got)
	}
}

func TestHeadingUsesDefaultLevel(t *testing.T) {
	doc := documents.Document{Version: 1, Children: []documents.Node{{ID: "heading", Type: "core.heading", Version: 1, Props: map[string]any{"text": "Title"}, Settings: map[string]any{}}}}
	html, err := New(blocks.CoreRegistry()).Render(doc)
	if err != nil || !strings.Contains(string(html), "<h2 ") {
		t.Fatalf("heading=%s err=%v", html, err)
	}
}
