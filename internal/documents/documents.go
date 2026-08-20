// Package documents defines the Stratum Document Tree (SDT) wire model.
package documents

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

const Version = 1
const maxDocumentBytes = 1 << 20 // 1 MiB

type Document struct {
	Version  int    `json:"version"`
	Children []Node `json:"children"`
}

type Node struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Version  int            `json:"version"`
	Props    map[string]any `json:"props"`
	Settings map[string]any `json:"settings"`
	Children []Node         `json:"children"`
}

func Empty() Document { return Document{Version: Version, Children: []Node{}} }

func Parse(data []byte) (Document, error) {
	if len(data) > maxDocumentBytes {
		return Document{}, fmt.Errorf("document exceeds %d byte limit", maxDocumentBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document Document
	if err := decoder.Decode(&document); err != nil {
		return Document{}, fmt.Errorf("decode document: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return Document{}, err
	}
	if document.Version != Version {
		return Document{}, fmt.Errorf("unsupported document version %d", document.Version)
	}
	if document.Children == nil {
		document.Children = []Node{}
	}
	return document, nil
}

func Marshal(document Document) ([]byte, error) {
	if document.Version != Version {
		return nil, fmt.Errorf("unsupported document version %d", document.Version)
	}
	data, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("marshal document: %w", err)
	}
	if len(data) > maxDocumentBytes {
		return nil, fmt.Errorf("document exceeds %d byte limit", maxDocumentBytes)
	}
	return data, nil
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("document has trailing JSON values")
		}
		return fmt.Errorf("decode document: %w", err)
	}
	return nil
}
