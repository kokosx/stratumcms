package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPagesPutGetAndPersistence(t *testing.T) {
	dir := t.TempDir()
	p, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	entry := Entry{Path: "/about", HTML: []byte("<p>about</p>"), ETag: "\"one\"", Dependencies: []string{"entry:a", "presentation"}}
	if err := p.Put(entry); err != nil {
		t.Fatal(err)
	}
	got, ok := p.Get("/about")
	if !ok || string(got.HTML) != string(entry.HTML) || got.ETag != entry.ETag {
		t.Fatalf("got=%#v ok=%v", got, ok)
	}
	p, err = New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Get("/about"); !ok {
		t.Fatal("cache did not survive new instance")
	}
}
func TestPagesInvalidationAndCorruption(t *testing.T) {
	p, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []Entry{{Path: "/a", HTML: []byte("a"), ETag: "\"a\"", Dependencies: []string{"entry:a"}}, {Path: "/b", HTML: []byte("b"), ETag: "\"b\"", Dependencies: []string{"entry:b"}}} {
		if err := p.Put(entry); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.InvalidateTag("entry:a"); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Get("/a"); ok {
		t.Fatal("matching tag was not invalidated")
	}
	if _, ok := p.Get("/b"); !ok {
		t.Fatal("non-matching tag was invalidated")
	}
	_, _, meta, err := p.names("/b")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(meta, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Get("/b"); ok {
		t.Fatal("corrupt metadata was a hit")
	}
	if _, err := os.Stat(filepath.Dir(meta)); err != nil {
		t.Fatal(err)
	}
}
func TestPagesInvalidatePathAndClear(t *testing.T) {
	p, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/a", "/b"} {
		if err := p.Put(Entry{Path: path, HTML: []byte(path), ETag: "\"x\""}); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.InvalidatePath("/a"); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Get("/a"); ok {
		t.Fatal("path was not invalidated")
	}
	if err := p.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Get("/b"); ok {
		t.Fatal("cache was not cleared")
	}
}
