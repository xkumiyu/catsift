package opencode

import (
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xkumiyu/catsift/internal/aggregate"
	"github.com/xkumiyu/catsift/internal/cache"
	"github.com/xkumiyu/catsift/internal/usage"
	_ "modernc.org/sqlite"
)

func TestOpenCodeRevisionIncludesDatabaseWALAndSHM(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(root, "opencode.db")
	if err := os.WriteFile(database, []byte("db"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := sourceRevision(database)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(database+"-wal", []byte("wal"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := sourceRevision(database)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("WAL did not change revision: %q", first)
	}
	if err := os.WriteFile(database+"-shm", []byte("shm"), 0o600); err != nil {
		t.Fatal(err)
	}
	third, err := sourceRevision(database)
	if err != nil {
		t.Fatal(err)
	}
	if second == third {
		t.Fatalf("SHM did not change revision: %q", second)
	}
}

func TestOpenCodeRevisionIncludesDatabaseName(t *testing.T) {
	root := t.TempDir()
	firstPath := filepath.Join(root, "opencode.db")
	secondPath := filepath.Join(root, "opencode-local.db")
	if err := os.WriteFile(firstPath, []byte("db"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondPath, []byte("db"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := sourceRevision(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := sourceRevision(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("database names share revision: %q", first)
	}
}

func TestLoadCachesOpenCodeSnapshotAndFiltersCachedObservations(t *testing.T) {
	root := t.TempDir()
	writeNormalizerFixture(t, root)
	now := time.UnixMilli(1_700_000_020_000).UTC()
	cacheDir := t.TempDir()

	fresh, err := Load(root, IngestOptions{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	cold, err := Load(root, IngestOptions{Now: now, CacheDir: cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	warm, err := Load(root, IngestOptions{Now: now, CacheDir: cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(openCodeReport(fresh), openCodeReport(cold)) || !reflect.DeepEqual(openCodeReport(cold), openCodeReport(warm)) {
		t.Fatalf("fresh/cold/warm reports differ: fresh=%#v cold=%#v warm=%#v", openCodeReport(fresh), openCodeReport(cold), openCodeReport(warm))
	}
	if len(warm.Agents) != 1 || warm.Agents[0] != "opencode" {
		t.Fatalf("cached agents = %#v", warm.Agents)
	}

	cachePath := cache.New(cacheDir).Path("opencode", root)
	serialized, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"$review inspect this", "cat /workspace/.agents/skills/review/SKILL.md", `"arguments"`} {
		if strings.Contains(string(serialized), forbidden) {
			t.Fatalf("cache contains raw history %q: %s", forbidden, serialized)
		}
	}
	revision, err := sourceRevision(filepath.Join(root, "opencode.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, hit, err := cache.New(cacheDir).Read("opencode", root, revision, ParserVersion); err != nil || !hit {
		t.Fatalf("OpenCode cache hit = %v, err = %v", hit, err)
	}
	if _, hit, err := cache.New(cacheDir).Read("opencode", root, revision, "opencode-normalizer-old"); err != nil || hit {
		t.Fatalf("old parser cache hit = %v, err = %v", hit, err)
	}

	from := time.UnixMilli(1_700_000_008_000).UTC()
	to := from.Add(2 * time.Second)
	freshFiltered, err := Load(root, IngestOptions{From: from, To: to, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	warmFiltered, err := Load(root, IngestOptions{From: from, To: to, Now: now, CacheDir: cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(openCodeReport(freshFiltered), openCodeReport(warmFiltered)) {
		t.Fatalf("fresh/cached filtered reports differ: fresh=%#v cached=%#v", openCodeReport(freshFiltered), openCodeReport(warmFiltered))
	}
}

func TestLoadPreservesMultipleDatabaseWarningOnCacheHit(t *testing.T) {
	root := t.TempDir()
	oldPath := filepath.Join(root, "opencode-old.db")
	newPath := filepath.Join(root, "opencode-new.db")
	writeMinimalOpenCodeDatabase(t, oldPath, "s-old")
	writeMinimalOpenCodeDatabase(t, newPath, "s-new")
	base := time.Unix(1_700_000_000, 0).UTC()
	if err := os.Chtimes(oldPath, base, base); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newPath, base.Add(time.Hour), base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	cacheDir := t.TempDir()

	cold, err := Load(root, IngestOptions{CacheDir: cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	warm, err := Load(root, IngestOptions{CacheDir: cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(cold.Sessions) != 1 || cold.Sessions[0].ID != "s-new" {
		t.Fatalf("cold sessions = %#v, want s-new", cold.Sessions)
	}
	if len(warm.Sessions) != 1 || warm.Sessions[0].ID != "s-new" {
		t.Fatalf("warm sessions = %#v, want s-new", warm.Sessions)
	}
	if got := countWarnings(cold.Warnings, MultipleDatabasesWarningReason); got != 1 {
		t.Fatalf("cold multiple databases warnings = %d, want 1 (%#v)", got, cold.Warnings)
	}
	if got := countWarnings(warm.Warnings, MultipleDatabasesWarningReason); got != 1 {
		t.Fatalf("warm multiple databases warnings = %d, want 1 (%#v)", got, warm.Warnings)
	}
}

func countWarnings(warnings []usage.Warning, reason string) int {
	count := 0
	for _, warning := range warnings {
		if warning.Reason == reason {
			count++
		}
	}
	return count
}

func TestOpenCodeDiagnosticsIncludeSourceAndCacheMetadata(t *testing.T) {
	root := t.TempDir()
	writeNormalizerFixture(t, root)
	cacheDir := t.TempDir()
	var diagnostics []string
	if _, err := Load(root, IngestOptions{
		CacheDir:   cacheDir,
		Diagnostic: func(message string) { diagnostics = append(diagnostics, message) },
	}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"opencode source: root=",
		"database=",
		"opencode cache: lookup path=",
		"parser=" + ParserVersion,
		"revision=",
		"opencode cache stored complete snapshot path=",
	} {
		if !containsDiagnostic(diagnostics, want) {
			t.Errorf("diagnostics missing %q: %v", want, diagnostics)
		}
	}

	diagnostics = nil
	if _, err := Load(root, IngestOptions{
		CacheDir:   cacheDir,
		Diagnostic: func(message string) { diagnostics = append(diagnostics, message) },
	}); err != nil {
		t.Fatal(err)
	}
	if !containsDiagnostic(diagnostics, "opencode cache: hit path=") {
		t.Fatalf("cache hit diagnostic missing: %v", diagnostics)
	}
}

func TestOpenCodeDiagnosticsReportRevisionChangeDuringRead(t *testing.T) {
	root := t.TempDir()
	writeNormalizerFixture(t, root)
	database := filepath.Join(root, "opencode.db")
	info, err := os.Stat(database)
	if err != nil {
		t.Fatal(err)
	}
	changedAt := info.ModTime().Add(time.Second)
	var diagnostics []string
	if _, err := Load(root, IngestOptions{
		CacheDir: t.TempDir(),
		Diagnostic: func(message string) {
			diagnostics = append(diagnostics, message)
			if strings.HasPrefix(message, "opencode source: reading database ") {
				if err := os.Chtimes(database, changedAt, changedAt); err != nil {
					t.Fatalf("change source revision: %v", err)
				}
			}
		},
	}); err != nil {
		t.Fatal(err)
	}
	if !containsDiagnostic(diagnostics, "opencode cache skipped; source revision changed: before=") {
		t.Fatalf("revision change diagnostic missing: %v", diagnostics)
	}
}

func TestLoadInvalidatesOpenCodeCacheForDatabaseAndWALChanges(t *testing.T) {
	root := t.TempDir()
	writeNormalizerFixture(t, root)
	cacheDir := t.TempDir()
	now := time.UnixMilli(1_700_000_020_000).UTC()
	if _, err := Load(root, IngestOptions{Now: now, CacheDir: cacheDir}); err != nil {
		t.Fatal(err)
	}

	database, err := sql.Open("sqlite", filepath.Join(root, "opencode.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO session VALUES (?, ?, ?, ?, ?)`, "s2", "/workspace/second", "1.18.27", int64(1_700_000_011_000), int64(1_700_000_011_000)); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	var diagnostics []string
	changed, err := Load(root, IngestOptions{Now: now, CacheDir: cacheDir, Diagnostic: func(message string) { diagnostics = append(diagnostics, message) }})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed.Sessions) != 2 || !containsDiagnostic(diagnostics, "cache miss; reading source") {
		t.Fatalf("database change was not re-read: sessions=%#v diagnostics=%v", changed.Sessions, diagnostics)
	}

	if err := os.WriteFile(filepath.Join(root, "opencode.db-wal"), []byte("wal-change"), 0o600); err != nil {
		t.Fatal(err)
	}
	diagnostics = nil
	if _, err := Load(root, IngestOptions{Now: now, CacheDir: cacheDir, Diagnostic: func(message string) { diagnostics = append(diagnostics, message) }}); err != nil {
		t.Fatal(err)
	}
	if !containsDiagnostic(diagnostics, "cache miss; reading source") {
		t.Fatalf("WAL change did not invalidate cache: %v", diagnostics)
	}
}

func TestLoadTreatsCorruptOpenCodeCacheAsMiss(t *testing.T) {
	root := t.TempDir()
	writeNormalizerFixture(t, root)
	cacheDir := t.TempDir()
	store := cache.New(cacheDir)
	cachePath := store.Path("opencode", root)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	var diagnostics []string
	if _, err := Load(root, IngestOptions{CacheDir: cacheDir, Diagnostic: func(message string) { diagnostics = append(diagnostics, message) }}); err != nil {
		t.Fatal(err)
	}
	if !containsDiagnostic(diagnostics, "cache miss; reading source") {
		t.Fatalf("corrupt cache was not reported as a miss: %v", diagnostics)
	}
}

func TestOpenCodeCacheSeparatesDataRoots(t *testing.T) {
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	writeNormalizerFixture(t, firstRoot)
	writeNormalizerFixture(t, secondRoot)
	cacheDir := t.TempDir()
	for _, root := range []string{firstRoot, secondRoot} {
		if _, err := Load(root, IngestOptions{CacheDir: cacheDir}); err != nil {
			t.Fatal(err)
		}
	}
	firstPath := cache.New(cacheDir).Path("opencode", firstRoot)
	secondPath := cache.New(cacheDir).Path("opencode", secondRoot)
	if firstPath == secondPath {
		t.Fatalf("different roots share cache path: %q", firstPath)
	}
	if _, err := os.Stat(firstPath); err != nil {
		t.Fatalf("first root cache missing: %v", err)
	}
	if _, err := os.Stat(secondPath); err != nil {
		t.Fatalf("second root cache missing: %v", err)
	}
}

func openCodeReport(result IngestResult) aggregate.Report {
	return aggregate.BuildOverview(aggregate.Input{
		Turns:        result.Turns,
		SessionCount: len(result.Sessions),
		Warnings:     result.Warnings,
		Source:       usage.SourceOpenCode,
		Agents:       result.Agents,
	})
}

func containsDiagnostic(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}
