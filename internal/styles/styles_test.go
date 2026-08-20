package styles

import (
	"strings"
	"testing"
)

func TestValidateRejectsUnsafeTokens(t *testing.T) {
	bad := Defaults()
	bad.Brand = "red;body{display:none}"
	if Validate(bad) == nil {
		t.Fatal("unsafe color accepted")
	}
	bad = Defaults()
	bad.Body = "url(https://evil.test)"
	if Validate(bad) == nil {
		t.Fatal("unsafe font accepted")
	}
}
func TestCSSUsesSemanticVariablesBeforeCustomCSS(t *testing.T) {
	s := Settings{Tokens: Defaults(), CustomCSS: "body{outline:0}"}
	css := CSS(s)
	if !strings.Contains(css, "--stratum-color-brand:#2563eb") || strings.Index(css, "--stratum-color-brand") > strings.Index(css, "body{outline:0}") {
		t.Fatalf("unexpected css: %s", css)
	}
	if !strings.Contains(css, "Arial, Helvetica, sans-serif") {
		t.Fatal("font stack missing")
	}
}
