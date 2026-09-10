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
