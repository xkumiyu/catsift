package githubcopilot

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestResolveHomePrefersExplicitRoot(t *testing.T) {
	if got, err := ResolveHomeFrom("/tmp/copilot-fixture", "/home/example"); err != nil || got != filepath.Clean("/tmp/copilot-fixture") {
		t.Fatalf("resolved root = %q, err=%v", got, err)
	}
}

func TestResolveHomeUsesUserHome(t *testing.T) {
	got, err := ResolveHomeFrom("", "/home/example")
	if err != nil || got != filepath.Join("/home/example", ".copilot") {
		t.Fatalf("resolved root = %q, err=%v", got, err)
	}
}

func TestResolveHomeRequiresUserHome(t *testing.T) {
	if _, err := ResolveHomeFrom("", ""); err == nil {
		t.Fatal("expected missing user home error")
	}
}

func TestDiscoverFindsOnlySessionEvents(t *testing.T) {
	root := t.TempDir()
	paths := []string{
		filepath.Join(root, "session-state", "session-b", "events.jsonl"),
		filepath.Join(root, "session-state", "session-a", "events.jsonl"),
		filepath.Join(root, "logs", "events.jsonl"),
		filepath.Join(root, "session-store.db"),
		filepath.Join(root, "session-state", "session-a", "checkpoint.json"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{}\n"), 0o640); err != nil {
			t.Fatal(err)
		}
	}

	got, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(root, "session-state", "session-a", "events.jsonl"),
		filepath.Join(root, "session-state", "session-b", "events.jsonl"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discovered files = %#v, want %#v", got, want)
	}
}

func TestDiscoverReturnsEmptyForDiagnosticOnlyRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "session-store.db"), []byte("fixture"), 0o640); err != nil {
		t.Fatal(err)
	}
	got, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("diagnostic-only root discovered files = %#v", got)
	}
}

func TestHasHistoryReportsOnlyRootsWithSessionEvents(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	available, err := HasHistory(missing)
	if err != nil || available {
		t.Fatalf("missing root availability = %t, %v", available, err)
	}

	root := t.TempDir()
	available, err = HasHistory(root)
	if err != nil || available {
		t.Fatalf("empty root availability = %t, %v", available, err)
	}

	path := filepath.Join(root, "session-state", "session-a", "events.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	available, err = HasHistory(root)
	if err != nil || !available {
		t.Fatalf("history root availability = %t, %v", available, err)
	}
}

func TestDiscoverRejectsMissingOrNonDirectoryRoot(t *testing.T) {
	if _, err := Discover(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected missing root error")
	}
	file := filepath.Join(t.TempDir(), "copilot")
	if err := os.WriteFile(file, []byte("fixture"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(file); err == nil {
		t.Fatal("expected non-directory root error")
	}
}
