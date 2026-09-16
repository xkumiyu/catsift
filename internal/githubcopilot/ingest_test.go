package githubcopilot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xkumiyu/catsift/internal/cache"
)

func TestLoadCachesFullSnapshotAndAppliesPeriodFilter(t *testing.T) {
	root := t.TempDir()
	writeCopilotFixture(t, root, "session-a", true)
	now := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
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
	if !reflect.DeepEqual(stripSourcePaths(fresh), stripSourcePaths(cold)) || !reflect.DeepEqual(stripSourcePaths(cold), stripSourcePaths(warm)) {
		t.Fatalf("fresh/cold/warm differ: fresh=%#v cold=%#v warm=%#v", fresh, cold, warm)
	}
	if len(warm.Sessions) != 1 || warm.Sessions[0].Title != "Session session-a" || warm.Sessions[0].ProjectName != "owner/session-a" || warm.Sessions[0].ProjectPath != "/workspace/session-a" {
		t.Fatalf("session title = %#v", warm.Sessions)
	}
	if len(warm.Turns) != 2 || len(warm.Turns[0].SkillEvidence) != 1 || warm.Turns[0].SkillEvidence[0].SkillName != "review" {
		t.Fatalf("skill evidence = %#v", warm.Turns)
	}
	cachePath := cache.New(cacheDir).Path(stringSource(), mustAbs(t, root))
	serialized, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"synthetic prompt", "secret-token", `"arguments"`, "tool payload"} {
		if strings.Contains(string(serialized), forbidden) {
			t.Fatalf("cache contains raw history %q: %s", forbidden, serialized)
		}
	}
	files, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := sourceRevision(mustAbs(t, root), files)
	if err != nil {
		t.Fatal(err)
	}
	if _, hit, err := cache.New(cacheDir).Read(stringSource(), mustAbs(t, root), revision, "copilot-normalizer-old"); err != nil || hit {
		t.Fatalf("old parser cache hit = %v, err = %v", hit, err)
	}

	from := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	filtered, err := Load(root, IngestOptions{From: from, To: to, Now: now, CacheDir: cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Turns) != 1 || filtered.Turns[0].UserPrompts != 1 || len(filtered.Sessions) != 1 {
		t.Fatalf("filtered result = %#v", filtered)
	}
}

