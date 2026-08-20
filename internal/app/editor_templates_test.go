package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kokosx/stratumcms/internal/blocks"
	"github.com/kokosx/stratumcms/internal/documents"
	"github.com/kokosx/stratumcms/internal/editor"
)

func TestEditorWorkspaceUsesCurrentInspectorValues(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	data := editorData{
		CSRFToken: "csrf",
		Draft: editor.Draft{
			EntryID:  "entry-1",
			Version:  3,
			Document: documents.Document{Version: documents.Version},
		},
		Inspector: []editorInspector{{
			NodeID: "heading-1",
			Definition: blocks.Definition{
				Name:  "Heading",
				Props: map[string]blocks.Field{"text": {Type: "text"}},
			},
			Props: map[string]any{"text": "Welcome"},
		}},
	}

	var output bytes.Buffer
	if err := templates.ExecuteTemplate(&output, "editor_workspace", data); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.Contains(got, `@post('/admin/editor/entry-1/blocks/heading-1')`) {
		t.Fatalf("inspector Datastar request did not use its node ID: %s", got)
	}
	if !strings.Contains(got, `value="Welcome"`) {
		t.Fatalf("inspector field did not use its value: %s", got)
	}
}
