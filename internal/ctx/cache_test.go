package ctx

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xkumiyu/catsift/internal/aggregate"
	"github.com/xkumiyu/catsift/internal/cache"
	"github.com/xkumiyu/catsift/internal/usage"
)

func TestLoadUsesCompleteGenerationCacheAndLocalDaysFilter(t *testing.T) {
	data := strings.Join([]string{
		eventLine(t, "old", "codex", "session", "message", "user", "2026-01-01T00:00:00Z", "old", nil),
		eventLine(t, "new", "codex", "session", "message", "user", "2026-01-02T00:00:00Z", "new", nil),
		completionLine(t, "generation-1", "", true),
	}, "\n") + "\n"
	cacheDir := t.TempDir()
	var calls [][]string
	runner := func(args []string) (CommandResult, error) {
		calls = append(calls, append([]string(nil), args...))
		if containsPair(args, "--limit", "1") {
			return CommandResult{Stdout: []byte(completionLine(t, "generation-1", "", true) + "\n")}, nil
		}
		return CommandResult{Stdout: []byte(data)}, nil
	}
	now := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	all, err := Load("/tmp/ctx-cache", IngestOptions{DataRoot: "/tmp/ctx-cache", Now: now, CacheDir: cacheDir, Runner: runner})
	if err != nil || len(all.Turns) != 2 {
		t.Fatalf("cold load = %#v, err = %v", all, err)
	}
	if len(calls) != 2 || containsPair(calls[1], "--limit", "1") {
		t.Fatalf("cold calls = %#v", calls)
	}

	calls = nil
	day, err := Load("/tmp/ctx-cache", IngestOptions{DataRoot: "/tmp/ctx-cache", Days: 1, DaysSet: true, Now: now, CacheDir: cacheDir, Runner: runner})
	if err != nil || len(day.Turns) != 1 || day.Turns[0].UserPrompts != 1 {
		t.Fatalf("warm day load = %#v, err = %v", day, err)
	}
	if len(calls) != 1 || !containsPair(calls[0], "--limit", "1") {
		t.Fatalf("warm cache calls = %#v", calls)
	}
}

func TestLoadReportsCacheActivity(t *testing.T) {
	data := strings.Join([]string{
		eventLine(t, "old", "codex", "session", "message", "user", "2026-01-01T00:00:00Z", "old", nil),
		completionLine(t, "generation-1", "", true),
	}, "\n") + "\n"
	cacheDir := t.TempDir()
	runner := func(args []string) (CommandResult, error) {
		if containsPair(args, "--limit", "1") {
			return CommandResult{Stdout: []byte(completionLine(t, "generation-1", "", true) + "\n")}, nil
		}
		return CommandResult{Stdout: []byte(data)}, nil
	}
	now := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	var cold []string
	if _, err := Load("/tmp/ctx-diagnostics", IngestOptions{
		DataRoot: "/tmp/ctx-diagnostics",
		Now:      now,
		CacheDir: cacheDir,
		Runner:   runner,
		Diagnostic: func(message string) {
			cold = append(cold, message)
		},
	}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"ctx source: data-root=",
		"ctx cache: lookup path=",
		"parser=ctx-normalizer-v2",
		"ctx cache: checking generation",
		"ctx cache: miss; reading source",
		"ctx source: reading full event stream",
		"ctx cache: stored complete generation",
	} {
		if !containsDiagnostic(cold, want) {
			t.Errorf("cold diagnostics missing %q: %v", want, cold)
		}
	}

	var warm []string
	if _, err := Load("/tmp/ctx-diagnostics", IngestOptions{
		DataRoot: "/tmp/ctx-diagnostics",
		Days:     1,
		DaysSet:  true,
		Now:      now,
		CacheDir: cacheDir,
		Runner:   runner,
		Diagnostic: func(message string) {
			warm = append(warm, message)
		},
	}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"ctx source: data-root=",
		"ctx cache: lookup path=",
		"parser=ctx-normalizer-v2",
		"ctx cache: generation=",
		"ctx cache: checking generation",
		"ctx cache: hit; applying selected period locally",
	} {
		if !containsDiagnostic(warm, want) {
			t.Errorf("warm diagnostics missing %q: %v", want, warm)
		}
	}

	var disabled []string
	if _, err := Load("/tmp/ctx-diagnostics-disabled", IngestOptions{
		DataRoot: "/tmp/ctx-diagnostics-disabled",
		Now:      now,
		Runner:   runner,
		Diagnostic: func(message string) {
			disabled = append(disabled, message)
		},
	}); err != nil {
		t.Fatal(err)
	}
	if !containsString(disabled, "ctx cache: disabled; reading source") {
		t.Errorf("disabled-cache diagnostic missing: %v", disabled)
	}
}

