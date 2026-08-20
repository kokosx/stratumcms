package documents

import (
	"reflect"
	"testing"
)

func TestMoveRejectsOwnSubtree(t *testing.T) {
	doc := Document{Version: Version, Children: []Node{{ID: "parent", Children: []Node{{ID: "child"}}}}}
	if err := MoveNode(&doc, "parent", "child", 0); err == nil {
		t.Fatal("cycle was accepted")
	}
}

func TestMoveFailureLeavesDocumentUnchanged(t *testing.T) {
	doc := Document{Version: Version, Children: []Node{{ID: "first"}, {ID: "second"}}}
	want := cloneDocument(doc)
	if err := MoveNode(&doc, "first", "missing", 0); err == nil {
		t.Fatal("move unexpectedly succeeded")
	}
	if !reflect.DeepEqual(doc, want) {
		t.Fatalf("document changed after failed move: %#v", doc)
	}
}

func TestDuplicateGeneratesIDsForWholeTree(t *testing.T) {
	doc := Document{Version: Version, Children: []Node{{ID: "parent", Children: []Node{{ID: "child"}}}}}
	next := 0
	_, err := DuplicateNode(&doc, "parent", func() (string, error) {
		next++
		return string(rune('a' + next)), nil
	})
	if err != nil || len(doc.Children) != 2 || doc.Children[1].Children[0].ID == "child" {
		t.Fatalf("doc=%#v err=%v", doc, err)
	}
}
