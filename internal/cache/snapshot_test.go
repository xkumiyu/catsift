package cache

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/xkumiyu/catsift/internal/usage"
)

func TestTurnDTOOmitsPromptPayloadToolArgumentsAndRawSourcePosition(t *testing.T) {
	source := usage.SourceRef{
		Path:       "/private/history.jsonl",
		Line:       42,
		Source:     usage.SourceCodex,
		Agent:      "codex",
		CLIVersion: "1",
	}
	turn := usage.NewTurn("session", "turn", 1, source)
	turn.UserPrompts = 1
	turn.ModelTools = []usage.ToolObservation{{
		SessionID:     "session",
		TurnID:        "turn",
		RawName:       "exec",
		CanonicalName: "shell",
		Arguments:     `{"cmd":"cat /private/secret"}`,
		Timestamp:     time.Unix(1, 0).UTC(),
		Layer:         usage.LayerModel,
		Status:        usage.StatusSuccess,
		Source:        source,
	}}
	turn.SkillEvidence = []usage.SkillEvidence{{
		SessionID: "session",
		TurnID:    "turn",
		SkillName: "review",
		Mode:      usage.ModeExplicit,
		Method:    usage.MethodExplicitRequest,
		State:     usage.StateConfirmed,
		Timestamp: time.Unix(2, 0).UTC(),
		Source:    source,
	}}

	data, err := json.Marshal(TurnFromUsage(turn))
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(data)
	for _, forbidden := range []string{"private/history.jsonl", "secret", "arguments", "prompt text"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("cache DTO contains %q: %s", forbidden, serialized)
		}
	}
	if !strings.Contains(serialized, `"canonical_name":"shell"`) || !strings.Contains(serialized, `"skill_name":"review"`) {
		t.Fatalf("cache DTO lost report facts: %s", serialized)
	}
}

func TestSnapshotRoundTripRestoresReportFacts(t *testing.T) {
	when := time.Date(2026, 1, 2, 3, 4, 5, 6, time.UTC)
	source := usage.NewCtxSourceRef("ctx://event", "codex", "provider-session", "ctx-session", "event")
	turn := usage.NewTurn("session", "turn", 1, source)
	turn.StartedAt = when
	turn.EndedAt = when.Add(time.Second)
	turn.UserPrompts = 2
	turn.RuntimeTools = []usage.ToolObservation{{
		SessionID:     "session",
		TurnID:        "turn",
		RawName:       "exec",
		CanonicalName: "shell",
		Arguments:     "discarded",
		Timestamp:     when,
		Layer:         usage.LayerRuntime,
		Status:        usage.StatusFailure,
		Source:        source,
	}}
	turn.SkillEvidence = []usage.SkillEvidence{{
		SessionID: "session",
		TurnID:    "turn",
		SkillName: "review",
		Mode:      usage.ModeImplicit,
		Method:    usage.MethodImplicitAccess,
		State:     usage.StateInferred,
		Timestamp: when,
		Source:    source,
	}}

	restored := TurnFromUsage(turn).Usage()
	if restored.SessionID != turn.SessionID || restored.ID != turn.ID || restored.Ordinal != turn.Ordinal || restored.UserPrompts != turn.UserPrompts {
		t.Fatalf("turn facts changed: %#v", restored)
	}
	if !restored.StartedAt.Equal(turn.StartedAt) || !restored.EndedAt.Equal(turn.EndedAt) {
		t.Fatalf("turn times changed: %#v", restored)
	}
	if len(restored.RuntimeTools) != 1 || restored.RuntimeTools[0].CanonicalName != "shell" || restored.RuntimeTools[0].Arguments != "" || restored.RuntimeTools[0].Status != usage.StatusFailure {
		t.Fatalf("runtime tool facts changed: %#v", restored.RuntimeTools)
	}
	if len(restored.SkillEvidence) != 1 || restored.SkillEvidence[0].SkillName != "review" || restored.SkillEvidence[0].State != usage.StateInferred {
		t.Fatalf("skill facts changed: %#v", restored.SkillEvidence)
	}
	if restored.Source.Path != "" || restored.Source.Line != 0 || restored.Source.Agent != source.Agent || restored.Source.Provider != source.Provider {
		t.Fatalf("source facts changed: %#v", restored.Source)
	}
}

