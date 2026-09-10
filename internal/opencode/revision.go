package opencode

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// sourceRevision changes when the database or either SQLite sidecar changes.
// Sidecars matter because WAL mode can advance history without changing the
// main database file's size or modification time.
func sourceRevision(database string) (string, error) {
	database = filepath.Clean(database)
	parts := make([]string, 1, 4)
	parts[0] = "database:" + filepath.Base(database)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		path := database + suffix
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			parts = append(parts, suffix+":missing")
			continue
		}
		if err != nil {
			return "", fmt.Errorf("stat OpenCode source file %q: %w", path, err)
		}
		parts = append(parts, fmt.Sprintf("%s:%d:%d", suffix, info.Size(), info.ModTime().UnixNano()))
	}
	return strings.Join(parts, "|"), nil
}
