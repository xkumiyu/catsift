package query

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xkumiyu/catsift/internal/usage"
)

func TestBuildReadModelFiltersAndSortsSyntheticSnapshotDeterministically(t *testing.T) {
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	codexSource := usage.NewCodexSourceRef("/private/codex.jsonl", 10, "1")
	openCodeSource := usage.NewOpenCodeSourceRef("/private/opencode.db", "2")
	turnA := usage.NewTurn("same-id", "turn-a", 1, codexSource)
	turnA.StartedAt = when
	turnA.EndedAt = when.Add(time.Minute)
	turnA.UserPrompts = 1
	turnA.UserPromptTimes = []time.Time{when}
	turnA.ObserveModelAt(usage.NewModelRef("codex", "model-a"), when, codexSource)
	turnA.AddTokenUsageForModelAt(usage.NewModelRef("codex", "model-a"), when, usage.TokenUsage{InputTokens: 2, TotalTokens: 3})
	turnA.SkillEvidence = []usage.SkillEvidence{
		usage.NewSkillEvidence("same-id", "turn-a", "review", usage.ModeExplicit, usage.MethodExplicitRequest, usage.StateUnconfirmed, when, codexSource),
		usage.NewSkillEvidence("same-id", "turn-a", "review", usage.ModeExplicit, usage.MethodStructuredTool, usage.StateConfirmed, when.Add(time.Second), codexSource),
	}
	turnA.ModelTools = []usage.ToolObservation{{
		SessionID: "same-id", TurnID: "turn-a", RawName: "exec", CanonicalName: "shell",
		Arguments: "cat /private/secret", Timestamp: when, Layer: usage.LayerModel, Status: usage.StatusSuccess, Source: codexSource,
	}}
	turnB := usage.NewTurn("same-id", "turn-b", 2, codexSource)
	turnB.StartedAt = when.Add(2 * time.Minute)
	turnB.EndedAt = when.Add(3 * time.Minute)
	turnB.ObserveModelAt(usage.NewModelRef("codex", "model-b"), turnB.StartedAt, codexSource)
	turnB.AddTokenUsageForModelAt(usage.NewModelRef("codex", "model-b"), turnB.StartedAt, usage.TokenUsage{TotalTokens: 8})
	turnOther := usage.NewTurn("same-id", "turn-other", 1, openCodeSource)
	turnOther.StartedAt = when.Add(4 * time.Minute)
	turnOther.EndedAt = when.Add(5 * time.Minute)
	turnOther.AddTokenUsageAt(turnOther.StartedAt, usage.TokenUsage{TotalTokens: 4})

	codexSession := usage.NewSession("same-id", codexSource)
	codexSession.ProjectPath = "/workspace/codex"
	opencodeSession := usage.NewSession("same-id", openCodeSource)
	opencodeSession.ProjectPath = "/workspace/opencode"
	input := Input{
		Turns:    []usage.Turn{turnB, turnOther, turnA},
		Sessions: []usage.Session{opencodeSession, codexSession},
		Warnings: []usage.Warning{{Reason: "malformed_json", Count: 1}},
	}

	model := Build(input, Filter{})
	if model.Overview.Sessions != 2 || model.Overview.Turns != 3 || model.Overview.UserPrompts != 1 || model.Overview.SkillUses != 1 {
		t.Fatalf("overview = %#v", model.Overview)
	}
	if model.Overview.TokenUsage.TotalTokens != 15 {
		t.Fatalf("overview token usage = %#v", model.Overview.TokenUsage)
	}
	if len(model.Models) != 3 || model.Models[0].Model != usage.NewModelRef("codex", "model-a") || model.Models[2].Model != usage.UnknownModel() {
		t.Fatalf("models = %#v", model.Models)
	}
	if len(model.Sessions) != 2 || model.Sessions[0].Key == model.Sessions[1].Key {
		t.Fatalf("source-qualified sessions = %#v", model.Sessions)
	}
	var codexSessionRow SessionSummary
	for _, row := range model.Sessions {
		if row.Source == usage.SourceCodex {
			codexSessionRow = row
		}
	}
	if !codexSessionRow.StartedAt.Equal(when) || !codexSessionRow.EndedAt.Equal(when.Add(3*time.Minute)) {
		t.Fatalf("session range = %#v", codexSessionRow)
	}

	filtered := Build(input, Filter{Model: usage.NewModelRef("codex", "model-a")})
	if filtered.Overview.Sessions != 1 || filtered.Overview.Turns != 1 || filtered.Overview.TokenUsage.TotalTokens != 3 {
		t.Fatalf("model-filtered overview = %#v", filtered.Overview)
	}
	if len(filtered.Sessions) != 1 || len(filtered.Models) != 1 || filtered.Models[0].Model != usage.NewModelRef("codex", "model-a") {
		t.Fatalf("model-filtered rows = %#v / %#v", filtered.Models, filtered.Sessions)
	}

	skillFiltered := Build(input, Filter{Skill: "review"})
	if skillFiltered.Overview.Sessions != 1 || skillFiltered.Overview.Turns != 1 || len(skillFiltered.Skills) != 1 {
		t.Fatalf("skill-filtered model = %#v", skillFiltered)
	}
	if skillFiltered.Skills[0].Uses != 1 || skillFiltered.Skills[0].MethodCounts[usage.MethodExplicitRequest] != 1 || skillFiltered.Skills[0].MethodCounts[usage.MethodStructuredTool] != 1 {
		t.Fatalf("skill summary = %#v", skillFiltered.Skills[0])
	}
	detail, ok := skillFiltered.SkillDetail("review")
	if !ok || len(detail.Sessions) != 1 || detail.Sessions[0].SessionID != "same-id" || detail.Sessions[0].Uses != 1 || detail.Sessions[0].StateCounts[usage.StateConfirmed] != 1 {
		t.Fatalf("skill detail = %#v, found=%v", detail, ok)
	}

	serialized, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"private/codex.jsonl", "private/opencode.db", "private/secret", "prompt text", "arguments"} {
		if strings.Contains(string(serialized), forbidden) {
			t.Fatalf("read model contains %q: %s", forbidden, serialized)
		}
	}

	reversed := input
	reversed.Turns = []usage.Turn{turnA, turnOther, turnB}
	reversed.Sessions = []usage.Session{codexSession, opencodeSession}
	if left, right := Build(input, Filter{}), Build(reversed, Filter{}); !reflect.DeepEqual(left, right) {
		t.Fatalf("read model depends on input order: left=%#v right=%#v", left, right)
	}
}