func TestSnapshotRoundTripPreservesOpenCodeSource(t *testing.T) {
	source := usage.NewOpenCodeSourceRef("/tmp/opencode/opencode.db", "1.18.27")
	turn := usage.NewTurn("opencode\x00session", "message-1", 1, source)
	turn.UserPrompts = 1
	restored := TurnFromUsage(turn).Usage()
	if restored.Source.Source != usage.SourceOpenCode || restored.Source.Agent != "opencode" || restored.Source.Provider != "opencode" {
		t.Fatalf("OpenCode source was not preserved: %#v", restored.Source)
	}
}

func TestSnapshotRoundTripPreservesModelsAndSessionMetadataWithoutRawSource(t *testing.T) {
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	source := usage.NewCodexSourceRef("/private/history.jsonl", 42, "1")
	turn := usage.NewTurn("session", "turn", 1, source)
	turn.ObserveModelAt(usage.NewModelRef("codex", "gpt-example"), when, source)
	turn.AddTokenUsageForModelAt(usage.NewModelRef("codex", "gpt-example"), when, usage.TokenUsage{TotalTokens: 7})
	turn.ModelTools = []usage.ToolObservation{{
		Arguments: `{"cmd":"cat /private/secret"}`,
	}}
	session := usage.NewSession("session", source)
	session.Title = "Implement usage explorer"
	session.ProjectName = "owner/project"
	session.ProjectPath = "/workspace/project"
	session.CreatedAt = when
	session.UpdatedAt = when.Add(time.Minute)

	snapshot := Snapshot{
		Turns:    []Turn{TurnFromUsage(turn)},
		Sessions: []Session{SessionFromUsage(session)},
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(data)
	for _, forbidden := range []string{"/private/history.jsonl", "/private/secret", "arguments", "prompt text"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("cache snapshot contains %q: %s", forbidden, serialized)
		}
	}

	var decoded Snapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	restored := decoded.Turns[0].Usage()
	if len(restored.ModelObservations) != 2 || restored.ModelObservations[0].Model.Name != "gpt-example" {
		t.Fatalf("models were not restored: %#v", restored.ModelObservations)
	}
	if len(restored.TokenUsageEvents) != 1 || restored.TokenUsageEvents[0].Model.Name != "gpt-example" {
		t.Fatalf("token model was not restored: %#v", restored.TokenUsageEvents)
	}
	restoredSession := decoded.Sessions[0].Usage()
	if restoredSession.QualifiedKey() != session.QualifiedKey() || restoredSession.Title != session.Title || !restoredSession.CreatedAt.Equal(session.CreatedAt) || restoredSession.ProjectName != session.ProjectName || restoredSession.ProjectPath != session.ProjectPath {
		t.Fatalf("session metadata changed: %#v", restoredSession)
	}
	if restored.ModelObservations[0].Source.Path != "" || restored.ModelObservations[0].Source.Line != 0 {
		t.Fatalf("raw model source survived cache: %#v", restored.ModelObservations[0].Source)
	}
}

func TestWarningsFromUsageOmitsRawSourcePosition(t *testing.T) {
	warnings := WarningsFromUsage([]usage.Warning{{Reason: "malformed_json", Type: "future", Path: "/private/history.jsonl", Line: 42, Count: 1}})
	data, err := json.Marshal(warnings)
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(data)
	if strings.Contains(serialized, "private/history.jsonl") || strings.Contains(serialized, "42") || strings.Contains(serialized, "path") || strings.Contains(serialized, "line") {
		t.Fatalf("sanitized warnings contain source position: %s", serialized)
	}
	if len(warnings) != 1 || warnings[0].Reason != "malformed_json" || warnings[0].Type != "future" || warnings[0].Count != 1 {
		t.Fatalf("warning facts changed: %#v", warnings)
	}
}
