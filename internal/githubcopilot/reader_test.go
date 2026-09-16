package githubcopilot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xkumiyu/catsift/internal/usage"
)

func TestDecodeFileKeepsMetadataAndContinuesAfterRecoverableLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-state", "session-001", "events.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := strings.Join([]string{
		`{"id":"event-001","parentId":"parent-001","timestamp":"2026-01-02T03:04:05Z","type":"session.start","data":{"version":"0.1.0"}}`,
		`not-json`,
		`{"id":"event-003","parentId":"event-001","type":"assistant.turn_start","data":{"turnId":"turn-001"}}`,
		`{"id":"event-004","timestamp":"2026-01-02T03:04:08Z","type":"future.event","data":{}}`,
		`{"id":"event-005","parentId":"event-003","timestamp":"2026-01-02T03:04:09Z","type":"user.message","data":{"content":"synthetic prompt"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}

	var events []Envelope
	warnings := &WarningCollector{}
	if err := DecodeFile(path, DecodeOptions{Warnings: warnings}, func(event Envelope) { events = append(events, event) }); err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("decoded events = %#v", events)
	}
	first := events[0]
	if first.ID != "event-001" || first.ParentID != "parent-001" || first.SessionID != "session-001" || first.Source.Path != path || first.Source.Line != 1 || first.Source.Source != usage.SourceCopilot || first.Source.Agent != "copilot" || first.Source.ProviderSessionID != "session-001" {
		t.Fatalf("event metadata = %#v", first)
	}
	if !first.Timestamp.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) || first.Type != "session.start" || first.Data["version"] != "0.1.0" {
		t.Fatalf("event payload = %#v", first)
	}
	if !events[1].Timestamp.IsZero() || events[2].ID != "event-005" {
		t.Fatalf("later events = %#v", events)
	}

	got := warnings.Warnings()
	if len(got) != 3 || got[0].Reason != "malformed_json" || got[1].Reason != "missing_timestamp" || got[2].Reason != "unknown_type" {
		t.Fatalf("warnings = %#v", got)
	}
}

func TestDecodeFileSkipsOversizedLineAndReadsNextEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-state", "session-001", "events.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := strings.Repeat("x", 512) + "\n" + `{"id":"event-002","timestamp":"2026-01-02T03:04:05Z","type":"session.shutdown","data":{}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
	var events []Envelope
	warnings := &WarningCollector{}
	if err := DecodeFile(path, DecodeOptions{MaxLineBytes: 256, Warnings: warnings}, func(event Envelope) { events = append(events, event) }); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != "event-002" {
		t.Fatalf("events after oversized line = %#v", events)
	}
	if got := warnings.Warnings(); len(got) != 1 || got[0].Reason != "large_line" || got[0].Line != 1 {
		t.Fatalf("oversized warning = %#v", got)
	}
}

func TestDecodeFileAcceptsSkillInvokedEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-state", "session-001", "events.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"id":"event-001","timestamp":"2026-01-02T03:04:05Z","type":"skill.invoked","data":{"name":"review","path":"/private/.agents/skills/review/SKILL.md","content":"synthetic"}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
	var events []Envelope
	warnings := &WarningCollector{}
	if err := DecodeFile(path, DecodeOptions{Warnings: warnings}, func(event Envelope) { events = append(events, event) }); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != "skill.invoked" || len(warnings.Warnings()) != 0 {
		t.Fatalf("skill event = %#v warnings = %#v", events, warnings.Warnings())
	}
}

func TestDecodeFileAcceptsSessionContextChangedEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-state", "session-001", "events.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"id":"event-001","timestamp":"2026-01-02T03:04:05Z","type":"session.context_changed","data":{"cwd":"/workspace/project","gitRoot":"/workspace/project","repository":"owner/project"}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
	var events []Envelope
	warnings := &WarningCollector{}
	if err := DecodeFile(path, DecodeOptions{Warnings: warnings}, func(event Envelope) { events = append(events, event) }); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != "session.context_changed" || len(warnings.Warnings()) != 0 {
		t.Fatalf("context event = %#v warnings = %#v", events, warnings.Warnings())
	}
}

func TestDecodeFileAcceptsAssistantUsageEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-state", "session-001", "events.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"id":"event-001","timestamp":"2026-01-02T03:04:05Z","type":"assistant.usage","data":{"usage":{"inputTokens":1,"outputTokens":2}}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
	var events []Envelope
	warnings := &WarningCollector{}
	if err := DecodeFile(path, DecodeOptions{Warnings: warnings}, func(event Envelope) { events = append(events, event) }); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != "assistant.usage" || len(warnings.Warnings()) != 0 {
		t.Fatalf("assistant usage event = %#v warnings = %#v", events, warnings.Warnings())
	}
}

func TestDecodeFixtureIsReadOnly(t *testing.T) {
	path := filepath.Join("testdata", "normal.jsonl")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := DecodeFile(path, DecodeOptions{}, func(Envelope) {}); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if before.Size() != after.Size() || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("fixture metadata changed: before=%v after=%v", before, after)
	}
}