func TestBuildOverviewUsesSourceAgentsAndDataPeriod(t *testing.T) {
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	source := usage.NewCodexSourceRef("/private/codex.jsonl", 10, "1")
	turn := usage.NewTurn("session", "turn", 1, source)
	turn.StartedAt = when
	turn.EndedAt = when.Add(time.Minute)
	session := usage.NewSession("session", source)

	model := Build(Input{
		Turns:    []usage.Turn{turn},
		Sessions: []usage.Session{session},
		Agents:   []string{"codex"},
		Source:   usage.SourceCodex,
	}, Filter{Source: usage.SourceCodex})

	if model.Overview.Agent != "codex" || len(model.Overview.Agents) != 1 || model.Overview.Agents[0] != "codex" {
		t.Fatalf("overview agents = %#v", model.Overview)
	}
	if !model.Overview.Period.From.Equal(when) || !model.Overview.Period.To.Equal(when.Add(time.Minute)) {
		t.Fatalf("overview period = %#v", model.Overview.Period)
	}
}

func TestBuildSanitizesWarningsForReadModel(t *testing.T) {
	model := Build(Input{Warnings: []usage.Warning{{Reason: "malformed_json", Path: "/private/history.jsonl", Line: 7, Count: 1}}}, Filter{})
	data, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "history.jsonl") || strings.Contains(string(data), "\"line\"") || strings.Contains(string(data), "\"path\"") {
		t.Fatalf("read model warning contains source position: %s", data)
	}
	if len(model.Warnings) != 1 || model.Warnings[0].Reason != "malformed_json" || model.Warnings[0].Count != 1 {
		t.Fatalf("warning facts changed: %#v", model.Warnings)
	}
}

