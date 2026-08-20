package themes

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

func TestStarterRendersPageAndPost(t *testing.T) {
	r, err := NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	theme, ok := r.Resolve("starter")
	if !ok {
		t.Fatal("starter missing")
	}
	for _, kind := range []string{"page", "post"} {
		var b bytes.Buffer
		err := theme.Execute(&b, kind, struct {
			Title                   string
			Content                 template.HTML
			SiteStyles, ThemeStyles string
		}{"Title", template.HTML("<p>Body</p>"), "/site.css", "/theme.css"})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(b.String(), "/theme.css") || !strings.Contains(b.String(), "/site.css") {
			t.Fatalf("assets missing: %s", b.String())
		}
		if kind == "post" && !strings.Contains(b.String(), "Post</p>") {
			t.Fatal("post template not used")
		}
	}
}
