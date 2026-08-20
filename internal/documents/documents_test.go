package documents

import "testing"

func TestParseMarshalRoundTrip(t *testing.T) {
	in := []byte(`{"version":1,"children":[{"id":"one","type":"core.text","version":1,"props":{"text":"Hello"},"settings":{},"children":[]}]}`)
	document, err := Parse(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(out); err != nil {
		t.Fatalf("round trip parse: %v", err)
	}
}
func TestParseRejectsMalformedAndUnknownTopLevel(t *testing.T) {
	for _, data := range [][]byte{[]byte(`{`), []byte(`{"version":1,"children":[],"extra":true}`), []byte(`{"version":2,"children":[]}`)} {
		if _, err := Parse(data); err == nil {
			t.Fatalf("Parse(%s) succeeded", data)
		}
	}
}