func TestSanitizeInputRemovesRendererUnsafeFields(t *testing.T) {
	source := usage.NewCodexSourceRef("/private/history.jsonl", 42, "1")
	turn := usage.NewTurn("session", "turn", 1, source)
	turn.ModelObservations = []usage.ModelObservation{{Model: usage.NewModelRef("codex", "gpt-example"), Source: source}}
	turn.ModelTools = []usage.ToolObservation{{Arguments: "secret", Source: source}}
	turn.RuntimeTools = []usage.ToolObservation{{Arguments: "secret", Source: source}}
	turn.SkillEvidence = []usage.SkillEvidence{{SkillName: "review", Source: source}}
	input := SanitizeInput(Input{Turns: []usage.Turn{turn}, Sessions: []usage.Session{{ID: "session", Source: source}}, Warnings: []usage.Warning{{Path: source.Path, Line: source.Line}}})

	if input.Turns[0].Source.Path != "" || input.Turns[0].Source.Line != 0 || input.Sessions[0].Source.Path != "" || input.Warnings[0].Path != "" {
		t.Fatalf("source position survived: %#v", input)
	}
	if input.Turns[0].ModelTools[0].Arguments != "" || input.Turns[0].RuntimeTools[0].Arguments != "" {
		t.Fatalf("tool arguments survived: %#v", input.Turns[0])
	}
	if len(input.Turns[0].ModelObservations) != 1 || len(input.Turns[0].SkillEvidence) != 1 {
		t.Fatalf("bounded observations changed: %#v", input.Turns[0])
	}
	if input.Turns[0].ModelObservations[0].Source.Path != "" || input.Turns[0].SkillEvidence[0].Source.Path != "" {
		t.Fatalf("nested source position survived: %#v", input.Turns[0])
	}
}

func TestSkillDetailGroupsUsesBySession(t *testing.T) {
	when := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	source := usage.NewCodexSourceRef("/private/history.jsonl", 1, "1")
	first := usage.NewTurn("session", "turn-1", 1, source)
	first.StartedAt = when
	first.EndedAt = when.Add(time.Minute)
	first.SkillEvidence = []usage.SkillEvidence{
		usage.NewSkillEvidence("session", "turn-1", "review", usage.ModeExplicit, usage.MethodExplicitRequest, usage.StateConfirmed, when, source),
	}
	second := usage.NewTurn("session", "turn-2", 2, source)
	second.StartedAt = when.Add(time.Hour)
	second.EndedAt = second.StartedAt.Add(time.Minute)
	second.SkillEvidence = []usage.SkillEvidence{
		usage.NewSkillEvidence("session", "turn-2", "review", usage.ModeImplicit, usage.MethodImplicitAccess, usage.StateInferred, second.StartedAt, source),
	}
	session := usage.NewSession("session", source)

	readModel := Build(Input{Turns: []usage.Turn{first, second}, Sessions: []usage.Session{session}}, Filter{})
	detail, ok := readModel.SkillDetail("review")
	if !ok || len(detail.Sessions) != 1 {
		t.Fatalf("skill detail sessions = %#v, found=%v", detail, ok)
	}
	row := detail.Sessions[0]
	if row.SessionID != "session" || row.Uses != 2 || row.Turns != 2 {
		t.Fatalf("grouped skill session = %#v", row)
	}
}

func TestSessionDetailContainsOnlyBoundedTurnFacts(t *testing.T) {
	when := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	source := usage.NewCodexSourceRef("/private/history.jsonl", 1, "1")
	turn := usage.NewTurn("s", "t", 1, source)
	turn.StartedAt = when
	turn.EndedAt = when.Add(time.Second)
	turn.AddTokenUsageForModelAt(usage.NewModelRef("codex", "gpt-example"), when, usage.TokenUsage{TotalTokens: 7})
	turn.RuntimeTools = []usage.ToolObservation{{RawName: "exec", CanonicalName: "shell", Arguments: "secret", Timestamp: when, Layer: usage.LayerRuntime, Source: source}}
	turn.SkillEvidence = []usage.SkillEvidence{{SkillName: "review", Mode: usage.ModeImplicit, Method: usage.MethodImplicitAccess, State: usage.StateInferred, Timestamp: when, Source: source}}
	session := usage.NewSession("s", source)
	readModel := Build(Input{Turns: []usage.Turn{turn}, Sessions: []usage.Session{session}}, Filter{})
	detail, ok := readModel.SessionDetail(session.QualifiedKey())
	if !ok || len(detail.Turns) != 1 || len(detail.Turns[0].Models) != 1 || detail.Turns[0].Models[0].Name != "gpt-example" {
		t.Fatalf("session detail = %#v, found=%v", detail, ok)
	}
	if len(detail.Turns[0].Tools) != 1 || detail.Turns[0].Tools[0] != "shell" || len(detail.Turns[0].Skills) != 1 || detail.Turns[0].Skills[0] != "review" {
		t.Fatalf("turn detail = %#v", detail.Turns[0])
	}
	data, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "history.jsonl") {
		t.Fatalf("session detail contains raw data: %s", data)
	}
}

