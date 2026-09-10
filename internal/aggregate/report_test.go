package aggregate

import (
	"reflect"
	"testing"
	"time"

	"github.com/xkumiyu/catsift/internal/skillinventory"
	"github.com/xkumiyu/catsift/internal/usage"
)

func TestBuildOverviewAndLayerAggregation(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	turn := usage.Turn{
		SessionID:    "s",
		UserPrompts:  1,
		ModelTools:   []usage.ToolObservation{{RawName: "exec", CanonicalName: "exec", Layer: usage.LayerModel, Timestamp: now}, {CanonicalName: "web_search", Layer: usage.LayerModel, Timestamp: now}},
		RuntimeTools: []usage.ToolObservation{{CanonicalName: "shell", Layer: usage.LayerRuntime, Status: usage.StatusFailure, Timestamp: now}},
		SkillEvidence: []usage.SkillEvidence{
			usage.NewSkillEvidence("s", "t", "report", usage.ModeExplicit, usage.MethodExplicitInjected, usage.StateConfirmed, now, usage.SourceRef{}),
			usage.NewSkillEvidence("s", "t", "report", usage.ModeExplicit, usage.MethodExplicitRequest, usage.StateUnconfirmed, now, usage.SourceRef{}),
		},
	}
	input := Input{Turns: []usage.Turn{turn}, SessionCount: 1}
	report := BuildOverview(input)
	if report.Overview.Sessions != 1 || report.Overview.Turns != 1 || report.Overview.UserPrompts != 1 || report.Overview.ToolCalls != 2 || report.Overview.SkillUsesTurn != 1 || report.Overview.SkillUsesSession != 1 {
		t.Fatalf("overview = %#v", report.Overview)
	}
	if len(report.Tools) != 2 || report.Tools[0].Name != "shell" || report.Tools[0].Failures != 1 {
		t.Fatalf("effective rows = %#v", report.Tools)
	}
	model := Tools(input, usage.LayerModel)
	if len(model) != 2 || model[0].Name != "exec" {
		t.Fatalf("model rows = %#v", model)
	}
	strict := Skills(input, true)
	if len(strict) != 1 || strict[0].Total != 1 || strict[0].Confirmed != 1 {
		t.Fatalf("strict rows = %#v", strict)
	}
}

func TestBuildOverviewSumsTokenUsage(t *testing.T) {
	first := usage.TokenUsage{InputTokens: 10, CachedInputTokens: 4, CacheWriteInputTokens: 1, OutputTokens: 3, ReasoningOutputTokens: 2, TotalTokens: 13}
	second := usage.TokenUsage{InputTokens: 20, CachedInputTokens: 8, CacheWriteInputTokens: 2, OutputTokens: 5, ReasoningOutputTokens: 1, TotalTokens: 25}
	report := BuildOverview(Input{Turns: []usage.Turn{
		{SessionID: "s", TokenUsage: &first},
		{SessionID: "s", TokenUsage: &second},
	}})
	want := usage.TokenUsage{InputTokens: 30, CachedInputTokens: 12, CacheWriteInputTokens: 3, OutputTokens: 8, ReasoningOutputTokens: 3, TotalTokens: 38}
	if !report.Overview.TokenUsageAvailable || report.Overview.TokenUsage != want {
		t.Fatalf("token usage = %#v, want %#v", report.Overview.TokenUsage, want)
	}
}

