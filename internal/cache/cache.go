// Package cache contains the best-effort persistent cache used by the source
// adapters. Cache contents are disposable and must never be required for a
// report to succeed.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	SchemaVersion = 1
	cacheDirName  = "catsift"
	cacheVersion  = "v1"
)

// Envelope wraps an opaque normalized snapshot with the information required
// to decide whether it can be reused.
type Envelope struct {
	SchemaVersion int             `json:"schema_version"`
	Source        string          `json:"source"`
	Scope         string          `json:"scope"`
	Revision      string          `json:"revision"`
	ParserVersion string          `json:"parser_version"`
	Complete      bool            `json:"complete"`
	Snapshot      json.RawMessage `json:"snapshot"`
}

// Store addresses one versioned CatSift cache directory.
type Store struct {
	Dir string
}

// New creates a cache store rooted at dir. The directory is created lazily on
// the first successful write.
func New(dir string) Store {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return Store{}
	}
	return Store{Dir: filepath.Clean(dir)}
}

// DefaultDir resolves the platform cache directory without creating it.
func DefaultDir() (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(root) == "" {
		return "", errors.New("user cache directory is empty")
	}
	return filepath.Join(root, cacheDirName, cacheVersion), nil
}

// Path returns the deterministic path for a source and canonical scope.
// Source namespaces are separate so equal scopes from different adapters can
// never overwrite one another.
func (s Store) Path(source, scope string) string {
	if s.Dir == "" {
		return ""
	}
	namespace := normalizeNamespace(source)
	hash := sha256.Sum256([]byte(scope))
	return filepath.Join(s.Dir, namespace, hex.EncodeToString(hash[:])+".json")
}

// Read returns a snapshot payload and whether a valid matching cache exists.
// Invalid, incomplete, or mismatched files are misses. Filesystem errors are
// returned so callers may optionally record diagnostics, but callers should
// continue with source ingestion.
func (s Store) Read(source, scope, revision, parserVersion string) (json.RawMessage, bool, error) {
	path := s.Path(source, scope)
	if path == "" {
		return nil, false, errors.New("cache directory is empty")
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var envelope Envelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, false, nil
	}
	if envelope.SchemaVersion != SchemaVersion ||
		envelope.Source != source ||
		envelope.Scope != scope ||
		envelope.Revision != revision ||
		envelope.ParserVersion != parserVersion ||
		!envelope.Complete ||
		len(envelope.Snapshot) == 0 || string(envelope.Snapshot) == "null" {
		return nil, false, nil
	}
	return append(json.RawMessage(nil), envelope.Snapshot...), true, nil
}

// Write serializes and atomically publishes a complete snapshot. A failed
// serialization or write leaves an already-published cache untouched.
func (s Store) Write(source, scope, revision, parserVersion string, snapshot any) error {
	if s.Dir == "" || strings.TrimSpace(source) == "" || strings.TrimSpace(scope) == "" || strings.TrimSpace(revision) == "" || strings.TrimSpace(parserVersion) == "" {
		return errors.New("cache source, scope, revision, and parser version are required")
	}
	snapshotData, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal cache snapshot: %w", err)
	}
	envelopeData, err := json.Marshal(Envelope{
		SchemaVersion: SchemaVersion,
		Source:        source,
		Scope:         scope,
		Revision:      revision,
		ParserVersion: parserVersion,
		Complete:      true,
		Snapshot:      snapshotData,
	})
	if err != nil {
		return fmt.Errorf("marshal cache envelope: %w", err)
	}

	target := s.Path(source, scope)
	directory := filepath.Dir(target)
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return fmt.Errorf("create cache root: %w", err)
	}
	if err := os.Chmod(s.Dir, 0o700); err != nil {
		return fmt.Errorf("secure cache root: %w", err)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("secure cache directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".catsift-cache-*")
	if err != nil {
		return fmt.Errorf("create cache temporary file: %w", err)
	}
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("secure cache temporary file: %w", err)
	}
	if _, err := temporary.Write(envelopeData); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write cache temporary file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync cache temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close cache temporary file: %w", err)
	}
	if err := os.Rename(temporaryName, target); err != nil {
		return fmt.Errorf("publish cache file: %w", err)
	}
	return syncDirectory(directory)
}

func normalizeNamespace(source string) string {
	source = strings.ToLower(strings.TrimSpace(source))
	if source == "" {
		return "unknown"
	}
	var builder strings.Builder
	for _, r := range source {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			builder.WriteRune(r)
		}
	}
	if builder.Len() == 0 {
		return "unknown"
	}
	return builder.String()
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = directory.Close() }()
	if err := directory.Sync(); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		return err
	}
	return nil
}