func TestBuildAppliesSourceAgentProjectAndPeriodFilters(t *testing.T) {
	old := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	newer := old.Add(24 * time.Hour)
	source := usage.NewCodexSourceRef("/private/history.jsonl", 1, "1")
	first := usage.NewTurn("s", "old", 1, source)
	first.StartedAt = old
	first.EndedAt = old.Add(time.Minute)
	first.UserPrompts = 1
	first.UserPromptTimes = []time.Time{old}
	first.AddTokenUsageForModelAt(usage.NewModelRef("codex", "old-model"), old, usage.TokenUsage{TotalTokens: 2})
	second := usage.NewTurn("s", "new", 2, source)
	second.StartedAt = newer
	second.EndedAt = newer.Add(time.Minute)
	second.UserPrompts = 1
	second.UserPromptTimes = []time.Time{newer}
	second.AddTokenUsageForModelAt(usage.NewModelRef("codex", "new-model"), newer, usage.TokenUsage{TotalTokens: 5})
	session := usage.NewSession("s", source)
	session.ProjectPath = "/workspace/project"

	readModel := Build(Input{Turns: []usage.Turn{first, second}, Sessions: []usage.Session{session}}, Filter{
		Source:  usage.SourceCodex,
		Agent:   "Codex",
		Project: "/workspace/project",
		From:    newer,
		To:      newer.Add(2 * time.Hour),
	})
	if readModel.Overview.Sessions != 1 || readModel.Overview.Turns != 1 || readModel.Overview.UserPrompts != 1 || readModel.Overview.TokenUsage.TotalTokens != 5 {
		t.Fatalf("metadata/period filtered overview = %#v", readModel.Overview)
	}
	if len(readModel.Sessions) != 1 || len(readModel.Sessions[0].Models) != 1 || readModel.Sessions[0].Models[0].Name != "new-model" {
		t.Fatalf("metadata/period filtered sessions = %#v", readModel.Sessions)
	}
}

func TestOverviewAgentsFollowObservationFilters(t *testing.T) {
	when := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	codexSource := usage.NewCodexSourceRef("codex", 1, "")
	openCodeSource := usage.NewOpenCodeSourceRef("opencode", "")
	codexTurn := usage.NewTurn("codex-session", "codex-turn", 1, codexSource)
	codexTurn.StartedAt = when
	codexTurn.ObserveModelAt(usage.NewModelRef("codex", "gpt-example"), when, codexSource)
	openCodeTurn := usage.NewTurn("opencode-session", "opencode-turn", 1, openCodeSource)
	openCodeTurn.StartedAt = when
	openCodeTurn.ObserveModelAt(usage.NewModelRef("opencode", "gpt-example"), when, openCodeSource)
	codexSession := usage.NewSession("codex-session", codexSource)
	openCodeSession := usage.NewSession("opencode-session", openCodeSource)
	model := Build(Input{
		Turns:    []usage.Turn{codexTurn, openCodeTurn},
		Sessions: []usage.Session{codexSession, openCodeSession},
		Agents:   []string{"codex", "opencode"},
	}, Filter{Model: usage.NewModelRef("codex", "gpt-example")})
	if len(model.Overview.Agents) != 1 || model.Overview.Agents[0] != "codex" {
		t.Fatalf("filtered overview agents = %#v", model.Overview.Agents)
	}
}

func TestSessionMetadataOnlyStillHasDetail(t *testing.T) {
	when := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	source := usage.NewCodexSourceRef("/private/history.jsonl", 1, "1")
	session := usage.NewSession("metadata-only", source)
	session.CreatedAt = when
	session.UpdatedAt = when.Add(time.Minute)
	model := Build(Input{Sessions: []usage.Session{session}}, Filter{From: when, To: when.Add(time.Hour)})

	if len(model.Sessions) != 1 {
		t.Fatalf("sessions = %#v", model.Sessions)
	}
	detail, ok := model.SessionDetail(session.QualifiedKey())
	if !ok || detail.Summary.ID != "metadata-only" || len(detail.Turns) != 0 {
		t.Fatalf("metadata-only session detail = %#v, found=%v", detail, ok)
	}
}
