package usage

import (
	"testing"
	"time"
)

func TestConstructorsKeepRequiredFields(t *testing.T) {
	source := NewSourceRef("fixture.jsonl", 7, "0.1.0")
	turn := NewTurn("session-001", "turn-001", 1, source)
	if turn.SessionID != "session-001" || turn.ID != "turn-001" || turn.Source != source {
		t.Fatalf("unexpected turn: %#v", turn)
	}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	tool := NewToolObservation("session-001", "turn-001", "exec", "exec", LayerModel, StatusSuccess, now, source)
	if tool.Layer != LayerModel || tool.Status != StatusSuccess || tool.Timestamp != now {
		t.Fatalf("unexpected tool: %#v", tool)
	}
	evidence := NewSkillEvidence("session-001", "turn-001", "report", ModeExplicit, MethodExplicitInjected, StateConfirmed, now, source)
	if evidence.SkillName != "report" || evidence.Method != MethodExplicitInjected || evidence.State != StateConfirmed {
		t.Fatalf("unexpected evidence: %#v", evidence)
	}
}

func TestEnumValues(t *testing.T) {
	for _, value := range []ToolLayer{LayerModel, LayerRuntime, LayerEffective} {
		if value == "" {
			t.Fatal("empty tool layer")
		}
	}
	for _, value := range []ToolStatus{StatusSuccess, StatusFailure} {
		if value == "" {
			t.Fatal("empty tool status")
		}
	}
	for _, value := range []SkillEvidenceMethod{MethodExplicitInjected, MethodSelectedSkillInstructions, MethodSkillInjection, MethodRuntimeSkillItem, MethodStructuredTool, MethodExplicitRequest, MethodImplicitAccess} {
		if value == "" {
			t.Fatal("empty skill method")
		}
	}
	for _, value := range []SkillMode{ModeExplicit, ModeImplicit, ModeUnknown} {
		if value == "" {
			t.Fatal("empty skill mode")
		}
	}
}

func TestZeroValuesAreSafe(t *testing.T) {
	var result Result
	if result.Sessions != 0 || result.ToolCalls != nil || result.SkillUses != nil {
		t.Fatalf("unexpected result zero value: %#v", result)
	}
	var source SourceRef
	if source.Path != "" || source.Line != 0 {
		t.Fatalf("unexpected source zero value: %#v", source)
	}
}

func TestSourceRefPreservesSourceAndAgentIdentity(t *testing.T) {
	source := NewCtxSourceRef("ctx", "OpenCode", "provider-session", "ctx-session", "ctx-event")
	if source.Source != SourceCtx || source.Agent != "opencode" || source.Provider != "OpenCode" {
		t.Fatalf("source identity = %#v", source)
	}
	if source.ProviderSessionID != "provider-session" || source.CtxSessionID != "ctx-session" || source.EventID != "ctx-event" {
		t.Fatalf("session identity = %#v", source)
	}
	if got := CanonicalAgentID(" Open-Code "); got != "open-code" {
		t.Fatalf("canonical agent = %q", got)
	}
	if got := AgentDisplayName("opencode"); got != "OpenCode" {
		t.Fatalf("agent display name = %q", got)
	}
}

func TestOpenCodeSourceRefPreservesIdentity(t *testing.T) {
	source := NewOpenCodeSourceRef("/tmp/opencode/opencode.db", "1.18.27")
	if !SourceOpenCode.Valid() {
		t.Fatal("OpenCode source should be valid")
	}
	if source.Source != SourceOpenCode || source.Agent != "opencode" || source.Provider != "opencode" {
		t.Fatalf("OpenCode source identity = %#v", source)
	}
	if source.Path != "/tmp/opencode/opencode.db" || source.CLIVersion != "1.18.27" {
		t.Fatalf("OpenCode source metadata = %#v", source)
	}
}
