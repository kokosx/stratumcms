package documents

import "fmt"

// FindNode returns the node identified by id and its parent, if any.
func FindNode(document Document, id string) (*Node, *Node) {
	var walk func([]Node, *Node) (*Node, *Node)
	walk = func(nodes []Node, parent *Node) (*Node, *Node) {
		for i := range nodes {
			if nodes[i].ID == id {
				return &nodes[i], parent
			}
			if found, foundParent := walk(nodes[i].Children, &nodes[i]); found != nil {
				return found, foundParent
			}
		}
		return nil, nil
	}
	return walk(document.Children, nil)
}

func InsertNode(document *Document, parentID string, node Node) error {
	if node.ID == "" || Find(document.Children, node.ID) != nil {
		return fmt.Errorf("node ID must be unique")
	}
	if parentID == "" {
		document.Children = append(document.Children, node)
		return nil
	}
	parent := Find(document.Children, parentID)
	if parent == nil {
		return fmt.Errorf("parent node not found")
	}
	parent.Children = append(parent.Children, node)
	return nil
}

func UpdateNode(document *Document, id string, update func(*Node) error) error {
	node := Find(document.Children, id)
	if node == nil {
		return fmt.Errorf("node not found")
	}
	return update(node)
}

func DeleteNode(document *Document, id string) error {
	return remove(document, id, nil)
}

func DuplicateNode(document *Document, id string, newID func() (string, error)) (string, error) {
	node := Find(document.Children, id)
	if node == nil {
		return "", fmt.Errorf("node not found")
	}
	clone, err := cloneWithIDs(*node, newID)
	if err != nil {
		return "", err
	}
	if err := insertAfter(document, id, clone); err != nil {
		return "", err
	}
	return clone.ID, nil
}

// MoveNode moves id to parentID (or the root for an empty parentID) at index.
func MoveNode(document *Document, id, parentID string, index int) error {
	if id == parentID || (parentID != "" && contains(Find(document.Children, id), parentID)) {
		return fmt.Errorf("cannot move a node into its own subtree")
	}
	var node Node
	if err := remove(document, id, &node); err != nil {
		return err
	}
	var siblings *[]Node = &document.Children
	if parentID != "" {
		parent := Find(document.Children, parentID)
		if parent == nil {
			return fmt.Errorf("parent node not found")
		}
		siblings = &parent.Children
	}
	if index < 0 || index > len(*siblings) {
		return fmt.Errorf("invalid target position")
	}
	*siblings = append(*siblings, Node{})
	copy((*siblings)[index+1:], (*siblings)[index:])
	(*siblings)[index] = node
	return nil
}

func Find(nodes []Node, id string) *Node {
	for i := range nodes {
		if nodes[i].ID == id {
			return &nodes[i]
		}
		if found := Find(nodes[i].Children, id); found != nil {
			return found
		}
	}
	return nil
}
func remove(document *Document, id string, result *Node) error {
	var walk func(*[]Node) bool
	walk = func(nodes *[]Node) bool {
		for i := range *nodes {
			if (*nodes)[i].ID == id {
				if result != nil {
					*result = (*nodes)[i]
				}
				*nodes = append((*nodes)[:i], (*nodes)[i+1:]...)
				return true
			}
			if walk(&(*nodes)[i].Children) {
				return true
			}
		}
		return false
	}
	if !walk(&document.Children) {
		return fmt.Errorf("node not found")
	}
	return nil
}
func insertAfter(document *Document, id string, node Node) error {
	var walk func(*[]Node) bool
	walk = func(nodes *[]Node) bool {
		for i := range *nodes {
			if (*nodes)[i].ID == id {
				*nodes = append((*nodes)[:i+1], append([]Node{node}, (*nodes)[i+1:]...)...)
				return true
			}
			if walk(&(*nodes)[i].Children) {
				return true
			}
		}
		return false
	}
	if !walk(&document.Children) {
		return fmt.Errorf("node not found")
	}
	return nil
}
func cloneWithIDs(node Node, newID func() (string, error)) (Node, error) {
	id, err := newID()
	if err != nil {
		return Node{}, err
	}
	node.ID = id
	node.Props = copyMap(node.Props)
	node.Settings = copyMap(node.Settings)
	children := make([]Node, len(node.Children))
	for i := range node.Children {
		children[i], err = cloneWithIDs(node.Children[i], newID)
		if err != nil {
			return Node{}, err
		}
	}
	node.Children = children
	return node, nil
}
func copyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
func contains(node *Node, id string) bool {
	if node == nil {
		return false
	}
	if node.ID == id {
		return true
	}
	for i := range node.Children {
		if contains(&node.Children[i], id) {
			return true
		}
	}
	return false
}
