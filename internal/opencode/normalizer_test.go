package opencode

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/xkumiyu/catsift/internal/usage"
	_ "modernc.org/sqlite"
)

func TestLoadNormalizesSessionsTurnsToolsSkillsAndTokens(t *testing.T) {
	root := t.TempDir()
	writeNormalizerFixture(t, root)

	result, err := Load(root, IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sessions) != 1 || result.Sessions[0].ID != "s1" {
		t.Fatalf("sessions = %#v", result.Sessions)
	}
	if result.Sessions[0].QualifiedKey() == result.Sessions[0].ID {
		t.Fatalf("session display ID and key were not separated: %#v", result.Sessions[0])
	}
	if result.Sessions[0].ProjectPath != "/workspace/project" || result.Sessions[0].CLIVersion != "1.18.27" {
		t.Fatalf("session metadata = %#v", result.Sessions[0])
	}
	if len(result.Turns) != 2 || result.Turns[0].UserPrompts != 1 || result.Turns[1].UserPrompts != 1 {
		t.Fatalf("turns = %#v", result.Turns)
	}
	first := result.Turns[0]
	if first.Source.Source != usage.SourceOpenCode || first.Source.Agent != "opencode" || first.Source.ProviderSessionID != "s1" {
		t.Fatalf("turn source = %#v", first.Source)
	}
	if len(first.RuntimeTools) != 3 {
		t.Fatalf("runtime tools = %#v", first.RuntimeTools)
	}
	if first.RuntimeTools[0].CanonicalName != "shell" || first.RuntimeTools[0].Status != usage.StatusSuccess || first.RuntimeTools[0].CallID != "call-1" {
		t.Fatalf("completed tool = %#v", first.RuntimeTools[0])
	}
	if first.RuntimeTools[1].Status != usage.StatusFailure {
		t.Fatalf("failed tool = %#v", first.RuntimeTools[1])
	}
	if first.TokenUsage == nil || *first.TokenUsage != (usage.TokenUsage{InputTokens: 110, CachedInputTokens: 40, OutputTokens: 25, ReasoningOutputTokens: 3, TotalTokens: 135}) {
		t.Fatalf("token usage = %#v", first.TokenUsage)
	}
	if len(first.TokenUsageEvents) == 0 || first.TokenUsageEvents[0].Model != usage.NewModelRef("opencode", "gpt-example") {
		t.Fatalf("token usage model = %#v", first.TokenUsageEvents)
	}
	if len(first.SkillEvidence) != 4 {
		t.Fatalf("skill evidence = %#v", first.SkillEvidence)
	}
	uses := usage.MergeSkillEvidence(first.SkillEvidence)
	if len(uses) != 1 || uses[0].SkillName != "review" || !uses[0].HasMode(usage.ModeExplicit) || !uses[0].HasMode(usage.ModeImplicit) {
		t.Fatalf("skill uses = %#v", uses)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Reason != "opencode_malformed_part" || result.Warnings[0].Source != usage.SourceOpenCode {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

func TestNormalizerPreservesSessionTitle(t *testing.T) {
	normalizer := newNormalizer("/tmp/opencode.db")
	if err := normalizer.consume(Row{Kind: RowSession, Session: SessionRow{ID: "s1", Title: "Implement usage explorer"}}); err != nil {
		t.Fatal(err)
	}
	result := normalizer.result()
	if len(result.Sessions) != 1 || result.Sessions[0].Title != "Implement usage explorer" {
		t.Fatalf("session metadata = %#v", result.Sessions)
	}
}

func TestLoadUsesOpenCodeSessionModelForModelAttribution(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "opencode.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE session (id TEXT PRIMARY KEY, directory TEXT, title TEXT, version TEXT, time_created INTEGER, time_updated INTEGER, model TEXT)`,
		`CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT, time_created INTEGER, time_updated INTEGER, data TEXT)`,
		`CREATE TABLE part (id TEXT PRIMARY KEY, message_id TEXT, session_id TEXT, time_created INTEGER, time_updated INTEGER, data TEXT)`,
		`INSERT INTO session VALUES ('s1', '/workspace', 'Model test', '1.0', 1000, 3000, '{"id":"model-a","providerID":"provider-a"}')`,
		`INSERT INTO message VALUES ('m1', 's1', 2000, 2000, '{"role":"user"}')`,
		`INSERT INTO part VALUES ('p1', 'm1', 's1', 2000, 2000, '{"type":"text","text":"prompt"}')`,
		`INSERT INTO message VALUES ('m2', 's1', 2500, 2500, '{"role":"assistant"}')`,
		`INSERT INTO part VALUES ('p2', 'm2', 's1', 2500, 2500, '{"type":"step-finish","tokens":{"total":7,"input":5,"output":2}}')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	result, err := Load(root, IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := usage.NewModelRef("provider-a", "model-a")
	if len(result.Turns) != 1 || len(result.Turns[0].ModelObservations) == 0 {
		t.Fatalf("model observations = %#v", result.Turns)
	}
	if got := result.Turns[0].ModelObservations[0].Model; got != want {
		t.Fatalf("model observation = %#v, want %#v", got, want)
	}
	if len(result.Turns[0].TokenUsageEvents) != 1 || result.Turns[0].TokenUsageEvents[0].Model != want {
		t.Fatalf("token usage events = %#v, want model %#v", result.Turns[0].TokenUsageEvents, want)
	}
}

func TestLoadIsDeterministicRegardlessOfInsertionOrder(t *testing.T) {
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	writeNormalizerFixture(t, firstRoot)
	writeNormalizerFixtureReversed(t, secondRoot)

	first, err := Load(firstRoot, IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Load(secondRoot, IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stripSourcePaths(first), stripSourcePaths(second)) {
		t.Fatalf("insertion order changed result:\nfirst=%#v\nsecond=%#v", first, second)
	}
}

func TestLoadAppliesPeriodFilterToEveryObservation(t *testing.T) {
	root := t.TempDir()
	writeNormalizerFixture(t, root)
	from := time.UnixMilli(1_700_000_008_000).UTC()
	to := from.Add(2 * time.Second)
	result, err := Load(root, IngestOptions{From: from, To: to})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || result.Turns[0].UserPrompts != 1 {
		t.Fatalf("filtered turns = %#v", result.Turns)
	}
	if len(result.Turns[0].RuntimeTools) != 0 || result.Turns[0].TokenUsage != nil {
		t.Fatalf("out-of-range observations survived: %#v", result.Turns[0])
	}
}

func TestFilterTurnPreservesModelAttribution(t *testing.T) {
	when := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	model := usage.NewModelRef("opencode", "gpt-example")
	turn := usage.NewTurn("session", "turn", 1, usage.NewOpenCodeSourceRef("/tmp/opencode.db", "1"))
	turn.ModelObservations = []usage.ModelObservation{{Model: model, Timestamp: when}}
	turn.TokenUsageEvents = []usage.TokenUsageEvent{{Model: model, Timestamp: when, Usage: usage.TokenUsage{TotalTokens: 7}}}

	filtered, ok := filterTurn(turn, timestampFilter{cutoff: when.Add(-time.Second), until: when.Add(time.Second)})
	if !ok {
		t.Fatal("model observation should keep the turn")
	}
	if len(filtered.ModelObservations) != 1 || filtered.ModelObservations[0].Model != model {
		t.Fatalf("model observations = %#v", filtered.ModelObservations)
	}
	if len(filtered.TokenUsageEvents) != 1 || filtered.TokenUsageEvents[0].Model != model {
		t.Fatalf("token usage events = %#v", filtered.TokenUsageEvents)
	}
}

func TestLoadIgnoresSyntheticAndIgnoredUserText(t *testing.T) {
	root := t.TempDir()
	writeFixtureWithStatements(t, root, []fixtureRow{
		{kind: "session", query: `INSERT INTO session VALUES (?, ?, ?, ?, ?)`, args: []any{"s1", "/workspace/project", "1.18.27", int64(1_700_000_000_000), int64(1_700_000_003_000)}},
		{kind: "message", query: `INSERT INTO message VALUES (?, ?, ?, ?, ?)`, args: []any{"m1", "s1", int64(1_700_000_001_000), int64(1_700_000_001_000), `{"role":"user"}`}},
		{kind: "part", query: `INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, args: []any{"p1", "m1", "s1", int64(1_700_000_001_000), int64(1_700_000_001_000), `{"type":"text","text":"$synthetic","synthetic":true}`}},
		{kind: "part", query: `INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, args: []any{"p2", "m1", "s1", int64(1_700_000_001_100), int64(1_700_000_001_100), `{"type":"text","text":"$ignored","ignored":true}`}},
		{kind: "part", query: `INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, args: []any{"p3", "m1", "s1", int64(1_700_000_001_200), int64(1_700_000_001_200), `{"type":"text","text":"$compaction","metadata":{"compaction_continue":true}}`}},
		{kind: "part", query: `INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, args: []any{"p4", "m1", "s1", int64(1_700_000_001_300), int64(1_700_000_001_300), `{"type":"compaction"}`}},
		{kind: "message", query: `INSERT INTO message VALUES (?, ?, ?, ?, ?)`, args: []any{"m2", "s1", int64(1_700_000_002_000), int64(1_700_000_002_000), `{"role":"user"}`}},
		{kind: "part", query: `INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, args: []any{"p5", "m2", "s1", int64(1_700_000_002_000), int64(1_700_000_002_000), `{"type":"text","text":"real prompt"}`}},
	})

	result, err := Load(root, IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || result.Turns[0].UserPrompts != 1 {
		t.Fatalf("synthetic and ignored text changed prompts: %#v", result.Turns)
	}
	if len(result.Turns[0].SkillEvidence) != 0 {
		t.Fatalf("synthetic and ignored text created skill evidence: %#v", result.Turns[0].SkillEvidence)
	}
}

func TestArgumentPathAcceptsOpenCodeFilePath(t *testing.T) {
	got := argumentPath(`{"filePath":"/workspace/.agents/skills/review/SKILL.md"}`)
	if got != "/workspace/.agents/skills/review/SKILL.md" {
		t.Fatalf("argument path = %q", got)
	}
}

func TestLoadUsesStepFinishTokensWhenMessageUsageIsMissing(t *testing.T) {
	root := t.TempDir()
	writeFixtureWithStatements(t, root, []fixtureRow{
		{kind: "session", query: `INSERT INTO session VALUES (?, ?, ?, ?, ?)`, args: []any{"s1", "/workspace/project", "1.18.27", int64(1_700_000_000_000), int64(1_700_000_003_000)}},
		{kind: "message", query: `INSERT INTO message VALUES (?, ?, ?, ?, ?)`, args: []any{"m1", "s1", int64(1_700_000_001_000), int64(1_700_000_001_000), `{"role":"user"}`}},
		{kind: "part", query: `INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, args: []any{"p1", "m1", "s1", int64(1_700_000_001_000), int64(1_700_000_001_000), `{"type":"text","text":"prompt"}`}},
		{kind: "message", query: `INSERT INTO message VALUES (?, ?, ?, ?, ?)`, args: []any{"m2", "s1", int64(1_700_000_002_000), int64(1_700_000_002_000), `{"role":"assistant"}`}},
		{kind: "part", query: `INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, args: []any{"p2", "m2", "s1", int64(1_700_000_002_000), int64(1_700_000_002_000), `{"type":"step-finish","reason":"stop","tokens":{"total":7,"input":5,"output":2,"reasoning":0,"cache":{"read":0,"write":0}}}`}},
	})

	result, err := Load(root, IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || result.Turns[0].TokenUsage == nil {
		t.Fatalf("turns = %#v", result.Turns)
	}
	want := usage.TokenUsage{InputTokens: 5, OutputTokens: 2, TotalTokens: 7}
	if got := *result.Turns[0].TokenUsage; got != want {
		t.Fatalf("token usage = %#v, want %#v", got, want)
	}
}

func TestLoadDoesNotDoubleCountStepFinishTokens(t *testing.T) {
	root := t.TempDir()
	writeFixtureWithStatements(t, root, []fixtureRow{
		{kind: "session", query: `INSERT INTO session VALUES (?, ?, ?, ?, ?)`, args: []any{"s1", "/workspace/project", "1.18.27", int64(1_700_000_000_000), int64(1_700_000_003_000)}},
		{kind: "message", query: `INSERT INTO message VALUES (?, ?, ?, ?, ?)`, args: []any{"m1", "s1", int64(1_700_000_001_000), int64(1_700_000_001_000), `{"role":"user"}`}},
		{kind: "part", query: `INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, args: []any{"p1", "m1", "s1", int64(1_700_000_001_000), int64(1_700_000_001_000), `{"type":"text","text":"prompt"}`}},
		{kind: "message", query: `INSERT INTO message VALUES (?, ?, ?, ?, ?)`, args: []any{"m2", "s1", int64(1_700_000_002_000), int64(1_700_000_002_000), `{"role":"assistant","tokens":{"total":7,"input":5,"output":2,"reasoning":0,"cache":{"read":0,"write":0}}}`}},
		{kind: "part", query: `INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, args: []any{"p2", "m2", "s1", int64(1_700_000_002_000), int64(1_700_000_002_000), `{"type":"step-finish","reason":"stop","tokens":{"total":7,"input":5,"output":2,"reasoning":0,"cache":{"read":0,"write":0}}}`}},
	})

	result, err := Load(root, IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || result.Turns[0].TokenUsage == nil {
		t.Fatalf("turns = %#v", result.Turns)
	}
	if got := result.Turns[0].TokenUsage.TotalTokens; got != 7 {
		t.Fatalf("total tokens = %d, want 7", got)
	}
}

func TestLoadIgnoresKnownNonObservationalParts(t *testing.T) {
	root := t.TempDir()
	writeFixtureWithStatements(t, root, []fixtureRow{
		{kind: "session", query: `INSERT INTO session VALUES (?, ?, ?, ?, ?)`, args: []any{"s1", "/workspace/project", "1.18.27", int64(1_700_000_000_000), int64(1_700_000_003_000)}},
		{kind: "message", query: `INSERT INTO message VALUES (?, ?, ?, ?, ?)`, args: []any{"m1", "s1", int64(1_700_000_001_000), int64(1_700_000_001_000), `{"role":"user"}`}},
		{kind: "part", query: `INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, args: []any{"p1", "m1", "s1", int64(1_700_000_001_000), int64(1_700_000_001_000), `{"type":"text","text":"prompt"}`}},
		{kind: "part", query: `INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, args: []any{"p2", "m1", "s1", int64(1_700_000_001_100), int64(1_700_000_001_100), `{"type":"agent","name":"build"}`}},
		{kind: "part", query: `INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, args: []any{"p3", "m1", "s1", int64(1_700_000_001_200), int64(1_700_000_001_200), `{"type":"retry","attempt":1}`}},
	})

	result, err := Load(root, IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || result.Turns[0].UserPrompts != 1 {
		t.Fatalf("turns = %#v", result.Turns)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

func TestLoadSkipsTrailingJSONPayload(t *testing.T) {
	root := t.TempDir()
	writeFixtureWithStatements(t, root, []fixtureRow{
		{kind: "session", query: `INSERT INTO session VALUES (?, ?, ?, ?, ?)`, args: []any{"s1", "/workspace/project", "1.18.27", int64(1_700_000_000_000), int64(1_700_000_003_000)}},
		{kind: "message", query: `INSERT INTO message VALUES (?, ?, ?, ?, ?)`, args: []any{"m1", "s1", int64(1_700_000_001_000), int64(1_700_000_001_000), `{"role":"user"}`}},
		{kind: "part", query: `INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, args: []any{"p1", "m1", "s1", int64(1_700_000_001_000), int64(1_700_000_001_000), `{"type":"text","text":"bad"}{"type":"text","text":"trailing"}`}},
		{kind: "message", query: `INSERT INTO message VALUES (?, ?, ?, ?, ?)`, args: []any{"m2", "s1", int64(1_700_000_002_000), int64(1_700_000_002_000), `{"role":"user"}`}},
		{kind: "part", query: `INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, args: []any{"p2", "m2", "s1", int64(1_700_000_002_000), int64(1_700_000_002_000), `{"type":"text","text":"valid"}`}},
	})

	result, err := Load(root, IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || result.Turns[0].UserPrompts != 1 {
		t.Fatalf("turns = %#v", result.Turns)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Reason != "opencode_malformed_part" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

func TestLoadDoesNotTimestampUntimedToolAtTurnStart(t *testing.T) {
	root := t.TempDir()
	writeFixtureWithStatements(t, root, []fixtureRow{
		{kind: "session", query: `INSERT INTO session VALUES (?, ?, ?, ?, ?)`, args: []any{"s1", "/workspace/project", "1.18.27", int64(1_700_000_000_000), int64(1_700_000_003_000)}},
		{kind: "message", query: `INSERT INTO message VALUES (?, ?, ?, ?, ?)`, args: []any{"m1", "s1", int64(1_700_000_001_000), int64(1_700_000_001_000), `{"role":"user"}`}},
		{kind: "part", query: `INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, args: []any{"p1", "m1", "s1", nil, nil, `{"type":"tool","tool":"bash","callID":"call-1","state":{"status":"completed","input":{"command":"pwd"}}}`}},
		{kind: "part", query: `INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, args: []any{"p2", "m1", "s1", int64(1_700_000_001_000), int64(1_700_000_001_000), `{"type":"text","text":"prompt"}`}},
	})

	all, err := Load(root, IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Turns) != 1 || len(all.Turns[0].RuntimeTools) != 1 || !all.Turns[0].RuntimeTools[0].Timestamp.IsZero() {
		t.Fatalf("untimed tool = %#v", all.Turns)
	}

	from := time.UnixMilli(1_700_000_000_000).UTC()
	to := from.Add(2 * time.Second)
	filtered, err := Load(root, IngestOptions{From: from, To: to})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Turns) != 1 || len(filtered.Turns[0].RuntimeTools) != 0 {
		t.Fatalf("filtered untimed tool = %#v", filtered.Turns)
	}
}

func TestLoadDoesNotCountSkillContentAsPrompt(t *testing.T) {
	root := t.TempDir()
	writeFixtureWithStatements(t, root, []fixtureRow{
		{kind: "session", query: `INSERT INTO session VALUES (?, ?, ?, ?, ?)`, args: []any{"s1", "/workspace/project", "1.18.27", int64(1_700_000_000_000), int64(1_700_000_003_000)}},
		{kind: "message", query: `INSERT INTO message VALUES (?, ?, ?, ?, ?)`, args: []any{"m1", "s1", int64(1_700_000_001_000), int64(1_700_000_001_000), `{"role":"user"}`}},
		{kind: "part", query: `INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, args: []any{"p1", "m1", "s1", int64(1_700_000_001_000), int64(1_700_000_001_000), `{"type":"text","text":"<skill_content name=\"review\">instructions</skill_content>"}`}},
	})

	result, err := Load(root, IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || result.Turns[0].UserPrompts != 0 {
		t.Fatalf("skill content prompt count = %#v", result.Turns)
	}
	if len(result.Turns[0].SkillEvidence) != 1 || result.Turns[0].SkillEvidence[0].SkillName != "review" {
		t.Fatalf("skill content evidence = %#v", result.Turns[0].SkillEvidence)
	}
}

func TestLoadStartsNewTurnAfterAssistantOnlyActivity(t *testing.T) {
	root := t.TempDir()
	writeFixtureWithStatements(t, root, []fixtureRow{
		{kind: "session", query: `INSERT INTO session VALUES (?, ?, ?, ?, ?)`, args: []any{"s1", "/workspace/project", "1.18.27", int64(1_700_000_000_000), int64(1_700_000_003_000)}},
		{kind: "message", query: `INSERT INTO message VALUES (?, ?, ?, ?, ?)`, args: []any{"m1", "s1", int64(1_700_000_001_000), int64(1_700_000_001_000), `{"role":"assistant"}`}},
		{kind: "message", query: `INSERT INTO message VALUES (?, ?, ?, ?, ?)`, args: []any{"m2", "s1", int64(1_700_000_002_000), int64(1_700_000_002_000), `{"role":"user"}`}},
		{kind: "part", query: `INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, args: []any{"p1", "m2", "s1", int64(1_700_000_002_000), int64(1_700_000_002_000), `{"type":"text","text":"real prompt"}`}},
	})

	result, err := Load(root, IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 2 || result.Turns[0].UserPrompts != 0 || result.Turns[1].UserPrompts != 1 {
		t.Fatalf("assistant-only and user turns = %#v", result.Turns)
	}
}

func TestLoadCountsFileOnlyUserMessageAsPrompt(t *testing.T) {
	root := t.TempDir()
	writeFixtureWithStatements(t, root, []fixtureRow{
		{kind: "session", query: `INSERT INTO session VALUES (?, ?, ?, ?, ?)`, args: []any{"s1", "/workspace/project", "1.18.27", int64(1_700_000_000_000), int64(1_700_000_003_000)}},
		{kind: "message", query: `INSERT INTO message VALUES (?, ?, ?, ?, ?)`, args: []any{"m1", "s1", int64(1_700_000_001_000), int64(1_700_000_001_000), `{"role":"user"}`}},
		{kind: "part", query: `INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, args: []any{"p1", "m1", "s1", int64(1_700_000_001_000), int64(1_700_000_001_000), `{"type":"file","mime":"image/png","url":"data:image/png;base64,fixture"}`}},
	})

	result, err := Load(root, IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || result.Turns[0].UserPrompts != 1 {
		t.Fatalf("file-only prompt = %#v", result.Turns)
	}
}

func writeNormalizerFixture(t *testing.T, root string) {
	t.Helper()
	writeFixtureWithStatements(t, root, normalizerFixtureRows())
}

func normalizerFixtureRows() []fixtureRow {
	return []fixtureRow{
		{kind: "session", query: `INSERT INTO session VALUES (?, ?, ?, ?, ?)`, args: []any{"s1", "/workspace/project", "1.18.27", int64(1_700_000_000_000), int64(1_700_000_010_000)}},
		{kind: "message", query: `INSERT INTO message VALUES (?, ?, ?, ?, ?)`, args: []any{"m1", "s1", int64(1_700_000_001_000), int64(1_700_000_001_000), `{"role":"user"}`}},
		{kind: "part", query: `INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, args: []any{"p1", "m1", "s1", int64(1_700_000_001_000), int64(1_700_000_001_000), `{"type":"text","text":"$review inspect this"}`}},
		{kind: "part", query: `INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, args: []any{"p2", "m1", "s1", int64(1_700_000_002_000), int64(1_700_000_002_000), `{"type":"tool","tool":"bash","callID":"call-1","state":{"status":"running","input":{"command":"cat /workspace/.agents/skills/review/SKILL.md"}}}`}},
		{kind: "part", query: `INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, args: []any{"p3", "m1", "s1", int64(1_700_000_002_100), int64(1_700_000_002_100), `{"type":"tool","tool":"bash","callID":"call-1","state":{"status":"completed","input":{"command":"cat /workspace/.agents/skills/review/SKILL.md"}}}`}},
		{kind: "part", query: `INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, args: []any{"p4", "m1", "s1", int64(1_700_000_003_000), int64(1_700_000_003_000), `{"type":"tool","tool":"read","callID":"call-2","state":{"status":"error","input":{"path":"/workspace/.agents/skills/review/SKILL.md"}}}`}},
		{kind: "part", query: `INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, args: []any{"p5", "m1", "s1", int64(1_700_000_003_500), int64(1_700_000_003_500), `{not-json}`}},
		{kind: "message", query: `INSERT INTO message VALUES (?, ?, ?, ?, ?)`, args: []any{"m2", "s1", int64(1_700_000_004_000), int64(1_700_000_004_000), `{"role":"assistant","model":"gpt-example","tokens":{"total":120,"input":100,"output":20,"reasoning":3,"cache":{"read":40,"write":0}},"unknown_field":"ignored"}`}},
		{kind: "part", query: `INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, args: []any{"p6", "m2", "s1", int64(1_700_000_004_000), int64(1_700_000_004_000), `{"type":"text","text":"done"}`}},
		{kind: "message", query: `INSERT INTO message VALUES (?, ?, ?, ?, ?)`, args: []any{"m3", "s1", int64(1_700_000_005_000), int64(1_700_000_005_000), `{"role":"user"}`}},
		{kind: "part", query: `INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, args: []any{"p7", "m3", "s1", int64(1_700_000_005_000), int64(1_700_000_005_000), `{"type":"text","text":"<skill name=\"review\">instructions</skill>"}`}},
		{kind: "part", query: `INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, args: []any{"p8", "m3", "s1", int64(1_700_000_006_000), int64(1_700_000_006_000), `{"type":"tool","tool":"Skill","callID":"call-3","state":{"status":"completed","input":{"name":"review"}}}`}},
		{kind: "message", query: `INSERT INTO message VALUES (?, ?, ?, ?, ?)`, args: []any{"m4", "s1", int64(1_700_000_007_000), int64(1_700_000_007_000), `{"role":"assistant","tokens":{"input":10,"output":5}}`}},
		{kind: "message", query: `INSERT INTO message VALUES (?, ?, ?, ?, ?)`, args: []any{"m5", "s1", int64(1_700_000_008_000), int64(1_700_000_008_000), `{"role":"user"}`}},
		{kind: "part", query: `INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, args: []any{"p9", "m5", "s1", int64(1_700_000_008_000), int64(1_700_000_008_000), `{"type":"text","text":"second prompt"}`}},
		{kind: "message", query: `INSERT INTO message VALUES (?, ?, ?, ?, ?)`, args: []any{"m6", "s1", int64(1_700_000_009_000), int64(1_700_000_009_000), `{"role":"assistant"}`}},
	}
}

type fixtureRow struct {
	kind  string
	query string
	args  []any
}

func writeFixtureWithStatements(t *testing.T, root string, rows []fixtureRow) {
	t.Helper()
	dbPath := filepath.Join(root, "opencode.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	for _, statement := range []string{
		`CREATE TABLE session (id TEXT PRIMARY KEY, directory TEXT, version TEXT, time_created INTEGER, time_updated INTEGER)`,
		`CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT, time_created INTEGER, time_updated INTEGER, data TEXT)`,
		`CREATE TABLE part (id TEXT PRIMARY KEY, message_id TEXT, session_id TEXT, time_created INTEGER, time_updated INTEGER, data TEXT)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range rows {
		if _, err := db.Exec(row.query, row.args...); err != nil {
			t.Fatalf("insert %s: %v", row.kind, err)
		}
	}
}

func writeNormalizerFixtureReversed(t *testing.T, root string) {
	t.Helper()
	rows := normalizerFixtureRows()
	for left, right := 0, len(rows)-1; left < right; left, right = left+1, right-1 {
		rows[left], rows[right] = rows[right], rows[left]
	}
	writeFixtureWithStatements(t, root, rows)
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
		copy.Turns[i].Source.Path = "fixture"
		for j := range copy.Turns[i].ModelObservations {
			copy.Turns[i].ModelObservations[j].Source.Path = "fixture"
		}
		for j := range copy.Turns[i].RuntimeTools {
			copy.Turns[i].RuntimeTools[j].Source.Path = "fixture"
			copy.Turns[i].RuntimeTools[j].Arguments = ""
		}
		for j := range copy.Turns[i].SkillEvidence {
			copy.Turns[i].SkillEvidence[j].Source.Path = "fixture"
		}
	}
	for i := range copy.Sessions {
		copy.Sessions[i].Source.Path = "fixture"
	}
	for i := range copy.Warnings {
		copy.Warnings[i].Path = "fixture"
	}
	return copy
}
