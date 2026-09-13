package codex

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

func TestLoadCachesCompleteFileSnapshotAndFiltersDaysLocally(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	contents := "{" + `"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s"}` + "}\n" +
		"{" + `"timestamp":"2026-01-01T00:00:01Z","type":"user_message","payload":{"text":"old"}` + "}\n" +
		"{" + `"timestamp":"2026-01-01T00:00:02Z","type":"task_complete","payload":{}` + "}\n" +
		"{" + `"timestamp":"2026-01-02T00:00:00Z","type":"user_message","payload":{"text":"new"}` + "}\n" +
		"{" + `"timestamp":"2026-01-02T00:00:01Z","type":"task_complete","payload":{}` + "}\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	cacheDir := t.TempDir()
	now := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	all, err := Load(home, IngestOptions{Now: now, CacheDir: cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Turns) != 2 {
		t.Fatalf("all-time turns = %#v", all.Turns)
	}

	day, err := Load(home, IngestOptions{Days: 1, DaysSet: true, Now: now, CacheDir: cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(day.Turns) != 1 || day.Turns[0].UserPrompts != 1 {
		t.Fatalf("local day filter turns = %#v", day.Turns)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	revision := fileRevision(info)
	if _, hit, err := cache.New(cacheDir).Read("codex", path, revision, codexParserVersion); err != nil || !hit {
		t.Fatalf("file snapshot cache hit = %v, err = %v", hit, err)
	}
}

func TestLoadDoesNotReusePreModelAttributionParserCache(t *testing.T) {
	cacheDir := t.TempDir()
	path := filepath.Join(t.TempDir(), "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.New(cacheDir).Write("codex", path, fileRevision(info), "codex-normalizer-v2", cache.Snapshot{}); err != nil {
		t.Fatal(err)
	}
	if _, hit, err := cache.New(cacheDir).Read("codex", path, fileRevision(info), codexParserVersion); err != nil {
		t.Fatal(err)
	} else if hit {
		t.Fatal("pre-model-attribution parser cache must be a miss")
	}
}

func TestLoadFiltersSessionsByDaysForFreshAndCachedResults(t *testing.T) {
	home := t.TempDir()
	sessionsDir := filepath.Join(home, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSession := func(name, id, timestamp string) {
		t.Helper()
		contents := strings.Join([]string{
			`{"timestamp":"` + timestamp + `","type":"session_meta","payload":{"id":"` + id + `"}}`,
			`{"timestamp":"` + timestamp + `","type":"user_message","payload":{"text":"hello"}}`,
			`{"timestamp":"` + timestamp + `","type":"task_complete","payload":{}}`,
		}, "\n") + "\n"
		if err := os.WriteFile(filepath.Join(sessionsDir, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeSession("old.jsonl", "old", "2026-01-01T00:00:00Z")
	writeSession("new.jsonl", "new", "2026-01-02T00:00:00Z")
	now := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)

	fresh, err := Load(home, IngestOptions{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	freshDay, err := Load(home, IngestOptions{Days: 1, DaysSet: true, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh.Sessions) != 2 || len(freshDay.Sessions) != 1 || freshDay.Sessions[0].ID != "new" {
		t.Fatalf("fresh sessions = %#v, fresh day sessions = %#v", fresh.Sessions, freshDay.Sessions)
	}

	cacheDir := t.TempDir()
	if _, err := Load(home, IngestOptions{Now: now, CacheDir: cacheDir}); err != nil {
		t.Fatal(err)
	}
	cachedDay, err := Load(home, IngestOptions{Days: 1, DaysSet: true, Now: now, CacheDir: cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(cachedDay.Sessions) != 1 || cachedDay.Sessions[0].ID != "new" {
		t.Fatalf("cached day sessions = %#v", cachedDay.Sessions)
	}
	cachedTo, err := Load(home, IngestOptions{To: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Now: now, CacheDir: cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(cachedTo.Sessions) != 1 || cachedTo.Sessions[0].ID != "old" {
		t.Fatalf("cached upper-only sessions = %#v", cachedTo.Sessions)
	}
}

func TestLoadFiltersTokenUsageByDateRangeForFreshAndCachedResults(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-01-01T23:59:59Z","type":"session_meta","payload":{"id":"s"}}`,
		`{"timestamp":"2026-01-02T00:00:00Z","type":"user_message","payload":{"text":"hello"}}`,
		`{"timestamp":"2026-01-01T23:59:59Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":10,"total_tokens":10}}}}`,
		`{"timestamp":"2026-01-02T00:00:01Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":20,"total_tokens":20}}}}`,
		`{"timestamp":"2026-01-02T00:00:02Z","type":"task_complete","payload":{}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	options := IngestOptions{
		From: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC),
		Now:  time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC),
	}
	fresh, err := Load(home, options)
	if err != nil {
		t.Fatal(err)
	}
	cacheDir := t.TempDir()
	options.CacheDir = cacheDir
	cold, err := Load(home, options)
	if err != nil {
		t.Fatal(err)
	}
	warm, err := Load(home, options)
	if err != nil {
		t.Fatal(err)
	}
	toReport := func(result IngestResult) aggregate.Report {
		return aggregate.BuildOverview(aggregate.Input{
			Turns:        result.Turns,
			SessionCount: len(result.Sessions),
			Warnings:     result.Warnings,
			Source:       usage.SourceCodex,
			Agents:       []string{"codex"},
		})
	}
	freshReport := toReport(fresh)
	if freshReport.Overview.TokenUsage.InputTokens != 20 {
		t.Fatalf("fresh token usage = %#v, want only in-range usage", freshReport.Overview.TokenUsage)
	}
	if !reflect.DeepEqual(freshReport, toReport(cold)) || !reflect.DeepEqual(freshReport, toReport(warm)) {
		t.Fatalf("date-range aggregate differs between fresh/cold/warm: fresh=%#v cold=%#v warm=%#v", freshReport, toReport(cold), toReport(warm))
	}
}

func TestLoadInvalidatesChangedAndDeletedFiles(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(text string) {
		t.Helper()
		data := "{" + `"timestamp":"2026-01-02T00:00:00Z","type":"session_meta","payload":{"id":"s"}` + "}\n" +
			"{" + `"timestamp":"2026-01-02T00:00:01Z","type":"user_message","payload":{"text":"` + text + `"}` + "}\n"
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("first")
	cacheDir := t.TempDir()
	first, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), CacheDir: cacheDir})
	if err != nil || len(first.Turns) != 1 {
		t.Fatalf("first load = %#v, err = %v", first, err)
	}
	write("second-content-is-longer")
	second, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), CacheDir: cacheDir})
	if err != nil || len(second.Turns) != 1 {
		t.Fatalf("changed load = %#v, err = %v", second, err)
	}
	_ = os.Remove(path)
	deleted, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), CacheDir: cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted.Turns) != 0 || len(deleted.Sessions) != 0 {
		t.Fatalf("deleted file was not excluded: %#v", deleted)
	}
}

func TestCodexColdAndWarmReportsHaveJSONParity(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-01-02T00:00:00Z","type":"session_meta","payload":{"id":"s"}}`,
		`{"timestamp":"2026-01-02T00:00:01Z","type":"user_message","payload":{"text":"$review"}}`,
		`{"timestamp":"2026-01-02T00:00:02Z","type":"response_item","payload":{"type":"function_call","name":"exec","call_id":"c1","arguments":"{\"cmd\":\"true\"}"}}`,
		`{"timestamp":"2026-01-02T00:00:03Z","type":"event_msg","payload":{"type":"ItemCompleted","item":{"type":"CommandExecution","id":"i1","status":"completed","command":"cat /x/skills/review/SKILL.md"}}}`,
		`{"timestamp":"2026-01-02T00:00:03Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":10,"cached_input_tokens":4,"cache_write_input_tokens":1,"output_tokens":3,"reasoning_output_tokens":2,"total_tokens":13}}}}`,
		`{"timestamp":"2026-01-02T00:00:04Z","type":"task_complete","payload":{}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	fresh, err := Load(home, IngestOptions{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	cacheDir := t.TempDir()
	cold, err := Load(home, IngestOptions{Now: now, CacheDir: cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	warm, err := Load(home, IngestOptions{Now: now, CacheDir: cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	input := func(result IngestResult) aggregate.Input {
		return aggregate.Input{Turns: result.Turns, SessionCount: len(result.Sessions), Warnings: result.Warnings, Source: usage.SourceCodex, Agents: []string{"codex"}}
	}
	freshReport := aggregate.BuildOverview(input(fresh))
	if !reflect.DeepEqual(freshReport, aggregate.BuildOverview(input(cold))) || !reflect.DeepEqual(freshReport, aggregate.BuildOverview(input(warm))) {
		t.Fatalf("aggregate report differs between fresh/cold/warm: fresh=%#v cold=%#v warm=%#v", freshReport, aggregate.BuildOverview(input(cold)), aggregate.BuildOverview(input(warm)))
	}
	wantUsage := usage.TokenUsage{InputTokens: 10, CachedInputTokens: 4, CacheWriteInputTokens: 1, OutputTokens: 3, ReasoningOutputTokens: 2, TotalTokens: 13}
	if freshReport.Overview.TokenUsage != wantUsage {
		t.Fatalf("fresh token usage = %#v, want %#v", freshReport.Overview.TokenUsage, wantUsage)
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

func TestLoadIgnoresUnavailableCacheAndReturnsSourceResult(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{\"timestamp\":\"2026-01-02T00:00:00Z\",\"type\":\"user_message\",\"payload\":{\"text\":\"hello\"}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(t.TempDir(), "cache-is-a-file")
	if err := os.WriteFile(cachePath, []byte("cache target"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	fresh, err := Load(home, IngestOptions{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	withUnavailableCache, err := Load(home, IngestOptions{Now: now, CacheDir: cachePath})
	if err != nil {
		t.Fatal(err)
	}
	toInput := func(result IngestResult) aggregate.Input {
		return aggregate.Input{Turns: result.Turns, SessionCount: len(result.Sessions), Warnings: result.Warnings, Source: usage.SourceCodex, Agents: []string{"codex"}}
	}
	if !reflect.DeepEqual(aggregate.BuildOverview(toInput(fresh)), aggregate.BuildOverview(toInput(withUnavailableCache))) {
		t.Fatalf("cache failure changed report result: fresh=%#v cached=%#v", fresh, withUnavailableCache)
	}
}
