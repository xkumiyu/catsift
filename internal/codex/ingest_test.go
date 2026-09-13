package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xkumiyu/catsift/internal/usage"
)

func TestResolveHomePrecedence(t *testing.T) {
	if got, _ := ResolveHomeFrom(" /explicit ", "/env", "/user"); got != " /explicit " {
		t.Fatalf("explicit home was unexpectedly trimmed: %q", got)
	}
	if got, _ := ResolveHomeFrom("", "/env", "/user"); got != "/env" {
		t.Fatalf("env home = %q", got)
	}
	if got, _ := ResolveHomeFrom("", "", "/user"); got != "/user/.codex" {
		t.Fatalf("default home = %q", got)
	}
	if _, err := ResolveHomeFrom("", "", ""); err == nil {
		t.Fatal("empty user home should fail")
	}
}

func TestDiscoverSortsBothRootsAndIgnoresMissingRoot(t *testing.T) {
	home := t.TempDir()
	paths := []string{
		filepath.Join(home, "sessions", "z", "two.jsonl"),
		filepath.Join(home, "sessions", "a.jsonl"),
		filepath.Join(home, "archived_sessions", "one.jsonl"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Discover(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(paths) || !strings.HasSuffix(got[0], "archived_sessions/one.jsonl") {
		t.Fatalf("unexpected files: %#v", got)
	}
	if _, err := Discover(filepath.Join(home, "missing")); err == nil {
		t.Fatal("missing home should fail")
	}
}

func TestDecodeFileContinuesAfterMalformedUnknownAndLargeLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	contents := "{\"timestamp\":\"2026-01-01T00:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"s\"}}\n" +
		"not-json\n" +
		"{\"type\":\"future\"}\n" +
		strings.Repeat("x", 512) + "\n" +
		"{\"timestamp\":\"2026-01-01T00:00:01Z\",\"type\":\"task_started\",\"payload\":{\"id\":\"t\"}}\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	var got []Envelope
	warnings := &WarningCollector{}
	if err := DecodeFile(path, DecodeOptions{MaxLineBytes: 256, Warnings: warnings}, func(env Envelope) { got = append(got, env) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Source.Line != 1 || got[1].Source.Line != 5 {
		t.Fatalf("decoded envelopes = %#v", got)
	}
	if len(warnings.Warnings()) != 3 {
		t.Fatalf("warnings = %#v", warnings.Warnings())
	}
}

func TestDecodeFileSkipsKnownMetadataWithoutWarning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	contents := strings.Join([]string{
		`{"type":"session_meta","payload":{"id":"s"}}`,
		`{"type":"turn_context","payload":{"turn_id":"t","model":"gpt"}}`,
		`{"type":"world_state","payload":{"cwd":"/tmp"}}`,
		`{"type":"compacted","payload":{"turn_id":"t"}}`,
		`{"type":"inter_agent_communication_metadata","payload":{"turn_id":"t"}}`,
		`{"type":"task_started","payload":{"turn_id":"t"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	var got []Envelope
	warnings := &WarningCollector{}
	if err := DecodeFile(path, DecodeOptions{Warnings: warnings}, func(env Envelope) { got = append(got, env) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Type != "session_meta" || got[1].Type != "turn_context" || got[2].Type != "task_started" {
		t.Fatalf("decoded envelopes = %#v", got)
	}
	if len(warnings.Warnings()) != 0 {
		t.Fatalf("metadata warnings = %#v", warnings.Warnings())
	}
}

func TestLoadAssemblesExplicitAndOrdinalTurnsAndFlushesAbort(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "2026", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	contents := strings.Join([]string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s","project_path":"/project","cli_version":"1"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"task_started","payload":{"turn_id":"turn-a"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"event_msg","payload":{"type":"user_message","text":"hello"}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"task_complete","payload":{"turn_id":"turn-a"}}`,
		`{"timestamp":"2026-01-01T00:00:04Z","type":"user_message","payload":{"text":"next"}}`,
		`{"timestamp":"2026-01-01T00:00:05Z","type":"turn_aborted","payload":{}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 2 {
		t.Fatalf("turns = %#v", result.Turns)
	}
	if result.Turns[0].ID != "turn-a" || result.Turns[0].UserPrompts != 1 || result.Turns[1].Ordinal != 2 || !result.Turns[1].Aborted {
		t.Fatalf("assembled turns = %#v", result.Turns)
	}
	if len(result.Sessions) != 1 || result.Sessions[0].ProjectPath != "/project" {
		t.Fatalf("sessions = %#v", result.Sessions)
	}
	if result.Sessions[0].Agent != "codex" || result.Sessions[0].QualifiedKey() == "" {
		t.Fatalf("session identity = %#v", result.Sessions[0])
	}
	if !result.Sessions[0].CreatedAt.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) || !result.Sessions[0].UpdatedAt.Equal(time.Date(2026, 1, 1, 0, 0, 5, 0, time.UTC)) {
		t.Fatalf("session time range = %v to %v", result.Sessions[0].CreatedAt, result.Sessions[0].UpdatedAt)
	}
}

func TestLoadCapturesTurnContextModel(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "2026", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"task_started","payload":{"turn_id":"t"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"turn_context","payload":{"turn_id":"t","model":"gpt-example"}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"task_complete","payload":{"turn_id":"t"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || len(result.Turns[0].ModelObservations) != 1 {
		t.Fatalf("turn model observations = %#v", result.Turns)
	}
	want := usage.NewModelRef("codex", "gpt-example")
	if got := result.Turns[0].ModelObservations[0].Model; got != want {
		t.Fatalf("model = %#v, want %#v", got, want)
	}
}

func TestLoadCapturesSessionTitleWhenPresent(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "2026", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s","title":"Implement usage explorer"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"task_started","payload":{"turn_id":"t"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"task_complete","payload":{"turn_id":"t"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sessions) != 1 || result.Sessions[0].Title != "Implement usage explorer" {
		t.Fatalf("session metadata = %#v", result.Sessions)
	}
}

func TestLoadUsesLatestSessionIndexThreadName(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "2026", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"type":"session_meta","payload":{"id":"s"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(home, "session_index.jsonl")
	if err := os.WriteFile(indexPath, []byte(strings.Join([]string{
		`{"id":"s","thread_name":"initial name"}`,
		`not-json`,
		`{"id":"s","thread_name":"latest name"}`,
	}, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cacheDir := t.TempDir()
	result, err := Load(home, IngestOptions{CacheDir: cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sessions) != 1 || result.Sessions[0].Title != "latest name" {
		t.Fatalf("session index title = %#v", result.Sessions)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Reason != "malformed_session_index" {
		t.Fatalf("session index warnings = %#v", result.Warnings)
	}

	if err := os.WriteFile(indexPath, []byte(`{"id":"s","thread_name":"renamed session"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err = Load(home, IngestOptions{CacheDir: cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sessions) != 1 || result.Sessions[0].Title != "renamed session" {
		t.Fatalf("cached session index title = %#v", result.Sessions)
	}
}

func TestLoadAggregatesLastTokenUsagePerTurn(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"user_message","payload":{"text":"hello"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":70,"cache_write_input_tokens":2,"output_tokens":20,"reasoning_output_tokens":8,"total_tokens":120},"last_token_usage":{"input_tokens":10,"cached_input_tokens":7,"cache_write_input_tokens":1,"output_tokens":2,"reasoning_output_tokens":1,"total_tokens":12}}}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":160,"cached_input_tokens":110,"cache_write_input_tokens":4,"output_tokens":30,"reasoning_output_tokens":10,"total_tokens":190},"last_token_usage":{"input_tokens":20,"cached_input_tokens":11,"cache_write_input_tokens":3,"output_tokens":4,"reasoning_output_tokens":2,"total_tokens":24}}}}`,
		`{"timestamp":"2026-01-01T00:00:04Z","type":"task_complete","payload":{}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || result.Turns[0].TokenUsage == nil {
		t.Fatalf("turns = %#v", result.Turns)
	}
	got := *result.Turns[0].TokenUsage
	want := usage.TokenUsage{InputTokens: 30, CachedInputTokens: 18, CacheWriteInputTokens: 4, OutputTokens: 6, ReasoningOutputTokens: 3, TotalTokens: 36}
	if got != want {
		t.Fatalf("token usage = %#v, want %#v", got, want)
	}
}

func TestLoadDifferencesCumulativeTokenUsageWhenLastUsageIsUnavailable(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"user_message","payload":{"text":"hello"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":30,"output_tokens":5,"total_tokens":35}}}}`,
		`{"timestamp":"2026-01-01T00:00:04Z","type":"task_complete","payload":{}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || result.Turns[0].TokenUsage == nil {
		t.Fatalf("turns = %#v", result.Turns)
	}
	want := usage.TokenUsage{InputTokens: 30, OutputTokens: 5, TotalTokens: 35}
	if got := *result.Turns[0].TokenUsage; got != want {
		t.Fatalf("cumulative token usage = %#v, want %#v", got, want)
	}
}

func TestLoadAggregatesTokenUsageRecordWithoutDoubleCountingTokenCount(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"task_started","payload":{"turn_id":"t"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"user_message","payload":{"turn_id":"t","text":"hello"}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"token_usage_record","payload":{"session_id":"s","turn_id":"t","response_id":"r1","usage":{"input_tokens":10,"cached_input_tokens":7,"output_tokens":2,"reasoning_output_tokens":1,"total_tokens":12},"turn_token_usage":{"input_tokens":999,"cached_input_tokens":888,"output_tokens":777,"reasoning_output_tokens":666,"total_tokens":1776}}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"token_usage_record","payload":{"session_id":"s","turn_id":"t","response_id":"r1","usage":{"input_tokens":10,"cached_input_tokens":7,"output_tokens":2,"reasoning_output_tokens":1,"total_tokens":12},"turn_token_usage":{"input_tokens":999,"cached_input_tokens":888,"output_tokens":777,"reasoning_output_tokens":666,"total_tokens":1776}}}`,
		`{"timestamp":"2026-01-01T00:00:04Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":20,"cached_input_tokens":11,"output_tokens":4,"reasoning_output_tokens":2,"total_tokens":24}}}}`,
		`{"timestamp":"2026-01-01T00:00:05Z","type":"task_complete","payload":{"turn_id":"t"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || result.Turns[0].TokenUsage == nil {
		t.Fatalf("turns = %#v", result.Turns)
	}
	want := usage.TokenUsage{InputTokens: 10, CachedInputTokens: 7, OutputTokens: 2, ReasoningOutputTokens: 1, TotalTokens: 12}
	if got := *result.Turns[0].TokenUsage; got != want {
		t.Fatalf("token usage = %#v, want %#v", got, want)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

func TestLoadFallsBackFromInvalidLastTokenUsageToCumulativeUsage(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"task_started","payload":{"turn_id":"t"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":30,"output_tokens":5,"total_tokens":35},"last_token_usage":{"input_tokens":0,"output_tokens":0,"total_tokens":35}}}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"task_complete","payload":{"turn_id":"t"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	want := usage.TokenUsage{InputTokens: 30, OutputTokens: 5, TotalTokens: 35}
	if len(result.Turns) != 1 || result.Turns[0].TokenUsage == nil || *result.Turns[0].TokenUsage != want {
		t.Fatalf("token usage = %#v, want %#v", result.Turns, want)
	}
}

func TestLoadDoesNotCreateTurnForThreadSettingsApplied(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"task_started","payload":{"turn_id":"t1"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"turn_context","payload":{"turn_id":"t1","model":"gpt-example"}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"task_complete","payload":{"turn_id":"t1"}}`,
		`{"timestamp":"2026-01-01T00:00:04Z","type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"model":"gpt-example","model_provider_id":"openai"}}}`,
		`{"timestamp":"2026-01-01T00:00:05Z","type":"task_started","payload":{"turn_id":"t2"}}`,
		`{"timestamp":"2026-01-01T00:00:06Z","type":"turn_context","payload":{"turn_id":"t2","model":"gpt-example"}}`,
		`{"timestamp":"2026-01-01T00:00:07Z","type":"task_complete","payload":{"turn_id":"t2"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 2 || result.Turns[0].ID != "t1" || result.Turns[1].ID != "t2" {
		t.Fatalf("turns = %#v", result.Turns)
	}
}

func TestLoadInheritsTurnModelForTokenCount(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"task_started","payload":{"turn_id":"t"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"turn_context","payload":{"turn_id":"t","model":"gpt-example"}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}}`,
		`{"timestamp":"2026-01-01T00:00:04Z","type":"task_complete","payload":{"turn_id":"t"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	want := usage.NewModelRef("codex", "gpt-example")
	if len(result.Turns) != 1 || len(result.Turns[0].TokenUsageEvents) != 1 || result.Turns[0].TokenUsageEvents[0].Model != want {
		t.Fatalf("token usage events = %#v, want model %#v", result.Turns, want)
	}
}

func TestLoadUsesThreadSettingsModelForTokenCountWithoutTurnContext(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"model":"gpt-example","model_provider_id":"openai"}}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"task_started","payload":{"turn_id":"t"}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}}`,
		`{"timestamp":"2026-01-01T00:00:04Z","type":"task_complete","payload":{"turn_id":"t"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	want := usage.NewModelRef("openai", "gpt-example")
	if len(result.Turns) != 1 || len(result.Turns[0].TokenUsageEvents) != 1 || result.Turns[0].TokenUsageEvents[0].Model != want {
		t.Fatalf("token usage events = %#v, want model %#v", result.Turns, want)
	}
}

func TestLoadDoesNotUseHistoryPathAsMissingSessionID(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"user_message","payload":{"text":"hello"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"task_complete","payload":{}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Load(home, IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sessions) != 1 || result.Sessions[0].ID == path || strings.Contains(result.Sessions[0].ID, path) {
		t.Fatalf("missing session ID exposed history path: %#v", result.Sessions)
	}
	snapshot, _, complete := parseFileSnapshot(path, IngestOptions{})
	if !complete {
		t.Fatal("snapshot parse was incomplete")
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), path) {
		t.Fatalf("cache snapshot exposed history path: %s", data)
	}
}

func TestFilterCodexTurnKeepsInRangeObservation(t *testing.T) {
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	current := old.Add(24 * time.Hour)
	filter, err := NewDateRangeFilter(current, current.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	turn := usage.NewTurn("s", "t", 1, usage.NewCodexSourceRef("history", 1, ""))
	turn.StartedAt = old
	turn.EndedAt = old.Add(time.Minute)
	turn.TokenUsageEvents = []usage.TokenUsageEvent{{Timestamp: current, Usage: usage.TokenUsage{TotalTokens: 7}}}
	filtered, ok := filterCodexTurn(turn, filter)
	if !ok || filtered.TokenUsage == nil || filtered.TokenUsage.TotalTokens != 7 {
		t.Fatalf("in-range observation was dropped: ok=%v turn=%#v", ok, filtered)
	}
}

func TestTimestampFilterIsCutoffInclusiveAndRejectsNegative(t *testing.T) {
	now := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	filter, err := NewTimestampFilter(2, now)
	if err != nil {
		t.Fatal(err)
	}
	if !filter.Accept(now.Add(-48*time.Hour)) || filter.Accept(now.Add(-48*time.Hour-time.Nanosecond)) {
		t.Fatal("cutoff is not inclusive")
	}
	if _, err := NewTimestampFilter(-1, now); err == nil {
		t.Fatal("negative days should fail")
	}
	if _, err := NewTimestampFilter(0, now); err == nil {
		t.Fatal("zero days should fail when a filter is requested")
	}
}

func TestLoadFiltersByDateRange(t *testing.T) {
	home := t.TempDir()
	sessionsDir := filepath.Join(home, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSession := func(name, id, timestamp string) {
		t.Helper()
		lines := []string{
			`{"timestamp":"` + timestamp + `","type":"session_meta","payload":{"id":"` + id + `"}}`,
			`{"timestamp":"` + timestamp + `","type":"user_message","payload":{"text":"hello"}}`,
			`{"timestamp":"` + timestamp + `","type":"task_complete","payload":{}}`,
		}
		if err := os.WriteFile(filepath.Join(sessionsDir, name), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeSession("old.jsonl", "old", "2026-01-01T12:00:00Z")
	writeSession("selected.jsonl", "selected", "2026-01-02T12:00:00Z")
	writeSession("new.jsonl", "new", "2026-01-03T00:00:00Z")

	result, err := Load(home, IngestOptions{
		From: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC),
		Now:  time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || result.Turns[0].SessionID != "selected" || len(result.Sessions) != 1 || result.Sessions[0].ID != "selected" {
		t.Fatalf("date range result = %#v", result)
	}

	toOnly, err := Load(home, IngestOptions{
		To:  time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC),
		Now: time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(toOnly.Turns) != 2 || len(toOnly.Sessions) != 2 {
		t.Fatalf("upper-only date range result = %#v", toOnly)
	}

	fromOnly, err := Load(home, IngestOptions{
		From: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		Now:  time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fromOnly.Turns) != 2 || len(fromOnly.Sessions) != 2 {
		t.Fatalf("lower-only date range result = %#v", fromOnly)
	}
}

func TestLoadAttachesModelRuntimeAndSkillEvidence(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s","cli_version":"9"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"user_message","payload":{"text":"$report"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"response_item","payload":{"type":"function_call","name":"exec","call_id":"c1","arguments":"{\"cmd\":\"cat /x/skills/report/SKILL.md\"}"}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"event_msg","payload":{"type":"ItemCompleted","item":{"type":"CommandExecution","id":"i1","status":"completed","command":"cat /x/skills/report/SKILL.md"}}}`,
		`{"timestamp":"2026-01-01T00:00:04Z","type":"task_complete","payload":{}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 {
		t.Fatalf("turns = %#v", result.Turns)
	}
	turn := result.Turns[0]
	if len(turn.ModelTools) != 1 || turn.ModelTools[0].CallID != "c1" || turn.ModelTools[0].Source.CLIVersion != "9" {
		t.Fatalf("model tools = %#v", turn.ModelTools)
	}
	if len(turn.RuntimeTools) != 1 || turn.RuntimeTools[0].CanonicalName != "shell" || turn.RuntimeTools[0].Source.CLIVersion != "9" {
		t.Fatalf("runtime tools = %#v", turn.RuntimeTools)
	}
	if len(turn.SkillEvidence) < 2 {
		t.Fatalf("skill evidence = %#v", turn.SkillEvidence)
	}
}

func TestLoadRecognizesSnakeCaseItemCompletedAndImplicitSkillAccess(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"task_started","payload":{"turn_id":"t"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution","id":"i1","status":"completed","command":["/bin/zsh","-lc","cat /fixture-home/.agents/skills/report/SKILL.md"]}}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"task_complete","payload":{"turn_id":"t"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || len(result.Turns[0].RuntimeTools) != 1 {
		t.Fatalf("runtime tools = %#v", result.Turns)
	}
	var found bool
	for _, evidence := range result.Turns[0].SkillEvidence {
		if evidence.SkillName == "report" && evidence.Mode == usage.ModeImplicit && evidence.Method == usage.MethodImplicitAccess {
			found = true
		}
	}
	if !found {
		t.Fatalf("implicit evidence = %#v", result.Turns[0].SkillEvidence)
	}
}

func TestLoadDoesNotInferSkillFromFailedRuntimeAccess(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"task_started","payload":{"turn_id":"t"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution","id":"i1","status":"failed","command":"cat /fixture-home/.agents/skills/report/SKILL.md"}}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"task_complete","payload":{"turn_id":"t"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	for _, evidence := range result.Turns[0].SkillEvidence {
		if evidence.Method == usage.MethodImplicitAccess {
			t.Fatalf("failed runtime access should not infer skill evidence: %#v", result.Turns[0].SkillEvidence)
		}
	}
}

func TestSelectedSkillInjectionWithoutExplicitRequestIsUnknown(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"task_started","payload":{"turn_id":"t"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<skill>\n<name>report</name>\n---\nname: report\n---\nbody\n</skill>"}],"internal_chat_message_metadata_passthrough":{"content_item_kinds":["skills.selected_skill_instructions"]}}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"task_complete","payload":{"turn_id":"t"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || len(result.Turns[0].SkillEvidence) != 1 {
		t.Fatalf("skill evidence = %#v", result.Turns)
	}
	evidence := result.Turns[0].SkillEvidence[0]
	if evidence.SkillName != "report" || evidence.Mode != usage.ModeUnknown || evidence.Method != usage.MethodSelectedSkillInstructions || evidence.State != usage.StateConfirmed {
		t.Fatalf("selected evidence = %#v", evidence)
	}
}

func TestSelectedSkillInjectionStaysInPromptTurn(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"$report"}],"internal_chat_message_metadata_passthrough":{"turn_id":"t","content_item_kinds":["user.text"]}}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"event_msg","payload":{"type":"item_completed","turn_id":"t","item":{"type":"UserMessage","content":[{"type":"text","text":"$report"},{"type":"skill","name":"report","path":"/fixture-home/.agents/skills/report/SKILL.md"}]}}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<skill><name>report</name><path>/fixture-home/.agents/skills/report/SKILL.md</path></skill>"}],"internal_chat_message_metadata_passthrough":{"turn_id":"t","content_item_kinds":["skills.selected_skill_instructions"]}}}`,
		`{"timestamp":"2026-01-01T00:00:04Z","type":"task_complete","payload":{"turn_id":"t"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || result.Turns[0].ID != "t" || result.Turns[0].UserPrompts != 1 {
		t.Fatalf("turns = %#v", result.Turns)
	}
	uses := usage.MergeSkillEvidence(result.Turns[0].SkillEvidence)
	if len(uses) != 1 || !uses[0].HasMode(usage.ModeExplicit) || uses[0].HasMode(usage.ModeUnknown) || uses[0].State != usage.StateConfirmed {
		t.Fatalf("merged skill uses = %#v", uses)
	}
}

func TestResponseItemUserMessageAndInjectedSkillAreNotPrompt(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s"}}`,
		`{"timestamp":"2026-01-01T00:00:00Z","type":"task_started","payload":{"turn_id":"injected"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<skill name=\"report\">body</skill>"}]}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"task_complete","payload":{"turn_id":"injected"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"task_started","payload":{"turn_id":"actual"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"task_complete","payload":{"turn_id":"actual"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 2 || result.Turns[0].UserPrompts != 0 || result.Turns[1].UserPrompts != 1 {
		t.Fatalf("turns = %#v", result.Turns)
	}
}

func TestInjectedSkillWithUserTextStillCountsPrompt(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"user_message","payload":{"text":"<skill name=\"report\">body</skill>\nplease summarize"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"task_complete","payload":{}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || result.Turns[0].UserPrompts != 1 {
		t.Fatalf("turns = %#v", result.Turns)
	}
}

func TestItemIDsDoNotCreateExtraTurns(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"task_started","payload":{"turn_id":"t"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"response_item","payload":{"type":"function_call","id":"item-id","name":"exec","call_id":"call-id"}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"event_msg","payload":{"type":"ItemCompleted","item":{"type":"CommandExecution","id":"runtime-id","command":"true"}}}`,
		`{"timestamp":"2026-01-01T00:00:04Z","type":"task_complete","payload":{"turn_id":"t"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || len(result.Turns[0].ModelTools) != 1 || len(result.Turns[0].RuntimeTools) != 1 {
		t.Fatalf("turns = %#v", result.Turns)
	}
}
