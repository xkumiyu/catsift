package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreSeparatesSourceNamespacesAndValidatesEnvelope(t *testing.T) {
	store := New(t.TempDir())
	snapshot := map[string]any{"turns": []any{map[string]any{"id": "turn-1"}}}
	if err := store.Write("codex", "/history/shared", "revision-1", "parser-1", snapshot); err != nil {
		t.Fatal(err)
	}
	if err := store.Write("ctx", "/history/shared", "revision-1", "parser-1", snapshot); err != nil {
		t.Fatal(err)
	}
	if store.Path("codex", "/history/shared") == store.Path("ctx", "/history/shared") {
		t.Fatal("source namespaces must not collide")
	}

	for _, test := range []struct {
		name     string
		source   string
		scope    string
		revision string
		parser   string
		wantHit  bool
	}{
		{name: "match", source: "codex", scope: "/history/shared", revision: "revision-1", parser: "parser-1", wantHit: true},
		{name: "source mismatch", source: "ctx", scope: "/history/shared", revision: "revision-1", parser: "parser-1", wantHit: true},
		{name: "scope mismatch", source: "codex", scope: "/history/other", revision: "revision-1", parser: "parser-1"},
		{name: "revision mismatch", source: "codex", scope: "/history/shared", revision: "revision-2", parser: "parser-1"},
		{name: "parser mismatch", source: "codex", scope: "/history/shared", revision: "revision-1", parser: "parser-2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, hit, err := store.Read(test.source, test.scope, test.revision, test.parser)
			if err != nil {
				t.Fatal(err)
			}
			if hit != test.wantHit {
				t.Fatalf("cache hit = %v, want %v", hit, test.wantHit)
			}
		})
	}
	entry, hit, err := store.ReadEntry("codex", "/history/shared", "parser-1")
	if err != nil {
		t.Fatal(err)
	}
	if !hit || entry.Revision != "revision-1" || entry.StoredAt.IsZero() {
		t.Fatalf("cache entry = %#v, hit=%v", entry, hit)
	}
}

func TestDefaultDirUsesNewSnapshotCacheNamespace(t *testing.T) {
	directory, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if got := filepath.Base(directory); got != "v2" {
		t.Fatalf("cache namespace = %q, want v2", got)
	}
}

func TestStoreIgnoresCorruptAndIncompleteFiles(t *testing.T) {
	store := New(t.TempDir())
	scope := "/history/shared"
	path := store.Path("codex", scope)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, hit, err := store.Read("codex", scope, "revision-1", "parser-1"); err != nil || hit {
		t.Fatalf("corrupt cache = hit %v, err %v", hit, err)
	}

	incomplete := Envelope{
		SchemaVersion: SchemaVersion,
		Source:        "codex",
		Scope:         scope,
		Revision:      "revision-1",
		ParserVersion: "parser-1",
		Complete:      false,
		Snapshot:      json.RawMessage(`{"turns":[]}`),
	}
	data, err := json.Marshal(incomplete)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, hit, err := store.Read("codex", scope, "revision-1", "parser-1"); err != nil || hit {
		t.Fatalf("incomplete cache = hit %v, err %v", hit, err)
	}
	incomplete.Complete = true
	incomplete.SchemaVersion++
	data, err = json.Marshal(incomplete)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, hit, err := store.Read("codex", scope, "revision-1", "parser-1"); err != nil || hit {
		t.Fatalf("schema mismatch cache = hit %v, err %v", hit, err)
	}
}

func TestStoreAtomicWritePreservesPreviousValueOnFailedMarshal(t *testing.T) {
	store := New(t.TempDir())
	scope := "/history/shared"
	if err := store.Write("codex", scope, "revision-1", "parser-1", map[string]string{"value": "old"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Write("codex", scope, "revision-2", "parser-1", func() {}); err == nil {
		t.Fatal("unsupported snapshot should fail before replacing the old cache")
	}
	raw, hit, err := store.Read("codex", scope, "revision-1", "parser-1")
	if err != nil {
		t.Fatal(err)
	}
	if !hit || !strings.Contains(string(raw), `"value":"old"`) {
		t.Fatalf("previous cache was not preserved: hit=%v raw=%s", hit, raw)
	}

	info, err := os.Stat(filepath.Dir(store.Path("codex", scope)))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("cache directory mode = %o, want 700", info.Mode().Perm())
	}
	rootInfo, err := os.Stat(store.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if rootInfo.Mode().Perm() != 0o700 {
		t.Fatalf("cache root mode = %o, want 700", rootInfo.Mode().Perm())
	}
	fileInfo, err := os.Stat(store.Path("codex", scope))
	if err != nil {
		t.Fatal(err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("cache file mode = %o, want 600", fileInfo.Mode().Perm())
	}
}
