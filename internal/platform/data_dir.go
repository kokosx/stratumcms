package platform

import (
	"fmt"
	"os"
	"path/filepath"
)

var dataDirectories = []string{
	"media",
	"themes",
	"blocks",
	"cache",
	"cache/compiled",
	"cache/pages",
	"cache/assets",
	"backups",
	"tmp",
}

// EnsureDataDir creates the application's data directory and its required children.
func EnsureDataDir(dataDir string) error {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	for _, directory := range dataDirectories {
		if err := os.MkdirAll(filepath.Join(dataDir, directory), 0o755); err != nil {
			return fmt.Errorf("create data directory %q: %w", directory, err)
		}
	}
	return nil
}
