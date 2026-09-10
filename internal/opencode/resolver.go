package opencode

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ResolveOptions makes data-root resolution independent of the host running
// the tests.
type ResolveOptions struct {
	Explicit    string
	EnvHome     string
	UserHome    string
	XDGDataHome string
}

// ResolveHome applies OpenCode's data-root precedence rule.
func ResolveHome(explicit string) (string, error) {
	envHome := os.Getenv("OPENCODE_HOME")
	dataHome := os.Getenv("XDG_DATA_HOME")
	userHome, err := os.UserHomeDir()
	if err != nil && strings.TrimSpace(explicit) == "" && strings.TrimSpace(envHome) == "" && strings.TrimSpace(dataHome) == "" {
		return "", err
	}
	return ResolveHomeFrom(ResolveOptions{
		Explicit:    explicit,
		EnvHome:     envHome,
		UserHome:    userHome,
		XDGDataHome: dataHome,
	})
}

// ResolveHomeFrom is the injectable form of ResolveHome.
func ResolveHomeFrom(options ResolveOptions) (string, error) {
	for _, candidate := range []string{options.Explicit, options.EnvHome} {
		if candidate = strings.TrimSpace(candidate); candidate != "" {
			return filepath.Clean(candidate), nil
		}
	}
	userHome := strings.TrimSpace(options.UserHome)
	base := strings.TrimSpace(options.XDGDataHome)
	if base == "" {
		if userHome == "" {
			return "", errors.New("cannot resolve user home")
		}
		base = filepath.Join(userHome, ".local", "share")
	}
	return filepath.Join(base, "opencode"), nil
}