func TestLoadCachesCopilotShutdownTokenMetrics(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "session-state", "session-a", "events.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := strings.Join([]string{
		`{"id":"turn-start","timestamp":"2026-01-01T00:00:01Z","type":"model.turn_started","data":{"turnId":"turn-1","model":"model-a"}}`,
		`{"id":"turn-end","timestamp":"2026-01-01T00:00:02Z","type":"model.turn_ended","data":{"turnId":"turn-1"}}`,
		`{"id":"shutdown","timestamp":"2026-01-01T00:00:03Z","type":"session.shutdown","data":{"modelMetrics":{"model-a":{"usage":{"inputTokens":100,"outputTokens":20,"cacheReadTokens":40,"cacheWriteTokens":3,"reasoningTokens":5}}}}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}

	cacheDir := t.TempDir()
	fresh, err := Load(root, IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cold, err := Load(root, IngestOptions{CacheDir: cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	warm, err := Load(root, IngestOptions{CacheDir: cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	for name, result := range map[string]IngestResult{"fresh": fresh, "cold": cold, "warm": warm} {
		if len(result.Turns) != 1 || len(result.Turns[0].TokenUsageEvents) != 1 {
			t.Fatalf("%s token metrics = %#v", name, result.Turns)
		}
		value := result.Turns[0].TokenUsageEvents[0].Usage
		if value.TotalTokens != 120 || value.CachedInputTokens != 40 || value.CacheWriteInputTokens != 3 || value.ReasoningOutputTokens != 5 {
			t.Fatalf("%s token usage = %#v", name, value)
		}
	}
	serialized, err := json.Marshal(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(serialized), "modelMetrics") || !strings.Contains(string(serialized), `"cached_input_tokens":40`) || !strings.Contains(string(serialized), `"reasoning_output_tokens":5`) {
		t.Fatalf("normalized token JSON = %s", serialized)
	}
	cachePath := cache.New(cacheDir).Path(stringSource(), mustAbs(t, root))
	cached, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cached), "modelMetrics") || !strings.Contains(string(cached), `"cached_input_tokens":40`) || !strings.Contains(string(cached), `"reasoning_output_tokens":5`) {
		t.Fatalf("cached token JSON = %s", cached)
	}
	files, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := sourceRevision(mustAbs(t, root), files)
	if err != nil {
		t.Fatal(err)
	}
	if _, hit, err := cache.New(cacheDir).Read(stringSource(), mustAbs(t, root), revision, "copilot-normalizer-old"); err != nil || hit {
		t.Fatalf("old parser cache hit = %v, err = %v", hit, err)
	}
}

func TestLoadInvalidatesCacheWhenSessionFilesChange(t *testing.T) {
	root := t.TempDir()
	writeCopilotFixture(t, root, "session-a", false)
	cacheDir := t.TempDir()
	if _, err := Load(root, IngestOptions{CacheDir: cacheDir}); err != nil {
		t.Fatal(err)
	}
	writeCopilotFixture(t, root, "session-b", false)
	var diagnostics []string
	changed, err := Load(root, IngestOptions{CacheDir: cacheDir, Diagnostic: func(message string) { diagnostics = append(diagnostics, message) }})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed.Sessions) != 2 || !containsDiagnostic(diagnostics, "cache miss; reading source") {
		t.Fatalf("added session did not invalidate cache: sessions=%#v diagnostics=%v", changed.Sessions, diagnostics)
	}

	path := filepath.Join(root, "session-state", "session-a", "events.jsonl")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	changedAt := info.ModTime().Add(2 * time.Second)
	if err := os.Chtimes(path, changedAt, changedAt); err != nil {
		t.Fatal(err)
	}
	diagnostics = nil
	if _, err := Load(root, IngestOptions{CacheDir: cacheDir, Diagnostic: func(message string) { diagnostics = append(diagnostics, message) }}); err != nil {
		t.Fatal(err)
	}
	if !containsDiagnostic(diagnostics, "cache miss; reading source") {
		t.Fatalf("mtime change did not invalidate cache: %v", diagnostics)
	}

	if err := os.Remove(filepath.Join(root, "session-state", "session-b", "events.jsonl")); err != nil {
		t.Fatal(err)
	}
	diagnostics = nil
	deleted, err := Load(root, IngestOptions{CacheDir: cacheDir, Diagnostic: func(message string) { diagnostics = append(diagnostics, message) }})
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted.Sessions) != 1 || !containsDiagnostic(diagnostics, "cache miss; reading source") {
		t.Fatalf("deleted session did not invalidate cache: sessions=%#v diagnostics=%v", deleted.Sessions, diagnostics)
	}
}

func TestLoadInvalidatesCacheWhenWorkspaceMetadataChanges(t *testing.T) {
	root := t.TempDir()
	writeCopilotFixture(t, root, "session-a", false)
	cacheDir := t.TempDir()
	if _, err := Load(root, IngestOptions{CacheDir: cacheDir}); err != nil {
		t.Fatal(err)
	}
	metadataPath := filepath.Join(root, "session-state", "session-a", "workspace.yaml")
	if err := os.WriteFile(metadataPath, []byte("name: 'Renamed session'\nuser_named: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var diagnostics []string
	changed, err := Load(root, IngestOptions{CacheDir: cacheDir, Diagnostic: func(message string) { diagnostics = append(diagnostics, message) }})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed.Sessions) != 1 || changed.Sessions[0].Title != "Renamed session" || !containsDiagnostic(diagnostics, "cache miss; reading source") {
		t.Fatalf("workspace metadata change was not applied: sessions=%#v diagnostics=%v", changed.Sessions, diagnostics)
	}
}

func TestLoadContinuesWhenWorkspaceMetadataIsInvalid(t *testing.T) {
	root := t.TempDir()
	writeCopilotFixture(t, root, "session-a", false)
	metadataPath := filepath.Join(root, "session-state", "session-a", "workspace.yaml")
	if err := os.WriteFile(metadataPath, []byte("name: 'unterminated\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Load(root, IngestOptions{})
	if err != nil || len(result.Sessions) != 1 || result.Sessions[0].Title != "" {
		t.Fatalf("invalid metadata result = %#v, err=%v", result, err)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Reason != "read_workspace" {
		t.Fatalf("metadata warning = %#v", result.Warnings)
	}
}

func TestLoadUsesWorkspaceProjectMetadataWithoutContextEvent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "session-state", "session-a", "events.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"id":"start","timestamp":"2026-01-01T00:00:00Z","type":"session.start","data":{"version":"1.0"}}`+"\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	metadataPath := filepath.Join(filepath.Dir(path), "workspace.yaml")
	if err := os.WriteFile(metadataPath, []byte("repository: owner/project\ngit_root: /workspace/project\ncwd: /workspace/project/subdir\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := Load(root, IngestOptions{})
	if err != nil || len(result.Sessions) != 1 {
		t.Fatalf("metadata result = %#v, err=%v", result, err)
	}
	if result.Sessions[0].ProjectName != "owner/project" || result.Sessions[0].ProjectPath != "/workspace/project/subdir" {
		t.Fatalf("workspace project metadata = %#v", result.Sessions[0])
	}
}

func TestLoadSkipsCacheWriteWhenSourceChangesDuringRead(t *testing.T) {
	root := t.TempDir()
	writeCopilotFixture(t, root, "session-a", false)
	cacheDir := t.TempDir()
	path := filepath.Join(root, "session-state", "session-a", "events.jsonl")
	changed := false
	var diagnostics []string
	if _, err := Load(root, IngestOptions{CacheDir: cacheDir, Diagnostic: func(message string) {
		diagnostics = append(diagnostics, message)
		if !changed && strings.Contains(message, "source: reading path=") {
			changed = true
			info, statErr := os.Stat(path)
			if statErr != nil {
				t.Fatalf("stat source during read: %v", statErr)
			}
			when := info.ModTime().Add(3 * time.Second)
			if err := os.Chtimes(path, when, when); err != nil {
				t.Fatalf("change source revision: %v", err)
			}
		}
	}}); err != nil {
		t.Fatal(err)
	}
	if !changed || !containsDiagnostic(diagnostics, "cache skipped; source revision changed") {
		t.Fatalf("revision change was not reported: %v", diagnostics)
	}
	files, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := sourceRevision(mustAbs(t, root), files)
	if err != nil {
		t.Fatal(err)
	}
	if _, hit, err := cache.New(cacheDir).Read(stringSource(), mustAbs(t, root), revision, ParserVersion); err != nil || hit {
		t.Fatalf("inconsistent source was cached: hit=%v err=%v", hit, err)
	}
}

func TestLoadTreatsDiagnosticOnlyRootAsEmpty(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "session-store.db"), []byte("diagnostic-only"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Load(root, IngestOptions{})
	if err != nil || len(result.Turns) != 0 || len(result.Sessions) != 0 || len(result.Agents) != 1 || result.Agents[0] != "copilot" {
		t.Fatalf("empty result = %#v, err=%v", result, err)
	}
}

func TestLoadRejectsUnavailableRoot(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing"), IngestOptions{}); err == nil {
		t.Fatal("expected unavailable source error")
	}
}

func writeCopilotFixture(t *testing.T, root, sessionID string, twoTurns bool) {
	t.Helper()
	path := filepath.Join(root, "session-state", sessionID, "events.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"id":"start","timestamp":"2026-01-01T00:00:00Z","type":"session.start","data":{"version":"1.0"}}`,
		`{"id":"context","timestamp":"2026-01-01T00:00:00.500Z","type":"session.context_changed","data":{"cwd":"/workspace/` + sessionID + `","gitRoot":"/workspace/` + sessionID + `","repository":"owner/` + sessionID + `"}}`,
		`{"id":"turn-start-1","timestamp":"2026-01-01T00:00:01Z","type":"model.turn_started","data":{"turnId":"turn-1","model":"model-a"}}`,
		`{"id":"prompt-1","timestamp":"2026-01-01T00:00:02Z","type":"user.message","data":{"role":"user","content":"synthetic prompt"}}`,
		`{"id":"skill-1","timestamp":"2026-01-01T00:00:02.500Z","type":"skill.invoked","data":{"name":"review","path":"/private/.agents/skills/review/SKILL.md","content":"synthetic skill instructions"}}`,
		`{"id":"tool-start","timestamp":"2026-01-01T00:00:03Z","type":"tool.execution_start","data":{"turnId":"turn-1","toolCallId":"call-1","toolName":"shell","arguments":{"command":"secret-token"}}}`,
		`{"id":"tool-end","timestamp":"2026-01-01T00:00:04Z","type":"tool.execution_complete","data":{"turnId":"turn-1","toolCallId":"call-1","toolName":"shell","status":"success","error":"tool payload"}}`,
		`{"id":"response-1","timestamp":"2026-01-01T00:00:05Z","type":"model.response","data":{"turnId":"turn-1","callId":"model-call-1","model":"model-a","usage":{"inputTokens":10,"outputTokens":4,"totalTokens":14}}}`,
		`{"id":"turn-end-1","timestamp":"2026-01-01T00:00:06Z","type":"model.turn_ended","data":{"turnId":"turn-1"}}`,
	}
	if twoTurns {
		lines = append(lines,
			`{"id":"turn-start-2","timestamp":"2026-01-02T00:00:01Z","type":"model.turn_started","data":{"turnId":"turn-2","model":"model-b"}}`,
			`{"id":"prompt-2","timestamp":"2026-01-02T00:00:02Z","type":"user.message","data":{"role":"user","content":"second synthetic prompt"}}`,
			`{"id":"turn-end-2","timestamp":"2026-01-02T00:00:03Z","type":"model.turn_ended","data":{"turnId":"turn-2"}}`,
		)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	metadata := filepath.Join(filepath.Dir(path), "workspace.yaml")
	if err := os.WriteFile(metadata, []byte("name: 'Session "+sessionID+"'\nuser_named: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func stripSourcePaths(result IngestResult) IngestResult {
	data, err := json.Marshal(result)
	if err != nil {
		return result
	}
	var copy IngestResult
	if err := json.Unmarshal(data, &copy); err != nil {
		return result
	}
	for i := range copy.Turns {
		copy.Turns[i].Source.Path = ""
		copy.Turns[i].Source.Line = 0
		for j := range copy.Turns[i].ModelObservations {
			copy.Turns[i].ModelObservations[j].Source.Path = ""
			copy.Turns[i].ModelObservations[j].Source.Line = 0
		}
		for j := range copy.Turns[i].ModelTools {
			copy.Turns[i].ModelTools[j].Source.Path = ""
			copy.Turns[i].ModelTools[j].Source.Line = 0
		}
		for j := range copy.Turns[i].RuntimeTools {
			copy.Turns[i].RuntimeTools[j].Source.Path = ""
			copy.Turns[i].RuntimeTools[j].Source.Line = 0
		}
		for j := range copy.Turns[i].SkillEvidence {
			copy.Turns[i].SkillEvidence[j].Source.Path = ""
			copy.Turns[i].SkillEvidence[j].Source.Line = 0
		}
		if len(copy.Turns[i].ModelTools) == 0 {
			copy.Turns[i].ModelTools = nil
		}
		if len(copy.Turns[i].RuntimeTools) == 0 {
			copy.Turns[i].RuntimeTools = nil
		}
		if len(copy.Turns[i].ModelObservations) == 0 {
			copy.Turns[i].ModelObservations = nil
		}
		if len(copy.Turns[i].SkillEvidence) == 0 {
			copy.Turns[i].SkillEvidence = nil
		}
		if len(copy.Turns[i].TokenUsageEvents) == 0 {
			copy.Turns[i].TokenUsageEvents = nil
		}
	}
	for i := range copy.Sessions {
		copy.Sessions[i].Source.Path = ""
		copy.Sessions[i].Source.Line = 0
	}
	if len(copy.Warnings) == 0 {
		copy.Warnings = nil
	}
	return copy
}

func containsDiagnostic(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}

func mustAbs(t *testing.T, path string) string {
	t.Helper()
	value, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func stringSource() string { return string("copilot") }
