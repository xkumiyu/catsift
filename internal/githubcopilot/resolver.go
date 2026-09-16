package githubcopilot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ResolveHome applies GitHub Copilot CLI's local data-root convention.
func ResolveHome(explicit string) (string, error) {
	userHome, err := os.UserHomeDir()
	if err != nil && strings.TrimSpace(explicit) == "" {
		return "", err
	}
	return ResolveHomeFrom(explicit, userHome)
}

// ResolveHomeFrom is the injectable form used by tests and callers with a
// pre-resolved user home.
func ResolveHomeFrom(explicit, userHome string) (string, error) {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		return filepath.Clean(explicit), nil
	}
	if userHome = strings.TrimSpace(userHome); userHome == "" {
		return "", errors.New("cannot resolve user home")
	}
	return filepath.Join(userHome, ".copilot"), nil
}

// Discover returns session event files in stable path order. Only regular
// events.jsonl files directly below session-state session directories qualify.
func Discover(root string) ([]string, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "." || root == "" {
		return nil, errors.New("GitHub Copilot data root is empty")
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("GitHub Copilot data root %q: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("GitHub Copilot data root %q is not a directory", root)
	}
	if info.Mode().Perm()&0444 == 0 {
		return nil, fmt.Errorf("GitHub Copilot data root %q: permission denied", root)
	}

	sessionRoot := filepath.Join(root, "session-state")
	info, err = os.Stat(sessionRoot)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("discover GitHub Copilot sessions %q: %w", sessionRoot, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("GitHub Copilot session root %q is not a directory", sessionRoot)
	}
	entries, err := os.ReadDir(sessionRoot)
	if err != nil {
		return nil, fmt.Errorf("read GitHub Copilot session root %q: %w", sessionRoot, err)
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.TrimSpace(entry.Name()) == "" {
			continue
		}
		path := filepath.Join(sessionRoot, entry.Name(), "events.jsonl")
		fileInfo, statErr := os.Lstat(path)
		if errors.Is(statErr, os.ErrNotExist) || statErr != nil || !fileInfo.Mode().IsRegular() {
			continue
		}
		files = append(files, path)
	}
	sort.Strings(files)
	return files, nil
}

// HasHistory reports whether root contains at least one Copilot session event file.
func HasHistory(root string) (bool, error) {
	files, err := Discover(root)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return len(files) > 0, nil
}

func sessionIDFromPath(path string) string {
	return filepath.Base(filepath.Dir(path))
}
