// Package id provides opaque IDs for persisted domain entities.
package id

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// New returns a URL-safe opaque random ID.
func New() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return hex.EncodeToString(bytes[:]), nil
}