func TestAggregateSortsByCountThenName(t *testing.T) {
	turns := []usage.Turn{
		{SessionID: "s", RuntimeTools: []usage.ToolObservation{{CanonicalName: "z", Status: usage.StatusSuccess}, {CanonicalName: "a", Status: usage.StatusSuccess}}},
		{SessionID: "s", RuntimeTools: []usage.ToolObservation{{CanonicalName: "a", Status: usage.StatusSuccess}}},
	}
	rows := Tools(Input{Turns: turns}, usage.LayerRuntime)
	if len(rows) != 2 || rows[0].Name != "a" || rows[1].Name != "z" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestAggregateCombinesCanonicalNamesAcrossAgents(t *testing.T) {
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	turns := []usage.Turn{
		{
			SessionID:    "ctx\x00codex\x00shared",
			Source:       usage.SourceRef{Source: usage.SourceCtx, Agent: "codex"},
			RuntimeTools: []usage.ToolObservation{{CanonicalName: "shell", Status: usage.StatusSuccess, Timestamp: stamp}},
			SkillEvidence: []usage.SkillEvidence{
				usage.NewSkillEvidence("ctx\x00codex\x00shared", "t1", "review", usage.ModeExplicit, usage.MethodStructuredTool, usage.StateConfirmed, stamp, usage.SourceRef{Source: usage.SourceCtx, Agent: "codex"}),
			},
		},
		{
			SessionID:    "ctx\x00opencode\x00shared",
			Source:       usage.SourceRef{Source: usage.SourceCtx, Agent: "opencode"},
			RuntimeTools: []usage.ToolObservation{{CanonicalName: "shell", Status: usage.StatusFailure, Timestamp: stamp.Add(time.Second)}},
			SkillEvidence: []usage.SkillEvidence{
				usage.NewSkillEvidence("ctx\x00opencode\x00shared", "t1", "review", usage.ModeExplicit, usage.MethodStructuredTool, usage.StateConfirmed, stamp.Add(time.Second), usage.SourceRef{Source: usage.SourceCtx, Agent: "opencode"}),
			},
		},
	}
	report := BuildOverview(Input{Turns: turns})
	if report.Overview.Sessions != 2 || report.Overview.Turns != 2 || report.Overview.ToolCalls != 2 || report.Overview.SkillUsesTurn != 2 || report.Overview.SkillUsesSession != 2 {
		t.Fatalf("overview = %#v", report.Overview)
	}
	if len(report.Tools) != 1 || report.Tools[0].Name != "shell" || report.Tools[0].Calls != 2 || report.Tools[0].Failures != 1 {
		t.Fatalf("tools = %#v", report.Tools)
	}
	if len(report.Skills) != 1 || report.Skills[0].Name != "review" || report.Skills[0].Total != 2 {
		t.Fatalf("skills = %#v", report.Skills)
	}
}

func TestSkillsPreserveOverlappingModesAndUnknownActivation(t *testing.T) {
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	turns := []usage.Turn{
		{SessionID: "s", ID: "explicit-and-implicit", SkillEvidence: []usage.SkillEvidence{
			usage.NewSkillEvidence("s", "explicit-and-implicit", "report", usage.ModeExplicit, usage.MethodExplicitRequest, usage.StateUnconfirmed, stamp, usage.SourceRef{}),
			usage.NewSkillEvidence("s", "explicit-and-implicit", "report", usage.ModeImplicit, usage.MethodImplicitAccess, usage.StateInferred, stamp, usage.SourceRef{}),
		}},
		{SessionID: "s", ID: "unknown", SkillEvidence: []usage.SkillEvidence{
			usage.NewSkillEvidence("s", "unknown", "report", usage.ModeUnknown, usage.MethodSelectedSkillInstructions, usage.StateConfirmed, stamp.Add(time.Second), usage.SourceRef{}),
		}},
	}
	rows := Skills(Input{Turns: turns}, false)
	if len(rows) != 1 {
		t.Fatalf("rows = %#v", rows)
	}
	row := rows[0]
	if row.Total != 2 || row.Explicit != 1 || row.Implicit != 1 || row.Unknown != 1 || row.Confirmed != 1 || row.Inferred != 1 || row.Unconfirmed != 0 {
		t.Fatalf("skill row = %#v", row)
	}
}

func TestSkillsCountEachSkillOncePerTurn(t *testing.T) {
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	turns := []usage.Turn{
		{SessionID: "s", ID: "t1", SkillEvidence: []usage.SkillEvidence{
			usage.NewSkillEvidence("s", "t1", "report", usage.ModeImplicit, usage.MethodImplicitAccess, usage.StateInferred, stamp, usage.SourceRef{}),
			usage.NewSkillEvidence("s", "t1", "report", usage.ModeImplicit, usage.MethodImplicitAccess, usage.StateInferred, stamp.Add(time.Second), usage.SourceRef{}),
		}},
		{SessionID: "s", ID: "t2", SkillEvidence: []usage.SkillEvidence{
			usage.NewSkillEvidence("s", "t2", "report", usage.ModeExplicit, usage.MethodExplicitInjected, usage.StateConfirmed, stamp.Add(2*time.Second), usage.SourceRef{}),
		}},
	}
	rows := Skills(Input{Turns: turns}, false)
	if len(rows) != 1 {
		t.Fatalf("rows = %#v", rows)
	}
	row := rows[0]
	if row.Total != 2 || row.Implicit != 1 || row.Explicit != 1 {
		t.Fatalf("skill count = %#v", row)
	}

	report := BuildOverview(Input{Turns: turns})
	if report.Overview.Turns != 2 || report.Overview.SkillUsesTurn != 2 || report.Overview.SkillUsesSession != 1 {
		t.Fatalf("overview grouping = %#v", report.Overview)
	}
}

func TestSkillsCanGroupEachSkillOncePerSession(t *testing.T) {
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	turns := []usage.Turn{
		{SessionID: "s1", ID: "t1", SkillEvidence: []usage.SkillEvidence{
			usage.NewSkillEvidence("s1", "t1", "report", usage.ModeImplicit, usage.MethodImplicitAccess, usage.StateInferred, stamp, usage.SourceRef{}),
		}},
		{SessionID: "s1", ID: "t2", SkillEvidence: []usage.SkillEvidence{
			usage.NewSkillEvidence("s1", "t2", "report", usage.ModeExplicit, usage.MethodExplicitInjected, usage.StateConfirmed, stamp.Add(time.Second), usage.SourceRef{}),
		}},
		{SessionID: "s2", ID: "t3", SkillEvidence: []usage.SkillEvidence{
			usage.NewSkillEvidence("s2", "t3", "report", usage.ModeUnknown, usage.MethodSelectedSkillInstructions, usage.StateConfirmed, stamp.Add(2*time.Second), usage.SourceRef{}),
		}},
	}
	rows := SkillsBy(Input{Turns: turns}, false, SkillGroupBySession)
	if len(rows) != 1 {
		t.Fatalf("rows = %#v", rows)
	}
	row := rows[0]
	if row.Total != 2 || row.Explicit != 1 || row.Implicit != 1 || row.Unknown != 1 || row.Confirmed != 2 || row.Inferred != 0 {
		t.Fatalf("session skill count = %#v", row)
	}
}

func TestUnusedSkillsMatchesCanonicalNamesAndPreservesPhysicalEntries(t *testing.T) {
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	turns := []usage.Turn{
		{SessionID: "s", ID: "used", SkillEvidence: []usage.SkillEvidence{
			usage.NewSkillEvidence("s", "used", "used", usage.ModeExplicit, usage.MethodExplicitInjected, usage.StateConfirmed, stamp, usage.SourceRef{}),
		}},
		{SessionID: "s", ID: "inferred", SkillEvidence: []usage.SkillEvidence{
			usage.NewSkillEvidence("s", "inferred", "inferred", usage.ModeImplicit, usage.MethodImplicitAccess, usage.StateInferred, stamp, usage.SourceRef{}),
		}},
		{SessionID: "s", ID: "shared", SkillEvidence: []usage.SkillEvidence{
			usage.NewSkillEvidence("s", "shared", "shared", usage.ModeExplicit, usage.MethodExplicitInjected, usage.StateConfirmed, stamp, usage.SourceRef{}),
		}},
	}
	inventory := []skillinventory.InventoryEntry{
		{Name: "used", Path: "/one/used"},
		{Name: "inferred", Path: "/one/inferred"},
		{Name: "canonical-name", Path: "/one/directory-name"},
		{Name: "shared", Path: "/one/shared"},
		{Name: "shared", Path: "/two/shared"},
		{Name: "unused", Path: "/one/unused"},
	}

	want := []skillinventory.InventoryEntry{
		inventory[2],
		inventory[5],
	}
	got := UnusedSkills(Input{Turns: turns}, inventory, false, SkillGroupByTurn)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UnusedSkills() = %#v, want %#v", got, want)
	}
	strict := UnusedSkills(Input{Turns: turns}, inventory, true, SkillGroupBySession)
	if !reflect.DeepEqual(strict, []skillinventory.InventoryEntry{inventory[2], inventory[1], inventory[5]}) {
		t.Fatalf("UnusedSkills(strict) = %#v", strict)
	}
}

