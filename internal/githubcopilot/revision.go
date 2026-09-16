package githubcopilot

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func sourceRevision(root string, files []string) (string, error) {
	hash := sha256.New()
	_, _ = fmt.Fprintf(hash, "root:%s\n", filepath.Clean(root))
	for _, path := range files {
		info, err := os.Stat(path)
		if err != nil {
			return "", fmt.Errorf("stat GitHub Copilot source file %q: %w", path, err)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return "", fmt.Errorf("relative GitHub Copilot source file %q: %w", path, err)
		}
		_, _ = fmt.Fprintf(hash, "%s:%d:%d\n", filepath.ToSlash(relative), info.Size(), info.ModTime().UnixNano())
		metadataPath := workspaceMetadataPath(path)
		metadataRelative, err := filepath.Rel(root, metadataPath)
		if err != nil {
			return "", fmt.Errorf("relative GitHub Copilot metadata file %q: %w", metadataPath, err)
		}
		metadataInfo, metadataErr := os.Stat(metadataPath)
		switch {
		case metadataErr == nil:
			_, _ = fmt.Fprintf(hash, "metadata:%s:%d:%d\n", filepath.ToSlash(metadataRelative), metadataInfo.Size(), metadataInfo.ModTime().UnixNano())
		case errors.Is(metadataErr, os.ErrNotExist):
			_, _ = fmt.Fprintf(hash, "metadata:%s:missing\n", filepath.ToSlash(metadataRelative))
		default:
			_, _ = fmt.Fprintf(hash, "metadata:%s:unreadable\n", filepath.ToSlash(metadataRelative))
		}
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func sameSourceFiles(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if strings.TrimSpace(left[i]) != strings.TrimSpace(right[i]) {
			return false
		}
	}
	return true
}