func TestLoadUsesRecentCacheWhenGenerationProbeFails(t *testing.T) {
	data := strings.Join([]string{
		eventLine(t, "cached", "codex", "session", "message", "user", "2026-01-02T00:00:00Z", "cached", nil),
		completionLine(t, "generation-1", "", true),
	}, "\n") + "\n"
	cacheDir := t.TempDir()
	now := time.Now().UTC()
	populate := func(args []string) (CommandResult, error) {
		if containsPair(args, "--limit", "1") {
			return CommandResult{Stdout: []byte(completionLine(t, "generation-1", "", true) + "\n")}, nil
		}
		return CommandResult{Stdout: []byte(data)}, nil
	}
	if _, err := Load("/tmp/ctx-stale", IngestOptions{
		DataRoot: "/tmp/ctx-stale",
		Now:      now,
		CacheDir: cacheDir,
		Runner:   populate,
	}); err != nil {
		t.Fatal(err)
	}
	cachePath := cache.New(cacheDir).Path("ctx", "/tmp/ctx-stale")
	if err := os.Chtimes(cachePath, now.Add(-time.Minute), now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}

	var calls [][]string
	failing := func(args []string) (CommandResult, error) {
		calls = append(calls, append([]string(nil), args...))
		return CommandResult{ExitCode: 1, Stderr: []byte("ctx unavailable")}, nil
	}
	result, err := Load("/tmp/ctx-stale", IngestOptions{
		DataRoot: "/tmp/ctx-stale",
		Now:      now,
		CacheDir: cacheDir,
		Runner:   failing,
	})
	if err != nil {
		t.Fatalf("stale fallback error = %v", err)
	}
	if len(result.Turns) != 1 || result.Turns[0].Source.EventID != "cached" {
		t.Fatalf("stale fallback result = %#v", result)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Reason != "stale_cache" || result.Warnings[0].Type != "ctx" {
		t.Fatalf("stale fallback warnings = %#v", result.Warnings)
	}
	if len(calls) != 1 || !containsPair(calls[0], "--limit", "1") {
		t.Fatalf("stale fallback calls = %#v", calls)
	}
}

func TestLoadUsesRecentCacheAfterFreshReadFails(t *testing.T) {
	data := strings.Join([]string{
		eventLine(t, "cached", "codex", "session", "message", "user", "2026-01-02T00:00:00Z", "cached", nil),
		completionLine(t, "generation-1", "", true),
	}, "\n") + "\n"
	cacheDir := t.TempDir()
	now := time.Now().UTC()
	populate := func(args []string) (CommandResult, error) {
		if containsPair(args, "--limit", "1") {
			return CommandResult{Stdout: []byte(completionLine(t, "generation-1", "", true) + "\n")}, nil
		}
		return CommandResult{Stdout: []byte(data)}, nil
	}
	if _, err := Load("/tmp/ctx-stale-read", IngestOptions{
		DataRoot: "/tmp/ctx-stale-read",
		Now:      now,
		CacheDir: cacheDir,
		Runner:   populate,
	}); err != nil {
		t.Fatal(err)
	}
	cachePath := cache.New(cacheDir).Path("ctx", "/tmp/ctx-stale-read")
	if err := os.Chtimes(cachePath, now.Add(-time.Minute), now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}

	var calls [][]string
	failing := func(args []string) (CommandResult, error) {
		calls = append(calls, append([]string(nil), args...))
		if containsPair(args, "--limit", "1") {
			return CommandResult{Stdout: []byte(completionLine(t, "generation-2", "", true) + "\n")}, nil
		}
		return CommandResult{ExitCode: 1, Stderr: []byte("ctx read failed")}, nil
	}
	result, err := Load("/tmp/ctx-stale-read", IngestOptions{
		DataRoot: "/tmp/ctx-stale-read",
		Now:      now,
		CacheDir: cacheDir,
		Runner:   failing,
	})
	if err != nil {
		t.Fatalf("stale read fallback error = %v", err)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Reason != "stale_cache" {
		t.Fatalf("stale read fallback warnings = %#v", result.Warnings)
	}
	if len(calls) != 2 || !containsPair(calls[0], "--limit", "1") || containsPair(calls[1], "--limit", "1") {
		t.Fatalf("stale read fallback calls = %#v", calls)
	}
}

func TestLoadDoesNotUseExpiredCacheWhenGenerationProbeFails(t *testing.T) {
	data := strings.Join([]string{
		eventLine(t, "cached", "codex", "session", "message", "user", "2026-01-02T00:00:00Z", "cached", nil),
		completionLine(t, "generation-1", "", true),
	}, "\n") + "\n"
	cacheDir := t.TempDir()
	now := time.Now().UTC()
	populate := func(args []string) (CommandResult, error) {
		if containsPair(args, "--limit", "1") {
			return CommandResult{Stdout: []byte(completionLine(t, "generation-1", "", true) + "\n")}, nil
		}
		return CommandResult{Stdout: []byte(data)}, nil
	}
	if _, err := Load("/tmp/ctx-expired", IngestOptions{
		DataRoot: "/tmp/ctx-expired",
		Now:      now,
		CacheDir: cacheDir,
		Runner:   populate,
	}); err != nil {
		t.Fatal(err)
	}
	cachePath := cache.New(cacheDir).Path("ctx", "/tmp/ctx-expired")
	old := now.Add(-maxStaleCacheAge - time.Minute)
	if err := os.Chtimes(cachePath, old, old); err != nil {
		t.Fatal(err)
	}

	var calls [][]string
	refresh := func(args []string) (CommandResult, error) {
		calls = append(calls, append([]string(nil), args...))
		if containsPair(args, "--limit", "1") {
			return CommandResult{ExitCode: 1, Stderr: []byte("ctx unavailable")}, nil
		}
		return CommandResult{Stdout: []byte(strings.Replace(data, "generation-1", "generation-2", 1))}, nil
	}
	result, err := Load("/tmp/ctx-expired", IngestOptions{
		DataRoot: "/tmp/ctx-expired",
		Now:      now,
		CacheDir: cacheDir,
		Runner:   refresh,
	})
	if err != nil {
		t.Fatalf("expired cache refresh error = %v", err)
	}
	if len(calls) != 2 || !containsPair(calls[0], "--limit", "1") || containsPair(calls[1], "--limit", "1") {
		t.Fatalf("expired cache calls = %#v", calls)
	}
	for _, warning := range result.Warnings {
		if warning.Reason == staleCacheWarning {
			t.Fatalf("expired cache unexpectedly used stale fallback: %#v", result.Warnings)
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsDiagnostic(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}

func TestLoadRefetchesWhenGenerationChangesAndWarmsRangeCache(t *testing.T) {
	cacheDir := t.TempDir()
	var generation string
	var calls [][]string
	runner := func(args []string) (CommandResult, error) {
		calls = append(calls, append([]string(nil), args...))
		if containsPair(args, "--limit", "1") {
			return CommandResult{Stdout: []byte(completionLine(t, generation, "", true) + "\n")}, nil
		}
		full := strings.Join([]string{
			eventLine(t, "new", "codex", "session", "message", "user", "2026-01-02T00:00:00Z", "new", nil),
			completionLine(t, generation, "", true),
		}, "\n") + "\n"
		return CommandResult{Stdout: []byte(full)}, nil
	}
	generation = "generation-1"
	if _, err := Load("/tmp/ctx-generation", IngestOptions{DataRoot: "/tmp/ctx-generation", Now: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), CacheDir: cacheDir, Runner: runner}); err != nil {
		t.Fatal(err)
	}
	generation = "generation-2"
	calls = nil
	if _, err := Load("/tmp/ctx-generation", IngestOptions{DataRoot: "/tmp/ctx-generation", Now: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), CacheDir: cacheDir, Runner: runner}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || containsPair(calls[1], "--limit", "1") {
		t.Fatalf("generation refresh calls = %#v", calls)
	}

	calls = nil
	rangeResult, err := Load("/tmp/ctx-range", IngestOptions{DataRoot: "/tmp/ctx-range", Days: 1, DaysSet: true, Now: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), CacheDir: cacheDir, Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	if len(rangeResult.Turns) != 1 || rangeResult.Turns[0].UserPrompts != 1 {
		t.Fatalf("range result = %#v", rangeResult)
	}
	if len(calls) != 2 || containsPair(calls[1], "--since", "2026-01-02T00:00:00Z") {
		t.Fatalf("range cache-warming calls = %#v", calls)
	}
	if _, hit, err := cache.New(cacheDir).Read("ctx", "/tmp/ctx-range", "generation-2", ctxParserVersion); err != nil || !hit {
		t.Fatalf("range query did not populate complete cache: hit=%v err=%v", hit, err)
	}
}

func TestCtxColdAndWarmReportsHaveJSONParity(t *testing.T) {
	data := strings.Join([]string{
		eventLine(t, "message", "codex", "session", "message", "user", "2026-01-02T00:00:00Z", "$review", nil),
		eventLine(t, "tool", "codex", "session", "tool_call", "assistant", "2026-01-02T00:00:01Z", "", map[string]any{
			"invocation": map[string]any{"id": "call", "protocol": "command", "name": "exec", "arguments": map[string]any{"cmd": "true"}},
		}),
		completionLine(t, "generation-1", "", true),
	}, "\n") + "\n"
	runner := func(args []string) (CommandResult, error) {
		if containsPair(args, "--limit", "1") {
			return CommandResult{Stdout: []byte(completionLine(t, "generation-1", "", true) + "\n")}, nil
		}
		return CommandResult{Stdout: []byte(data)}, nil
	}
	now := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	fresh, err := Load("/tmp/ctx-parity", IngestOptions{DataRoot: "/tmp/ctx-parity", Now: now, Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	cacheDir := t.TempDir()
	cold, err := Load("/tmp/ctx-parity", IngestOptions{DataRoot: "/tmp/ctx-parity", Now: now, CacheDir: cacheDir, Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	warm, err := Load("/tmp/ctx-parity", IngestOptions{DataRoot: "/tmp/ctx-parity", Now: now, CacheDir: cacheDir, Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	input := func(result IngestResult) aggregate.Input {
		return aggregate.Input{Turns: result.Turns, SessionCount: len(result.Sessions), Warnings: result.Warnings, Source: usage.SourceCtx, Agents: result.Agents}
	}
	freshReport := aggregate.BuildOverview(input(fresh))
	if !reflect.DeepEqual(freshReport, aggregate.BuildOverview(input(cold))) || !reflect.DeepEqual(freshReport, aggregate.BuildOverview(input(warm))) {
		t.Fatalf("aggregate report differs between fresh/cold/warm: fresh=%#v cold=%#v warm=%#v", freshReport, aggregate.BuildOverview(input(cold)), aggregate.BuildOverview(input(warm)))
	}
	freshJSON, err := json.Marshal(freshReport)
	if err != nil {
		t.Fatal(err)
	}
	warmJSON, err := json.Marshal(aggregate.BuildOverview(input(warm)))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(freshJSON, warmJSON) {
		t.Fatalf("report JSON differs: fresh=%s warm=%s", freshJSON, warmJSON)
	}
}

func TestLoadDoesNotPublishIncompleteGenerationCache(t *testing.T) {
	cacheDir := t.TempDir()
	runner := func(args []string) (CommandResult, error) {
		if containsPair(args, "--limit", "1") {
			return CommandResult{Stdout: []byte(completionLine(t, "generation-incomplete", "", true) + "\n")}, nil
		}
		return CommandResult{Stdout: []byte(eventLine(t, "event", "codex", "session", "message", "user", "2026-01-02T00:00:00Z", "hello", nil) + "\n")}, nil
	}
	_, err := Load("/tmp/ctx-incomplete", IngestOptions{DataRoot: "/tmp/ctx-incomplete", Now: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), CacheDir: cacheDir, Runner: runner})
	if err == nil || !strings.Contains(err.Error(), "completion") {
		t.Fatalf("incomplete stream error = %v", err)
	}
	if _, hit, readErr := cache.New(cacheDir).Read("ctx", "/tmp/ctx-incomplete", "generation-incomplete", ctxParserVersion); readErr != nil || hit {
		t.Fatalf("incomplete stream cache = hit %v, err %v", hit, readErr)
	}
}

func TestParsePageReaderStreamsEventsToCallback(t *testing.T) {
	data := strings.Join([]string{
		eventLine(t, "event", "codex", "session", "message", "user", "2026-01-02T00:00:00Z", "hello", nil),
		completionLine(t, "generation-1", "", true),
	}, "\n") + "\n"
	count := 0
	_, err := parsePageReader(strings.NewReader(data), func(event) { count++ })
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("streamed events = %d", count)
	}
}

func TestParsePageReaderSkipsOversizedLineAndContinues(t *testing.T) {
	input := strings.Repeat("x", ctxMaxLineBytes+1) + "\n" + completionLine(t, "generation-1", "", true) + "\n"
	page, err := parsePageReader(strings.NewReader(input), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !page.Complete || len(page.Warnings) != 1 || page.Warnings[0].Reason != "ctx_large_line" {
		t.Fatalf("oversized line handling = %#v", page)
	}
}

func TestLoadUsesBoundedHundredThousandEventPageLimit(t *testing.T) {
	var args []string
	runner := func(value []string) (CommandResult, error) {
		args = append([]string(nil), value...)
		return CommandResult{Stdout: []byte(completionLine(t, "generation-1", "", true) + "\n")}, nil
	}
	if _, err := Load("/tmp/ctx-limit", IngestOptions{Runner: runner}); err != nil {
		t.Fatal(err)
	}
	if !containsPair(args, "--limit", "100000") {
		t.Fatalf("event page limit args = %#v", args)
	}
}

func TestLoadIgnoresUnavailableCacheAndReturnsSourceResult(t *testing.T) {
	data := strings.Join([]string{
		eventLine(t, "event", "codex", "session", "message", "user", "2026-01-02T00:00:00Z", "hello", nil),
		completionLine(t, "generation-1", "", true),
	}, "\n") + "\n"
	runner := func(args []string) (CommandResult, error) {
		if containsPair(args, "--limit", "1") {
			return CommandResult{Stdout: []byte(completionLine(t, "generation-1", "", true) + "\n")}, nil
		}
		return CommandResult{Stdout: []byte(data)}, nil
	}
	cachePath := t.TempDir() + "/cache-is-a-file"
	if err := os.WriteFile(cachePath, []byte("cache target"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	fresh, err := Load("/tmp/ctx-unavailable-cache", IngestOptions{DataRoot: "/tmp/ctx-unavailable-cache", Now: now, Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	withUnavailableCache, err := Load("/tmp/ctx-unavailable-cache", IngestOptions{DataRoot: "/tmp/ctx-unavailable-cache", Now: now, CacheDir: cachePath, Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fresh.Turns, withUnavailableCache.Turns) || !reflect.DeepEqual(fresh.Warnings, withUnavailableCache.Warnings) {
		t.Fatalf("cache failure changed source result: fresh=%#v cached=%#v", fresh, withUnavailableCache)
	}
}

func TestLoadKeepsCtxDataRootReadOnly(t *testing.T) {
	dataRoot := t.TempDir()
	sentinel := filepath.Join(dataRoot, "sentinel")
	if err := os.WriteFile(sentinel, []byte("ctx data"), 0o640); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(sentinel)
	if err != nil {
		t.Fatal(err)
	}
	runner := func(args []string) (CommandResult, error) {
		if containsPair(args, "--limit", "1") {
			return CommandResult{Stdout: []byte(completionLine(t, "generation-1", "", true) + "\n")}, nil
		}
		return CommandResult{Stdout: []byte(completionLine(t, "generation-1", "", true) + "\n")}, nil
	}
	if _, err := Load(dataRoot, IngestOptions{DataRoot: dataRoot, CacheDir: t.TempDir(), Runner: runner}); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(sentinel)
	if err != nil {
		t.Fatal(err)
	}
	if before.Size() != after.Size() || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("ctx data root metadata changed: before=%v after=%v", before, after)
	}
}