func TestUnusedSkillsMembershipDoesNotDependOnGrouping(t *testing.T) {
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	turns := []usage.Turn{
		{SessionID: "s", ID: "t1", SkillEvidence: []usage.SkillEvidence{
			usage.NewSkillEvidence("s", "t1", "used", usage.ModeImplicit, usage.MethodImplicitAccess, usage.StateInferred, stamp, usage.SourceRef{}),
		}},
		{SessionID: "s", ID: "t2", SkillEvidence: []usage.SkillEvidence{
			usage.NewSkillEvidence("s", "t2", "used", usage.ModeExplicit, usage.MethodExplicitInjected, usage.StateConfirmed, stamp.Add(time.Second), usage.SourceRef{}),
		}},
	}
	inventory := []skillinventory.InventoryEntry{
		{Name: "used", Path: "/used"},
		{Name: "unused", Path: "/unused"},
	}
	turn := UnusedSkills(Input{Turns: turns}, inventory, false, SkillGroupByTurn)
	session := UnusedSkills(Input{Turns: turns}, inventory, false, SkillGroupBySession)
	if !reflect.DeepEqual(turn, session) {
		t.Fatalf("grouping changed unused membership: turn=%#v session=%#v", turn, session)
	}
}

func TestBuildUnusedReportKeepsInventoryMetadataSeparateFromUsageRows(t *testing.T) {
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	entry := skillinventory.InventoryEntry{Name: "unused", Path: "/skills/unused"}
	snapshot := skillinventory.InventorySnapshot{
		Roots:          []string{"/skills"},
		InstalledCount: 1,
		Entries:        []skillinventory.InventoryEntry{entry},
		Warnings:       []usage.Warning{{Reason: "walk warning", Type: "skill_inventory_walk", Path: "/skills/skip", Count: 1}},
	}
	report := BuildUnusedReport(Input{Turns: []usage.Turn{{SkillEvidence: []usage.SkillEvidence{
		usage.NewSkillEvidence("s", "t", "used", usage.ModeExplicit, usage.MethodExplicitInjected, usage.StateConfirmed, stamp, usage.SourceRef{}),
	}}}}, snapshot, false, SkillGroupByTurn)
	if !reflect.DeepEqual(report.UnusedSkills, []skillinventory.InventoryEntry{entry}) {
		t.Fatalf("UnusedSkills = %#v", report.UnusedSkills)
	}
	if report.InstalledSkills != 1 || !reflect.DeepEqual(report.UnusedRoots, snapshot.Roots) {
		t.Fatalf("inventory metadata = installed:%d roots:%#v", report.InstalledSkills, report.UnusedRoots)
	}
	if len(report.Skills) != 0 || len(report.Warnings) != 1 {
		t.Fatalf("usage rows/warnings = skills:%#v warnings:%#v", report.Skills, report.Warnings)
	}
}
