package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	ctxsource "github.com/xkumiyu/catsift/internal/ctx"
	"github.com/xkumiyu/catsift/internal/output"
	"github.com/xkumiyu/catsift/internal/query"
	"github.com/xkumiyu/catsift/internal/usage"
	appversion "github.com/xkumiyu/catsift/internal/version"
	_ "modernc.org/sqlite"
)

func testHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(home, "sessions", "2026", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s","cli_version":"1"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"task_started","payload":{"turn_id":"t"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"user_message","payload":{"text":"hello"}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"response_item","payload":{"type":"custom_tool_call","name":"exec","call_id":"c","input":"{\"cmd\":\"echo ok\"}"}}`,
		`{"timestamp":"2026-01-01T00:00:04Z","type":"event_msg","payload":{"type":"ItemCompleted","item":{"type":"CommandExecution","id":"i","status":"completed","command":"echo ok"}}}`,
		`{"timestamp":"2026-01-01T00:00:05Z","type":"task_complete","payload":{"turn_id":"t"}}`,
		`{"timestamp":"2026-01-01T00:00:06Z","type":"task_started","payload":{"turn_id":"t2"}}`,
		`{"timestamp":"2026-01-01T00:00:07Z","type":"user_message","payload":{"text":"$report"}}`,
		`{"timestamp":"2026-01-01T00:00:08Z","type":"task_complete","payload":{"turn_id":"t2"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", home)
	t.Setenv("OPENCODE_HOME", t.TempDir())
	return home
}

func writeTestSkill(t *testing.T, directory, frontmatterName string) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := "---\n"
	if frontmatterName != "" {
		contents += "name: " + frontmatterName + "\n"
	}
	contents += "---\n\n# test skill\n"
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func usageHomeAt(t *testing.T, when time.Time) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(home, "sessions", when.UTC().Format("2006"), "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	stamp := func(offset time.Duration) string { return when.Add(offset).UTC().Format(time.RFC3339) }
	lines := []string{
		`{"timestamp":"` + stamp(0) + `","type":"session_meta","payload":{"id":"s","cli_version":"1"}}`,
		`{"timestamp":"` + stamp(time.Second) + `","type":"task_started","payload":{"turn_id":"t"}}`,
		`{"timestamp":"` + stamp(2*time.Second) + `","type":"user_message","payload":{"text":"$report"}}`,
		`{"timestamp":"` + stamp(3*time.Second) + `","type":"task_complete","payload":{"turn_id":"t"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", home)
	t.Setenv("OPENCODE_HOME", t.TempDir())
	return home
}

func TestRunCommandsAndMachineOutput(t *testing.T) {
	home := testHome(t)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"stats", "--color", "never"}, &stdout, &stderr); code != 0 {
		t.Fatalf("stats exit=%d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{"USAGE OVERVIEW", "Source: Codex (" + home + ")", "Agents: Codex", "Activity", "Sessions", "Turns", "User Prompts", "Tool Calls", "Skill Usage", "By turn", "By session", "Token Usage"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stats missing %q: %s", want, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), "CATSIFT") || strings.Contains(stdout.String(), " · ") {
		t.Fatalf("stats contains obsolete display: %s", stdout.String())
	}
	if strings.Contains(stdout.String(), "\x1b[") {
		t.Fatal("plain report contains ANSI")
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"stats", "--json", "--color", "always"}, &stdout, &stderr); code != 0 {
		t.Fatalf("json exit=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "\x1b[") {
		t.Fatal("JSON contains ANSI")
	}
	var value map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
		t.Fatalf("invalid JSON: %v (%s)", err, stdout.String())
	}
	if value["tool_calls"] != float64(1) {
		t.Fatalf("tool_calls=%v", value["tool_calls"])
	}
	stdout.Reset()
	if code := run([]string{"tools", "--color", "never"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "shell") || !strings.Contains(stdout.String(), "1 tool, 1 call total") {
		t.Fatalf("tools exit=%d output=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	if code := run([]string{"tools", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("tools JSON exit=%d stderr=%s", code, stderr.String())
	}
	var toolsValue struct {
		Rows []struct {
			Calls int `json:"calls"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &toolsValue); err != nil {
		t.Fatal(err)
	}
	toolTotal := 0
	for _, row := range toolsValue.Rows {
		toolTotal += row.Calls
	}
	stdout.Reset()
	if code := run([]string{"stats", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("stats JSON exit=%d stderr=%s", code, stderr.String())
	}
	var statsValue struct {
		ToolCalls int `json:"tool_calls"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &statsValue); err != nil {
		t.Fatal(err)
	}
	if toolTotal != statsValue.ToolCalls {
		t.Fatalf("overview/tools mismatch: %d != %d", statsValue.ToolCalls, toolTotal)
	}
	stdout.Reset()
	if code := run([]string{"skills", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("skills JSON exit=%d stderr=%s", code, stderr.String())
	}
	var skillsValue struct {
		Rows []struct {
			Total int `json:"total"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &skillsValue); err != nil {
		t.Fatal(err)
	}
	skillTotal := 0
	for _, row := range skillsValue.Rows {
		skillTotal += row.Total
	}
	stdout.Reset()
	if code := run([]string{"stats", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("stats JSON exit=%d stderr=%s", code, stderr.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), &statsValue); err != nil {
		t.Fatal(err)
	}
	var statsSkills struct {
		Turns            int `json:"turns"`
		SkillUsesTurn    int `json:"skill_uses_turn"`
		SkillUsesSession int `json:"skill_uses_session"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &statsSkills); err != nil {
		t.Fatal(err)
	}
	if statsSkills.Turns != 2 || skillTotal != statsSkills.SkillUsesTurn || statsSkills.SkillUsesSession > statsSkills.SkillUsesTurn {
		t.Fatalf("overview/skills mismatch: turns=%d turn=%d session=%d skills=%d", statsSkills.Turns, statsSkills.SkillUsesTurn, statsSkills.SkillUsesSession, skillTotal)
	}
}

func TestVerboseDiagnosticsIncludeRuntimeMetadata(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	home := testHome(t)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"stats", "--verbose", "--color", "never"}, &stdout, &stderr); code != 0 {
		t.Fatalf("stats exit=%d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{
		"catsift version:",
		"cache version:",
		"codex source: root=",
		"codex cache: summary",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("verbose diagnostics missing %q: %s", want, stderr.String())
		}
	}
	if !strings.Contains(stderr.String(), home) {
		t.Errorf("verbose diagnostics missing Codex root %q: %s", home, stderr.String())
	}
}

func TestMergeLoadedHistoriesCombinesSourceSnapshots(t *testing.T) {
	codexSource := usage.NewCodexSourceRef("codex", 1, "")
	opencodeSource := usage.NewOpenCodeSourceRef("opencode", "")
	codexTurn := usage.NewTurn("codex-session", "turn", 1, codexSource)
	opencodeTurn := usage.NewTurn("opencode-session", "turn", 1, opencodeSource)

	merged := mergeLoadedHistories(
		loadedHistory{
			Input:      query.Input{Turns: []usage.Turn{codexTurn}, Source: usage.SourceCodex},
			SourcePath: "/tmp/codex",
		},
		loadedHistory{
			Input:      query.Input{Turns: []usage.Turn{opencodeTurn}, Source: usage.SourceOpenCode},
			SourcePath: "/tmp/opencode",
		},
	)

	if len(merged.Turns) != 2 || merged.Source != "" {
		t.Fatalf("merged turns/source = %d/%q", len(merged.Turns), merged.Source)
	}
	if len(merged.Sources) != 2 || merged.Sources[0] != usage.SourceCodex || merged.Sources[1] != usage.SourceOpenCode {
		t.Fatalf("merged sources = %#v", merged.Sources)
	}
	if merged.SourcePaths[usage.SourceCodex] != "/tmp/codex" || merged.SourcePaths[usage.SourceOpenCode] != "/tmp/opencode" {
		t.Fatalf("merged source paths = %#v", merged.SourcePaths)
	}
}

func TestLoadAllHistoryRunsSourcesInParallelAndReportsTiming(t *testing.T) {
	sources := usage.DefaultSourceKinds()
	started := make(chan usage.SourceKind, len(sources))
	release := make(chan struct{})
	loader := func(options historyLoadOptions) (loadedHistory, error) {
		started <- options.Source
		options.Diagnostics.write("debug", "loader started")
		<-release
		return loadedHistory{Input: query.Input{Source: options.Source}}, nil
	}
	var diagnostics bytes.Buffer
	done := make(chan struct {
		history loadedHistory
		err     error
	}, 1)
	go func() {
		history, err := loadAllHistoryWith(historyLoadOptions{
			Verbose:     true,
			Sources:     sources,
			Diagnostics: newDiagnosticWriter(&diagnostics, output.ColorNever, true),
		}, loader)
		done <- struct {
			history loadedHistory
			err     error
		}{history: history, err: err}
	}()

	seen := make(map[usage.SourceKind]struct{}, len(sources))
	for range sources {
		select {
		case source := <-started:
			seen[source] = struct{}{}
		case <-time.After(time.Second):
			t.Fatal("source loaders did not start concurrently")
		}
	}
	close(release)

	result := <-done
	if result.err != nil {
		t.Fatalf("load all history: %v", result.err)
	}
	if len(seen) != len(sources) || len(result.history.Sources) != len(sources) {
		t.Fatalf("loaded sources = %#v, started = %#v", result.history.Sources, seen)
	}
	for _, source := range sources {
		if !strings.Contains(diagnostics.String(), "source "+string(source)+" loaded in ") {
			t.Errorf("timing missing for %s: %q", source, diagnostics.String())
		}
	}
	if !strings.Contains(diagnostics.String(), "selected history sources loaded in ") {
		t.Errorf("total timing missing: %q", diagnostics.String())
	}
}

func TestRunVersionFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("version exit=%d stderr=%s", code, stderr.String())
	}
	if got, want := stdout.String(), "catsift "+appversion.String()+"\n"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("version wrote stderr: %q", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"stats", "--version"}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "use catsift --version") {
		t.Fatalf("subcommand version exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunListsCLIReadModelCommandsAndScopedOptions(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("root help exit=%d stderr=%q", code, stderr.String())
	}
	for _, want := range []string{"activity", "models", "sessions"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("root help missing %q: %s", want, stdout.String())
		}
	}

	for _, test := range []struct {
		args []string
		want string
	}{
		{args: []string{"help", "models"}, want: "catsift models detail PROVIDER/NAME"},
		{args: []string{"help", "sessions"}, want: "catsift sessions detail ID"},
		{args: []string{"help", "activity"}, want: "Usage: catsift activity"},
		{args: []string{"help", "skills"}, want: "catsift skills detail NAME"},
		{args: []string{"help", "stats"}, want: "Usage: catsift stats"},
		{args: []string{"models", "detail", "--help"}, want: "Usage: catsift models detail PROVIDER/NAME"},
		{args: []string{"sessions", "detail", "--help"}, want: "Usage: catsift sessions detail ID"},
		{args: []string{"help", "skills", "detail"}, want: "Usage: catsift skills detail NAME"},
	} {
		stdout.Reset()
		stderr.Reset()
		if code := run(test.args, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), test.want) {
			t.Fatalf("args=%v exit=%d stdout=%q stderr=%q, want %q", test.args, code, stdout.String(), stderr.String(), test.want)
		}
		if strings.Contains(stdout.String(), "--trend") {
			t.Fatalf("args=%v still documents removed --trend option: %q", test.args, stdout.String())
		}
	}
}

func TestRunRejectsReadModelOptionsOutsideTheirCommands(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "removed trend option", args: []string{"stats", "--trend"}, want: "flag provided but not defined: -trend"},
		{name: "model option removed", args: []string{"models", "--model", "codex/gpt"}, want: "flag provided but not defined: -model"},
		{name: "skill option removed", args: []string{"skills", "--skill", "review"}, want: "flag provided but not defined: -skill"},
		{name: "session option removed", args: []string{"sessions", "--session", "session-1"}, want: "flag provided but not defined: -session"},
		{name: "detail group by", args: []string{"skills", "detail", "review", "--group-by", "session"}, want: "--group-by cannot be combined with skills detail"},
		{name: "detail unused", args: []string{"skills", "detail", "review", "--unused"}, want: "--unused cannot be combined with skills detail"},
		{name: "trend on TUI", args: []string{"--trend"}, want: "flag provided but not defined: -trend"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(test.args, &stdout, &stderr); code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("exit=%d stdout=%q stderr=%q, want %q", code, stdout.String(), stderr.String(), test.want)
			}
		})
	}
}

func syntheticQueryHistory() ctxHistoryLoader {
	return func(string, ctxsource.IngestOptions) (ctxsource.IngestResult, error) {
		firstWhen := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		secondWhen := firstWhen.Add(2 * time.Hour)
		model := usage.NewModelRef("codex", "gpt-example")
		firstSource := usage.NewCtxSourceRef("/private/history.jsonl", "codex", "provider-001", "ctx-001", "event-001")
		secondSource := usage.NewCtxSourceRef("/private/history.jsonl", "codex", "provider-002", "ctx-002", "event-002")
		firstSession := usage.NewSession("session-001", firstSource)
		firstSession.Title = "Review session"
		firstSession.ProjectPath = "/workspace/project"
		secondSession := usage.NewSession("session-002", secondSource)
		secondSession.ProjectPath = "/workspace/other"

		first := usage.NewTurn("session-001", "turn-001", 1, firstSource)
		first.StartedAt = firstWhen
		first.EndedAt = firstWhen.Add(time.Minute)
		first.UserPrompts = 1
		first.UserPromptTimes = []time.Time{firstWhen}
		first.AddTokenUsageForModelAt(model, firstWhen, usage.TokenUsage{InputTokens: 5, OutputTokens: 2, TotalTokens: 7})
		first.RuntimeTools = []usage.ToolObservation{{SessionID: "session-001", TurnID: "turn-001", RawName: "exec", CanonicalName: "shell", Arguments: "prompt-secret", Timestamp: firstWhen, Layer: usage.LayerRuntime, Status: usage.StatusSuccess, Source: firstSource}}
		first.SkillEvidence = []usage.SkillEvidence{usage.NewSkillEvidence("session-001", "turn-001", "review", usage.ModeExplicit, usage.MethodStructuredTool, usage.StateConfirmed, firstWhen, firstSource)}

		second := usage.NewTurn("session-001", "turn-002", 2, firstSource)
		second.StartedAt = secondWhen
		second.EndedAt = secondWhen.Add(time.Minute)
		second.UserPrompts = 1
		second.UserPromptTimes = []time.Time{secondWhen}
		second.AddTokenUsageForModelAt(model, secondWhen, usage.TokenUsage{InputTokens: 3, OutputTokens: 1, TotalTokens: 4})
		second.SkillEvidence = []usage.SkillEvidence{usage.NewSkillEvidence("session-001", "turn-002", "review", usage.ModeImplicit, usage.MethodImplicitAccess, usage.StateInferred, secondWhen, firstSource)}

		other := usage.NewTurn("session-002", "turn-003", 1, secondSource)
		other.StartedAt = secondWhen.Add(time.Hour)
		other.EndedAt = other.StartedAt.Add(time.Minute)
		other.ObserveModelAt(model, other.StartedAt, secondSource)

		return ctxsource.IngestResult{
			Turns:    []usage.Turn{first, second, other},
			Sessions: []usage.Session{firstSession, secondSession},
			Agents:   []string{"codex"},
		}, nil
	}
}

func TestRunQueryReportsUseTheTUIReadModel(t *testing.T) {
	loader := syntheticQueryHistory()
	var stdout, stderr bytes.Buffer
	if code := runWithCtxLoader([]string{"activity", "--source", "ctx", "--json"}, &stdout, &stderr, loader); code != 0 {
		t.Fatalf("activity exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var activity struct {
		Rows []struct {
			Date     string `json:"date"`
			Sessions int    `json:"sessions"`
			Turns    int    `json:"turns"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &activity); err != nil {
		t.Fatal(err)
	}
	if len(activity.Rows) != 1 || activity.Rows[0].Sessions != 2 || activity.Rows[0].Turns != 3 {
		t.Fatalf("activity = %#v", activity)
	}
	var activityDocument map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &activityDocument); err != nil {
		t.Fatal(err)
	}
	if _, ok := activityDocument["sessions"]; ok {
		t.Fatalf("activity unexpectedly includes stats overview: %s", stdout.String())
	}
	if strings.Contains(stdout.String(), "prompt-secret") || strings.Contains(stdout.String(), "history.jsonl") {
		t.Fatalf("activity leaked unsafe data: %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runWithCtxLoader([]string{"activity", "--source", "ctx", "--color", "never"}, &stdout, &stderr, loader); code != 0 {
		t.Fatalf("activity human exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "ACTIVITY") || !strings.Contains(stdout.String(), "Daily Trend") || !strings.Contains(stdout.String(), "2026-01-02") || strings.Contains(stdout.String(), "USAGE OVERVIEW") {
		t.Fatalf("activity human report = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runWithCtxLoader([]string{"stats", "--source", "ctx", "--json"}, &stdout, &stderr, loader); code != 0 {
		t.Fatalf("stats exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var stats map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	if _, ok := stats["trend"]; ok {
		t.Fatalf("stats unexpectedly includes removed trend: %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runWithCtxLoader([]string{"models", "--source", "ctx", "--json"}, &stdout, &stderr, loader); code != 0 {
		t.Fatalf("models exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var models struct {
		Rows []struct {
			Model struct {
				Provider string `json:"provider"`
				Name     string `json:"name"`
			} `json:"model"`
			Sessions int `json:"sessions"`
			Turns    int `json:"turns"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &models); err != nil {
		t.Fatal(err)
	}
	if len(models.Rows) != 1 || models.Rows[0].Model.Provider != "codex" || models.Rows[0].Model.Name != "gpt-example" || models.Rows[0].Sessions != 2 || models.Rows[0].Turns != 3 {
		t.Fatalf("models = %#v", models)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runWithCtxLoader([]string{"models", "detail", "CODEX/GPT-EXAMPLE", "--source", "ctx", "--json"}, &stdout, &stderr, loader); code != 0 {
		t.Fatalf("model detail exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var modelDetail struct {
		Summary struct {
			Turns int `json:"turns"`
		} `json:"summary"`
		Sessions []struct {
			Project string `json:"project"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &modelDetail); err != nil {
		t.Fatal(err)
	}
	if modelDetail.Summary.Turns != 3 || len(modelDetail.Sessions) != 2 || !strings.Contains(stdout.String(), "/workspace/project") {
		t.Fatalf("model detail = %#v", modelDetail)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runWithCtxLoader([]string{"skills", "detail", "REVIEW", "--source", "ctx", "--json"}, &stdout, &stderr, loader); code != 0 {
		t.Fatalf("skill detail exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var skillDetail struct {
		Summary struct {
			Uses      int `json:"uses"`
			Sessions  int `json:"sessions"`
			Confirmed int `json:"confirmed"`
			Inferred  int `json:"inferred"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &skillDetail); err != nil {
		t.Fatal(err)
	}
	if skillDetail.Summary.Uses != 2 || skillDetail.Summary.Sessions != 1 || skillDetail.Summary.Confirmed != 1 || skillDetail.Summary.Inferred != 1 {
		t.Fatalf("skill detail = %#v", skillDetail)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runWithCtxLoader([]string{"skills", "detail", "review", "--source", "ctx", "--strict", "--json"}, &stdout, &stderr, loader); code != 0 {
		t.Fatalf("strict skill detail exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), &skillDetail); err != nil {
		t.Fatal(err)
	}
	if skillDetail.Summary.Uses != 1 || skillDetail.Summary.Confirmed != 1 || skillDetail.Summary.Inferred != 0 {
		t.Fatalf("strict skill detail = %#v", skillDetail)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runWithCtxLoader([]string{"sessions", "detail", "session-001", "--source", "ctx", "--json"}, &stdout, &stderr, loader); code != 0 {
		t.Fatalf("session detail exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var sessionDetail struct {
		Summary struct {
			ID string `json:"id"`
		} `json:"summary"`
		Turns []struct {
			ID     string   `json:"id"`
			Tools  []string `json:"tools"`
			Skills []string `json:"skills"`
			Status string   `json:"status"`
		} `json:"turns"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &sessionDetail); err != nil {
		t.Fatal(err)
	}
	if sessionDetail.Summary.ID != "session-001" || len(sessionDetail.Turns) != 2 || sessionDetail.Turns[0].ID != "turn-001" || sessionDetail.Turns[0].Tools[0] != "shell" || sessionDetail.Turns[0].Skills[0] != "review" || sessionDetail.Turns[0].Status != "done" {
		t.Fatalf("session detail = %#v", sessionDetail)
	}
	if strings.Contains(stdout.String(), "prompt-secret") || strings.Contains(stdout.String(), "history.jsonl") {
		t.Fatalf("session detail leaked unsafe data: %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runWithCtxLoader([]string{"sessions", "detail", "session", "--source", "ctx", "--json"}, &stdout, &stderr, loader); code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "ambiguous") || !strings.Contains(stderr.String(), "session-001") || !strings.Contains(stderr.String(), "session-002") {
		t.Fatalf("ambiguous session exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runWithCtxLoader([]string{"models", "detail", "codex/missing", "--source", "ctx", "--json"}, &stdout, &stderr, loader); code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "was not found") {
		t.Fatalf("missing model exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	warningLoader := func(root string, options ctxsource.IngestOptions) (ctxsource.IngestResult, error) {
		result, err := loader(root, options)
		result.Warnings = []usage.Warning{{Reason: "malformed_json", Path: "/private/history.jsonl", Line: 7, Count: 1}}
		return result, err
	}
	stdout.Reset()
	stderr.Reset()
	if code := runWithCtxLoader([]string{"sessions", "--source", "ctx", "--json"}, &stdout, &stderr, warningLoader); code != 0 {
		t.Fatalf("warning session list exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var warningValue map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &warningValue); err != nil {
		t.Fatalf("warning query stdout is not JSON: %v (%s)", err, stdout.String())
	}
	if strings.Contains(stdout.String(), "malformed_json") || !strings.Contains(stderr.String(), "warning: skipped 1 record") {
		t.Fatalf("warning routing stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunDefaultsToTUIWithoutCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "interactive terminal") || !strings.Contains(stderr.String(), "catsift stats") {
		t.Fatalf("default TUI exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--json"}, &stdout, &stderr); code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "--json is not supported for the interactive view") {
		t.Fatalf("default TUI option exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunRejectsRemovedTUICommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"tui"}, &stdout, &stderr); code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), `unknown command "tui"`) {
		t.Fatalf("removed tui command exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunValidatesExclusiveHistorySources(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "invalid source", args: []string{"stats", "--source", "sqlite"}, want: "invalid --source"},
		{name: "days and range", args: []string{"stats", "--days", "1", "--from", "2026-01-01"}, want: "cannot be combined"},
		{name: "reversed range", args: []string{"stats", "--from", "2026-01-02", "--to", "2026-01-01"}, want: "must not be after"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(tt.args, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), tt.want) {
				t.Fatalf("exit=%d stdout=%q stderr=%q, want %q", code, stdout.String(), stderr.String(), tt.want)
			}
		})
	}
}

func TestParseSourceSelectionAcceptsMultipleSources(t *testing.T) {
	got, err := parseSourceSelection([]string{"opencode,codex", "ctx", "codex"})
	if err != nil {
		t.Fatal(err)
	}
	want := []usage.SourceKind{usage.SourceCodex, usage.SourceOpenCode, usage.SourceCtx}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sources = %#v, want %#v", got, want)
	}

	if _, err := parseSourceSelection([]string{"codex,sqlite"}); err == nil || !strings.Contains(err.Error(), "invalid --source") {
		t.Fatalf("invalid source error = %v", err)
	}
}

func TestLoadAllHistoryUsesSelectedSources(t *testing.T) {
	selected := []usage.SourceKind{usage.SourceCodex, usage.SourceOpenCode}
	started := make(chan usage.SourceKind, len(usage.AllSourceKinds()))
	loader := func(options historyLoadOptions) (loadedHistory, error) {
		started <- options.Source
		return loadedHistory{Input: query.Input{Source: options.Source}}, nil
	}
	history, err := loadAllHistoryWith(historyLoadOptions{Sources: selected}, loader)
	if err != nil {
		t.Fatal(err)
	}
	close(started)
	var got []usage.SourceKind
	for source := range started {
		got = append(got, source)
	}
	if len(got) != len(selected) || !reflect.DeepEqual(history.Sources, selected) {
		t.Fatalf("selected sources = started:%#v history:%#v, want %#v", got, history.Sources, selected)
	}
	seen := make(map[usage.SourceKind]bool, len(got))
	for _, source := range got {
		seen[source] = true
	}
	for _, source := range selected {
		if !seen[source] {
			t.Fatalf("source %s was not loaded: %#v", source, got)
		}
	}
}

func TestRunDefaultsAndSelectsMultipleSources(t *testing.T) {
	testHome(t)
	writeOpenCodeHome(t)
	for _, args := range [][]string{
		{"stats", "--json"},
		{"stats", "--source", "codex,opencode", "--json"},
		{"stats", "--source", "codex", "--source", "opencode", "--json"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 0 {
			t.Fatalf("args=%v exit=%d stdout=%s stderr=%s", args, code, stdout.String(), stderr.String())
		}
		var value struct {
			Source   string             `json:"source"`
			Sources  []usage.SourceKind `json:"sources"`
			Sessions int                `json:"sessions"`
			Turns    int                `json:"turns"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
			t.Fatalf("args=%v invalid JSON: %v (%s)", args, err, stdout.String())
		}
		wantSources := []usage.SourceKind{usage.SourceCodex, usage.SourceOpenCode}
		if value.Source != "" || !reflect.DeepEqual(value.Sources, wantSources) || value.Sessions != 2 || value.Turns != 3 {
			t.Fatalf("args=%v stats = %#v", args, value)
		}
	}
}

func TestRunOpenCodeSourceProducesJSONAndHumanReports(t *testing.T) {
	root := writeOpenCodeHome(t)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"stats", "--source", "opencode", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("OpenCode stats JSON exit=%d stderr=%s", code, stderr.String())
	}
	var stats struct {
		Source      string   `json:"source"`
		Agents      []string `json:"agents"`
		Sessions    int      `json:"sessions"`
		Turns       int      `json:"turns"`
		UserPrompts int      `json:"user_prompts"`
		ToolCalls   int      `json:"tool_calls"`
		InputTokens int64    `json:"input_tokens"`
		TotalTokens int64    `json:"total_tokens"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.Source != "opencode" || strings.Join(stats.Agents, ",") != "opencode" || stats.Sessions != 1 || stats.Turns != 1 || stats.UserPrompts != 1 || stats.ToolCalls != 1 || stats.InputTokens != 5 || stats.TotalTokens != 7 {
		t.Fatalf("OpenCode stats = %#v", stats)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"tools", "--source", "opencode", "--color", "never"}, &stdout, &stderr); code != 0 {
		t.Fatalf("OpenCode tools exit=%d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{"Source: OpenCode (" + root + ")", "Agents: OpenCode", "shell"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("OpenCode human report missing %q: %s", want, stdout.String())
		}
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"skills", "--source", "opencode", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("OpenCode skills JSON exit=%d stderr=%s", code, stderr.String())
	}
	var skills struct {
		Rows []struct {
			Name  string `json:"name"`
			Total int    `json:"total"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &skills); err != nil {
		t.Fatal(err)
	}
	if len(skills.Rows) != 1 || skills.Rows[0].Name != "review" || skills.Rows[0].Total != 1 {
		t.Fatalf("OpenCode skills = %#v", skills)
	}

	skillRoot := t.TempDir()
	writeTestSkill(t, filepath.Join(skillRoot, ".agents", "skills", "review"), "review")
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"skills", "--source", "opencode", "--unused", "--root", skillRoot, "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("OpenCode unused skills JSON exit=%d stderr=%s", code, stderr.String())
	}
	var unused struct {
		Source         string `json:"source"`
		UnusedCount    int    `json:"unused_count"`
		InstalledCount int    `json:"installed_count"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &unused); err != nil {
		t.Fatal(err)
	}
	if unused.Source != "opencode" || unused.InstalledCount != 1 || unused.UnusedCount != 0 {
		t.Fatalf("OpenCode unused skills = %#v", unused)
	}

	emptyRoot := t.TempDir()
	t.Setenv("OPENCODE_HOME", emptyRoot)
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"stats", "--source", "opencode", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("empty OpenCode source exit=%d stderr=%s", code, stderr.String())
	}
	var empty struct {
		Sessions int `json:"sessions"`
		Turns    int `json:"turns"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &empty); err != nil {
		t.Fatal(err)
	}
	if empty.Sessions != 0 || empty.Turns != 0 {
		t.Fatalf("empty OpenCode result = %#v", empty)
	}
}

func TestRunOpenCodeWarningsStayOnStderrAndStrictInputFails(t *testing.T) {
	root := writeOpenCodeHome(t)
	database, err := sql.Open("sqlite", filepath.Join(root, "opencode.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, "bad", "m1", "s1", int64(1_700_000_004_000), int64(1_700_000_004_000), `{not-json}`); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"stats", "--source", "opencode", "--json", "--strict-input"}, &stdout, &stderr); code != 1 {
		t.Fatalf("strict OpenCode exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var value map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
		t.Fatalf("stdout is not standalone JSON: %v (%s)", err, stdout.String())
	}
	if !strings.Contains(stderr.String(), "input diagnostics encountered") || strings.Contains(stdout.String(), "opencode_malformed_part") {
		t.Fatalf("warning routing = stdout %q stderr %q", stdout.String(), stderr.String())
	}
}

func TestRunOpenCodeAcceptsCommonReportOptions(t *testing.T) {
	writeOpenCodeHome(t)
	for _, args := range [][]string{
		{"stats", "--source", "opencode", "--days", "1", "--json"},
		{"stats", "--source", "opencode", "--from", "2023-11-14", "--to", "2023-11-14", "--json"},
		{"tools", "--source", "opencode", "--layer", "runtime", "--json"},
		{"skills", "--source", "opencode", "--group-by", "session", "--strict", "--json"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 0 {
			t.Fatalf("OpenCode options %v exit=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
		}
	}
}

func writeOpenCodeHome(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	database, err := sql.Open("sqlite", filepath.Join(root, "opencode.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	for _, statement := range []string{
		`CREATE TABLE session (id TEXT PRIMARY KEY, directory TEXT, version TEXT, time_created INTEGER, time_updated INTEGER)`,
		`CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT, time_created INTEGER, time_updated INTEGER, data TEXT)`,
		`CREATE TABLE part (id TEXT PRIMARY KEY, message_id TEXT, session_id TEXT, time_created INTEGER, time_updated INTEGER, data TEXT)`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	rows := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO session VALUES (?, ?, ?, ?, ?)`, []any{"s1", "/workspace/project", "1.18.27", int64(1_700_000_000_000), int64(1_700_000_010_000)}},
		{`INSERT INTO message VALUES (?, ?, ?, ?, ?)`, []any{"m1", "s1", int64(1_700_000_001_000), int64(1_700_000_001_000), `{"role":"user"}`}},
		{`INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, []any{"p1", "m1", "s1", int64(1_700_000_001_000), int64(1_700_000_001_000), `{"type":"text","text":"$review"}`}},
		{`INSERT INTO part VALUES (?, ?, ?, ?, ?, ?)`, []any{"p2", "m1", "s1", int64(1_700_000_002_000), int64(1_700_000_002_000), `{"type":"tool","tool":"bash","callID":"call-1","state":{"status":"completed","input":{"command":"true"}}}`}},
		{`INSERT INTO message VALUES (?, ?, ?, ?, ?)`, []any{"m2", "s1", int64(1_700_000_003_000), int64(1_700_000_003_000), `{"role":"assistant","tokens":{"input":5,"output":2,"total":7}}`}},
	}
	for _, row := range rows {
		if _, err := database.Exec(row.query, row.args...); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("OPENCODE_HOME", root)
	return root
}

func TestRunDateRangeFiltersCodexHistory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", home)
	t.Setenv("OPENCODE_HOME", t.TempDir())
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
	writeSession("old.jsonl", "old", "2026-01-01T23:59:59Z")
	writeSession("selected.jsonl", "selected", "2026-01-02T12:00:00Z")

	var stdout, stderr bytes.Buffer
	code := run([]string{"stats", "--from", "2026-01-02", "--to", "2026-01-02", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var value struct {
		Period   string `json:"period"`
		Sessions int    `json:"sessions"`
		Turns    int    `json:"turns"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if value.Period != "2026-01-02 to 2026-01-02" || value.Sessions != 1 || value.Turns != 1 {
		t.Fatalf("date range stats = %#v", value)
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"stats", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("all-time stats exit=%d stderr=%s", code, stderr.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if value.Period != "2026-01-01 to 2026-01-02" {
		t.Fatalf("all-time period = %q", value.Period)
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"stats", "--from", "2026-01-02", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("open-start date range exit=%d stderr=%s", code, stderr.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if value.Period != "2026-01-02 to 2026-01-02" {
		t.Fatalf("open-start date range period = %q", value.Period)
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"stats", "--from", "2025-12-01", "--to", "2026-01-02", "--color", "never"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("partial date range exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "info: selected period starts before the first usage record (2026-01-01)") {
		t.Fatalf("partial date range info = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"stats", "--to", "2026-01-02", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("open-end date range exit=%d stderr=%s", code, stderr.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if value.Period != "2026-01-01 to 2026-01-02" {
		t.Fatalf("open-end date range period = %q", value.Period)
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"stats", "--from", "2027-01-01", "--color", "never"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("empty date range exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "info: No usage found for the selected period.") {
		t.Fatalf("empty date range info = %q", stdout.String())
	}
}

func TestFormatPeriodUsesActualTurnRange(t *testing.T) {
	turns := []usage.Turn{
		{
			StartedAt: time.Date(2026, 1, 2, 23, 0, 0, 0, time.UTC),
			EndedAt:   time.Date(2026, 1, 3, 1, 0, 0, 0, time.UTC),
		},
		{
			StartedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
			EndedAt:   time.Date(2026, 1, 1, 13, 0, 0, 0, time.UTC),
		},
	}
	if got := formatPeriod(turns); got != "2026-01-01 to 2026-01-03" {
		t.Fatalf("formatPeriod() = %q, want actual turn range", got)
	}
	if got := formatPeriod(nil); got != "no data" {
		t.Fatalf("formatPeriod(empty) = %q, want no data", got)
	}
}

func TestFormatPeriodInfoDescribesRequestedRangeMismatch(t *testing.T) {
	turns := []usage.Turn{{
		StartedAt: time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC),
		EndedAt:   time.Date(2026, 2, 1, 13, 0, 0, 0, time.UTC),
	}}
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	if got := formatPeriodInfo(turns, false, 0, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC), now); got != "selected period starts before the first usage record (2026-02-01)" {
		t.Fatalf("start mismatch info = %q", got)
	}
	if got := formatPeriodInfo(turns, false, 0, time.Time{}, time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC), now); got != "selected period ends after the last usage record (2026-02-01)" {
		t.Fatalf("end mismatch info = %q", got)
	}
	if got := formatPeriodInfo(turns, false, 0, time.Time{}, time.Time{}, now); got != "" {
		t.Fatalf("unfiltered period info = %q, want empty", got)
	}
	if got := formatPeriodInfo(nil, true, 30, time.Time{}, time.Time{}, now); got != "" {
		t.Fatalf("empty period info = %q, want empty", got)
	}
}

func TestRunCtxSourceAggregatesAgents(t *testing.T) {
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	turns := []usage.Turn{
		{
			SessionID:    "ctx\x00codex\x00session",
			UserPrompts:  1,
			Source:       usage.SourceRef{Source: usage.SourceCtx, Agent: "codex"},
			RuntimeTools: []usage.ToolObservation{{CanonicalName: "shell", Status: usage.StatusSuccess, Timestamp: stamp}},
			SkillEvidence: []usage.SkillEvidence{
				usage.NewSkillEvidence("ctx\x00codex\x00session", "turn", "review", usage.ModeExplicit, usage.MethodStructuredTool, usage.StateConfirmed, stamp, usage.SourceRef{Source: usage.SourceCtx, Agent: "codex"}),
			},
		},
		{
			SessionID:    "ctx\x00opencode\x00session",
			UserPrompts:  1,
			Source:       usage.SourceRef{Source: usage.SourceCtx, Agent: "opencode"},
			RuntimeTools: []usage.ToolObservation{{CanonicalName: "shell", Status: usage.StatusFailure, Timestamp: stamp.Add(time.Second)}},
			SkillEvidence: []usage.SkillEvidence{
				usage.NewSkillEvidence("ctx\x00opencode\x00session", "turn", "review", usage.ModeExplicit, usage.MethodStructuredTool, usage.StateConfirmed, stamp.Add(time.Second), usage.SourceRef{Source: usage.SourceCtx, Agent: "opencode"}),
			},
		},
	}
	loader := func(root string, options ctxsource.IngestOptions) (ctxsource.IngestResult, error) {
		if root != "" || options.DataRoot != "" {
			t.Fatalf("ctx root = %q options = %#v", root, options)
		}
		return ctxsource.IngestResult{Turns: turns, Sessions: []ctxsource.SessionMetadata{{ID: turns[0].SessionID}, {ID: turns[1].SessionID}}, Agents: []string{"opencode", "codex"}}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runWithCtxLoader([]string{"stats", "--source", "ctx", "--json"}, &stdout, &stderr, loader); code != 0 {
		t.Fatalf("ctx stats exit=%d stderr=%s", code, stderr.String())
	}
	var stats struct {
		Source           string   `json:"source"`
		Agents           []string `json:"agents"`
		Agent            string   `json:"agent"`
		Sessions         int      `json:"sessions"`
		Turns            int      `json:"turns"`
		UserPrompts      int      `json:"user_prompts"`
		ToolCalls        int      `json:"tool_calls"`
		SkillUsesTurn    int      `json:"skill_uses_turn"`
		SkillUsesSession int      `json:"skill_uses_session"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.Source != "ctx" || strings.Join(stats.Agents, ",") != "codex,opencode" || stats.Agent != "codex,opencode" || stats.Sessions != 2 || stats.Turns != 2 || stats.UserPrompts != 2 || stats.ToolCalls != 2 || stats.SkillUsesTurn != 2 || stats.SkillUsesSession != 2 {
		t.Fatalf("ctx stats = %#v", stats)
	}

	stdout.Reset()
	if code := runWithCtxLoader([]string{"tools", "--source", "ctx", "--json"}, &stdout, &stderr, loader); code != 0 {
		t.Fatalf("ctx tools exit=%d stderr=%s", code, stderr.String())
	}
	var toolsValue struct {
		Rows []struct {
			Name     string `json:"name"`
			Calls    int    `json:"calls"`
			Failures int    `json:"failures"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &toolsValue); err != nil {
		t.Fatal(err)
	}
	if len(toolsValue.Rows) != 1 || toolsValue.Rows[0].Name != "shell" || toolsValue.Rows[0].Calls != 2 || toolsValue.Rows[0].Failures != 1 {
		t.Fatalf("ctx tools = %#v", toolsValue)
	}

	stdout.Reset()
	if code := runWithCtxLoader([]string{"skills", "--source", "ctx", "--json"}, &stdout, &stderr, loader); code != 0 {
		t.Fatalf("ctx skills exit=%d stderr=%s", code, stderr.String())
	}
	var skillsValue struct {
		Rows []struct {
			Name  string `json:"name"`
			Total int    `json:"total"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &skillsValue); err != nil {
		t.Fatal(err)
	}
	if len(skillsValue.Rows) != 1 || skillsValue.Rows[0].Name != "review" || skillsValue.Rows[0].Total != 2 {
		t.Fatalf("ctx skills = %#v", skillsValue)
	}
}

func TestRunCtxVerboseReportsSourceDiagnosticsOnStderr(t *testing.T) {
	loader := func(string, ctxsource.IngestOptions) (ctxsource.IngestResult, error) {
		return ctxsource.IngestResult{}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runWithCtxLoader([]string{"stats", "--source", "ctx", "--verbose", "--json"}, &stdout, &stderr, func(root string, options ctxsource.IngestOptions) (ctxsource.IngestResult, error) {
		if options.Diagnostic == nil {
			t.Fatal("ctx diagnostic callback is nil in verbose mode")
		}
		options.Diagnostic("ctx cache: hit; using complete cached history")
		return loader(root, options)
	}); code != 0 {
		t.Fatalf("ctx verbose exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "debug: ctx cache: hit; using complete cached history") {
		t.Fatalf("ctx cache diagnostic missing from stderr: %q", stderr.String())
	}
	if strings.Contains(stdout.String(), "ctx cache:") || strings.Contains(stdout.String(), "info:") {
		t.Fatalf("ctx cache diagnostic leaked into stdout: %q", stdout.String())
	}
}

func TestRunCtxStrictInputKeepsJSONAndReturnsNonZero(t *testing.T) {
	loader := func(string, ctxsource.IngestOptions) (ctxsource.IngestResult, error) {
		return ctxsource.IngestResult{Warnings: []usage.Warning{{Reason: "ctx_unknown_event", Type: "future", Count: 1}}}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runWithCtxLoader([]string{"stats", "--source", "ctx", "--json", "--strict-input"}, &stdout, &stderr, loader); code != 1 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var value map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
		t.Fatalf("stdout is not JSON: %v (%s)", err, stdout.String())
	}
	if !strings.Contains(stderr.String(), "strict-input") {
		t.Fatalf("strict-input diagnostic missing: %s", stderr.String())
	}
}

func TestRunCtxUnusedSkillsKeepsPhysicalRowsAndUsesAgentUnion(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	writeTestSkill(t, filepath.Join(first, ".agents", "skills", "review"), "review")
	writeTestSkill(t, filepath.Join(second, ".codex", "skills", "review"), "review")
	loader := func(string, ctxsource.IngestOptions) (ctxsource.IngestResult, error) {
		return ctxsource.IngestResult{Agents: []string{"codex", "opencode"}}, nil
	}
	var stdout, stderr bytes.Buffer
	args := []string{"skills", "--source", "ctx", "--unused", "--root", first, "--root", second, "--json"}
	if code := runWithCtxLoader(args, &stdout, &stderr, loader); code != 0 {
		t.Fatalf("unused ctx exit=%d stderr=%s", code, stderr.String())
	}
	var value struct {
		Source         string   `json:"source"`
		Agents         []string `json:"agents"`
		InstalledCount int      `json:"installed_count"`
		UnusedCount    int      `json:"unused_count"`
		Rows           []struct {
			Path string `json:"path"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if value.Source != "ctx" || strings.Join(value.Agents, ",") != "codex,opencode" || value.InstalledCount != 2 || value.UnusedCount != 2 || len(value.Rows) != 2 {
		t.Fatalf("unused ctx physical rows = %#v", value)
	}
	if value.Rows[0].Path >= value.Rows[1].Path {
		t.Fatalf("unused rows are not path sorted = %#v", value.Rows)
	}

	stdout.Reset()
	usedTurn := usage.Turn{SessionID: "ctx\x00codex\x00session", SkillEvidence: []usage.SkillEvidence{
		usage.NewSkillEvidence("ctx\x00codex\x00session", "turn", "review", usage.ModeExplicit, usage.MethodStructuredTool, usage.StateConfirmed, time.Unix(1, 0), usage.SourceRef{Source: usage.SourceCtx, Agent: "codex"}),
	}}
	loader = func(string, ctxsource.IngestOptions) (ctxsource.IngestResult, error) {
		return ctxsource.IngestResult{Turns: []usage.Turn{usedTurn}, Sessions: []ctxsource.SessionMetadata{{ID: usedTurn.SessionID}}, Agents: []string{"codex", "opencode"}}, nil
	}
	if code := runWithCtxLoader(args, &stdout, &stderr, loader); code != 0 {
		t.Fatalf("used unused ctx exit=%d stderr=%s", code, stderr.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if value.UnusedCount != 0 || len(value.Rows) != 0 {
		t.Fatalf("used name should remove both physical rows = %#v", value)
	}
}

func TestRunHelpDocumentsHistorySourceOptions(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"stats", "--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("help exit=%d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{"--source SOURCE", "codex, copilot, opencode, or ctx", "--days N", "--from DATE", "--to DATE", "input and cache diagnostic details"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("help missing %q: %s", want, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), "--github-copilot-home") {
		t.Fatalf("help contains removed GitHub Copilot root option: %s", stdout.String())
	}
}

func TestRunLoadsGitHubCopilotFromDefaultHome(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".copilot")
	t.Setenv("HOME", home)
	path := filepath.Join(root, "session-state", "session-001", "events.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"id":"start","timestamp":"2026-01-02T00:00:00Z","type":"session.start","data":{"version":"1.0"}}`,
		`{"id":"turn","timestamp":"2026-01-02T00:00:01Z","type":"model.turn_started","data":{"turnId":"turn-001","model":"copilot-model"}}`,
		`{"id":"prompt","timestamp":"2026-01-02T00:00:02Z","type":"user.message","data":{"role":"user","content":"synthetic prompt"}}`,
		`{"id":"tool","timestamp":"2026-01-02T00:00:03Z","type":"tool.execution_complete","data":{"turnId":"turn-001","toolCallId":"tool-001","toolName":"shell","status":"success"}}`,
		`{"id":"end","timestamp":"2026-01-02T00:00:04Z","type":"model.turn_ended","data":{"turnId":"turn-001"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	if code := run([]string{"stats", "--source", "copilot", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("GitHub Copilot stats exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var report struct {
		Source string   `json:"source"`
		Agents []string `json:"agents"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid GitHub Copilot JSON: %v; stdout=%s", err, stdout.String())
	}
	if report.Source != "copilot" || !reflect.DeepEqual(report.Agents, []string{"copilot"}) {
		t.Fatalf("GitHub Copilot report metadata = %#v", report)
	}
	if strings.Contains(stdout.String(), "synthetic prompt") || strings.Contains(stdout.String(), "tool payload") {
		t.Fatalf("raw GitHub Copilot content leaked to stdout: %s", stdout.String())
	}
	for _, command := range []string{"activity", "models", "tools", "skills", "sessions"} {
		stdout.Reset()
		stderr.Reset()
		if code := run([]string{command, "--source", "copilot", "--json"}, &stdout, &stderr); code != 0 {
			t.Fatalf("GitHub Copilot %s exit=%d stdout=%s stderr=%s", command, code, stdout.String(), stderr.String())
		}
		var document any
		if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
			t.Fatalf("GitHub Copilot %s JSON: %v; stdout=%s", command, err, stdout.String())
		}
		if strings.Contains(stdout.String(), "synthetic prompt") || strings.Contains(stdout.String(), "tool payload") {
			t.Fatalf("raw GitHub Copilot %s content leaked to stdout: %s", command, stdout.String())
		}
	}
}

func TestRunAutoDetectsGitHubCopilotHistory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("OPENCODE_HOME", t.TempDir())
	path := filepath.Join(home, ".copilot", "session-state", "session-001", "events.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"id":"start","timestamp":"2026-01-02T00:00:00Z","type":"session.start","data":{"version":"1.0"}}`+"\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	var stdout, stderr bytes.Buffer
	ctxCalled := false
	loadCtx := func(string, ctxsource.IngestOptions) (ctxsource.IngestResult, error) {
		ctxCalled = true
		return ctxsource.IngestResult{}, nil
	}
	if code := runWithCtxLoader([]string{"stats", "--json"}, &stdout, &stderr, loadCtx); code != 0 {
		t.Fatalf("GitHub Copilot auto-detection exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if ctxCalled {
		t.Fatal("ctx was loaded during automatic source detection")
	}
	var report struct {
		Source   usage.SourceKind `json:"source"`
		Sessions int              `json:"sessions"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid GitHub Copilot auto-detection JSON: %v; stdout=%s", err, stdout.String())
	}
	if report.Source != usage.SourceCopilot || report.Sessions != 1 {
		t.Fatalf("GitHub Copilot auto-detection report = %#v", report)
	}
}

func TestRunRejectsRemovedHistoryRootOptions(t *testing.T) {
	for _, option := range []string{"--codex-home", "--ctx-data-root", "--opencode-home", "--github-copilot-home"} {
		t.Run(option, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run([]string{"stats", option, t.TempDir()}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "flag provided but not defined") {
				t.Fatalf("removed option exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunInteractiveViewRejectsJSONAndNonInteractiveOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--json"}, &stdout, &stderr); code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "--json is not supported for the interactive view") {
		t.Fatalf("interactive view json exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	for _, args := range [][]string{{"--days", "1"}, {"--from", "2026-01-01"}, {"--to", "2026-01-31"}} {
		stdout.Reset()
		stderr.Reset()
		if code := run(args, &stdout, &stderr); code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "not supported for the interactive view") {
			t.Fatalf("interactive view period option %v exit=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
		}
	}
}

func TestRunInteractiveViewHelpDocumentsInteractiveScope(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--source", "codex", "--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("interactive view help exit=%d stderr=%q", code, stderr.String())
	}
	for _, want := range []string{"catsift [options]", "model", "skill", "session", "--strict-input", "d", "set period", "read-only", "interactive terminal"} {
		if !strings.Contains(strings.ToLower(stdout.String()), strings.ToLower(want)) {
			t.Errorf("interactive view help missing %q: %s", want, stdout.String())
		}
	}
	for _, unwanted := range []string{"--days", "--from", "--to"} {
		if strings.Contains(stdout.String(), unwanted) {
			t.Errorf("interactive view help contains CLI-only option %q: %s", unwanted, stdout.String())
		}
	}
}

func TestLoadHistoryBuildsQueryInputWithSessionMetadata(t *testing.T) {
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	source := usage.NewCodexSourceRef("history", 1, "1")
	session := usage.NewSession("session", source)
	session.ProjectPath = "/workspace/project"
	turn := usage.NewTurn("session", "turn", 1, source)
	turn.StartedAt = stamp
	var observedRoot string
	history, err := loadHistory(historyLoadOptions{
		Source: usage.SourceCtx,
		LoadCtx: func(root string, options ctxsource.IngestOptions) (ctxsource.IngestResult, error) {
			observedRoot = root
			return ctxsource.IngestResult{Turns: []usage.Turn{turn}, Sessions: []ctxsource.SessionMetadata{session}, Agents: []string{"codex"}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if observedRoot != "" || len(history.Sessions) != 1 || history.Sessions[0].ProjectPath != "/workspace/project" || history.Source != usage.SourceCtx {
		t.Fatalf("loaded query input = %#v", history)
	}
}

func TestRunHelpIsScopedToCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("root help exit=%d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{"Usage:", "catsift [options]", "catsift <command> [options]", "Options:", "--source SOURCE", "--verbose", "--strict-input", "stats", "tools", "skills", "  --help            Show this help", "  --version         Show the catsift version", "Run \"catsift <command> --help\" for command-specific options."} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("root help missing %q: %s", want, stdout.String())
		}
	}
	for _, unwanted := range []string{"Usage options:", "Report options:", "Default:", "  catsift --help\n", "  catsift --version\n", "catsift tui", "tui       Explore usage interactively"} {
		if strings.Contains(stdout.String(), unwanted) {
			t.Errorf("root help contains obsolete section or usage %q: %s", unwanted, stdout.String())
		}
	}
	for _, unwanted := range []string{"--days", "--from", "--to", "--layer", "--strict", "--json"} {
		if helpContainsOption(stdout.String(), unwanted) {
			t.Errorf("root help contains command option %q: %s", unwanted, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), "Usage Explorer") {
		t.Errorf("root help contains obsolete product name: %s", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("root help wrote stderr: %q", stderr.String())
	}

	for _, test := range []struct {
		command string
		want    []string
		omit    []string
	}{
		{command: "stats", want: []string{"Usage: catsift stats [options]", "--days"}, omit: []string{"--layer", "--strict", "--group-by"}},
		{command: "tools", want: []string{"Usage: catsift tools [options]", "--days", "--layer"}, omit: []string{"--group-by", "--strict"}},
		{command: "skills", want: []string{"Usage: catsift skills [options]", "--days", "--group-by", "--strict", "--unused", "--root", "--view"}, omit: []string{"--layer"}},
	} {
		stdout.Reset()
		stderr.Reset()
		if code := run([]string{test.command, "--help"}, &stdout, &stderr); code != 0 {
			t.Fatalf("%s help exit=%d stderr=%s", test.command, code, stderr.String())
		}
		for _, want := range test.want {
			if !strings.Contains(stdout.String(), want) {
				t.Errorf("%s help missing %q: %s", test.command, want, stdout.String())
			}
		}
		for _, unwanted := range test.omit {
			if helpContainsOption(stdout.String(), unwanted) {
				t.Errorf("%s help contains unrelated option %q: %s", test.command, unwanted, stdout.String())
			}
		}
		if stderr.Len() != 0 {
			t.Errorf("%s help wrote stderr: %q", test.command, stderr.String())
		}
	}
}

func TestRunHelpDocumentsDefaults(t *testing.T) {
	tests := []struct {
		command string
		want    []string
	}{
		{
			command: "stats",
			want: []string{
				"--days N          Include the last N days (N >= 1; default: all time)",
				"--color MODE      auto, always, or never (default: auto; human report only)",
			},
		},
		{
			command: "tools",
			want: []string{
				"--days N          Include the last N days (N >= 1; default: all time)",
				"--color MODE      auto, always, or never (default: auto; human report only)",
				"--layer LAYER     effective, runtime, or model (default: effective)",
			},
		},
		{
			command: "skills",
			want: []string{
				"--days N          Include the last N days (N >= 1; default: all time)",
				"--color MODE      auto, always, or never (default: auto; human report only)",
				"--group-by UNIT   turn or session (default: turn; no effect on --unused)",
				"--view VIEW       auto, compact, mode, state, or all (default: auto; human report only)",
				"--root PATH       Scan a scope root for .agents/skills, .codex/skills, plugin cache layouts (repeatable; only with --unused; default if omitted: ~/.agents/skills)",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run([]string{tt.command, "--help"}, &stdout, &stderr); code != 0 {
				t.Fatalf("help exit=%d stderr=%s", code, stderr.String())
			}
			for _, want := range tt.want {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("help missing %q:\n%s", want, stdout.String())
				}
			}
		})
	}
}

func TestRenderHelpAddsTerminalStyles(t *testing.T) {
	colored := renderHelp(usageText, output.TerminalCapabilities{ColorMode: output.ColorAlways})
	if !strings.Contains(colored, "\x1b[") {
		t.Fatalf("colored help contains no ANSI styles: %q", colored)
	}
	if helpCommandStyle.Render("name") != helpOptionStyle.Render("name") {
		t.Fatalf("command and option styles differ")
	}
	for _, want := range []string{
		helpCommandStyle.Render("catsift"),
		helpHeadingStyle.Render("Commands:"),
		helpCommandStyle.Render("stats"),
		helpOptionStyle.Render("--source"),
		helpMutedStyle.Render("SOURCE"),
		helpMutedStyle.Render("[options]"),
		helpMutedStyle.Render("<command>"),
		helpMutedStyle.Render("(default: detected agent histories; ctx is opt-in)"),
	} {
		if !strings.Contains(colored, want) {
			t.Errorf("colored help missing styled text %q: %q", want, colored)
		}
	}
	if strings.Contains(colored, "\x1b[1m") || strings.Contains(colored, "\x1b[1;") {
		t.Fatalf("colored help contains bold styling: %q", colored)
	}
	commandHelp := renderHelp(statsUsageText, output.TerminalCapabilities{ColorMode: output.ColorAlways})
	for _, want := range []string{
		helpCommandStyle.Render("stats"),
		helpOptionStyle.Render("--days"),
		helpMutedStyle.Render("(N >= 1; default: all time)"),
	} {
		if !strings.Contains(commandHelp, want) {
			t.Errorf("command help missing styled text %q: %q", want, commandHelp)
		}
	}

	plain := renderHelp(usageText, output.TerminalCapabilities{ColorMode: output.ColorNever})
	if plain != usageText {
		t.Fatalf("plain help changed: got %q, want %q", plain, usageText)
	}
}

func TestRenderHelpRespectsNoColor(t *testing.T) {
	got := renderHelp(usageText, output.TerminalCapabilities{ColorMode: output.ColorAuto, IsTTY: true, NoColor: true})
	if got != usageText {
		t.Fatalf("NO_COLOR help changed: got %q, want %q", got, usageText)
	}
}

func helpContainsOption(text, option string) bool {
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == option {
			return true
		}
	}
	return false
}

func TestRunSkillsSupportsSessionGrouping(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", home)
	t.Setenv("OPENCODE_HOME", t.TempDir())
	writeSession := func(id string, turns int) {
		history := filepath.Join(home, "sessions", id+".jsonl")
		lines := []string{`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"` + id + `"}}`}
		for i := 0; i < turns; i++ {
			turnID := id + "-t" + strconv.Itoa(i+1)
			lines = append(lines,
				`{"timestamp":"2026-01-01T00:00:0`+strconv.Itoa(i+1)+`Z","type":"task_started","payload":{"turn_id":"`+turnID+`"}}`,
				`{"timestamp":"2026-01-01T00:00:0`+strconv.Itoa(i+1)+`Z","type":"user_message","payload":{"text":"$report"}}`,
				`{"timestamp":"2026-01-01T00:00:0`+strconv.Itoa(i+1)+`Z","type":"task_complete","payload":{"turn_id":"`+turnID+`"}}`,
			)
		}
		if err := os.MkdirAll(filepath.Dir(history), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(history, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeSession("s1", 2)
	writeSession("s2", 1)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"skills", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("turn grouping exit=%d stderr=%s", code, stderr.String())
	}
	var turnValue struct {
		GroupBy string `json:"group_by"`
		Rows    []struct {
			Total int `json:"total"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &turnValue); err != nil {
		t.Fatal(err)
	}
	if turnValue.GroupBy != "turn" || len(turnValue.Rows) != 1 || turnValue.Rows[0].Total != 3 {
		t.Fatalf("turn grouping = %#v", turnValue)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"skills", "--json", "--group-by", "session"}, &stdout, &stderr); code != 0 {
		t.Fatalf("session grouping exit=%d stderr=%s", code, stderr.String())
	}
	var sessionValue struct {
		GroupBy string `json:"group_by"`
		Rows    []struct {
			Total int `json:"total"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &sessionValue); err != nil {
		t.Fatal(err)
	}
	if sessionValue.GroupBy != "session" || len(sessionValue.Rows) != 1 || sessionValue.Rows[0].Total != 2 {
		t.Fatalf("session grouping = %#v", sessionValue)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"stats", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("stats overview grouping exit=%d stderr=%s", code, stderr.String())
	}
	var statsValue struct {
		Turns            int `json:"turns"`
		SkillUsesTurn    int `json:"skill_uses_turn"`
		SkillUsesSession int `json:"skill_uses_session"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &statsValue); err != nil {
		t.Fatal(err)
	}
	if statsValue.Turns != 3 || statsValue.SkillUsesTurn != 3 || statsValue.SkillUsesSession != 2 {
		t.Fatalf("stats grouping = %#v", statsValue)
	}
}

func TestRunUnusedSkillsEndToEnd(t *testing.T) {
	testHome(t)
	root := t.TempDir()
	writeTestSkill(t, filepath.Join(root, "repo", ".agents", "skills", "report"), "report")
	writeTestSkill(t, filepath.Join(root, "repo", ".codex", "skills", "review"), "canonical-review")
	writeTestSkill(t, filepath.Join(root, "repo", ".codex", "plugins", "cache", "example", "data-analytics", "1.0.0", "skills", "router"), "")
	writeTestSkill(t, filepath.Join(root, "repo", "skills", "ignored"), "ignored")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"skills", "--root", root, "--unused", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("unused JSON exit=%d stderr=%s", code, stderr.String())
	}
	var value struct {
		View           string   `json:"view"`
		Roots          []string `json:"roots"`
		InstalledCount int      `json:"installed_count"`
		UnusedCount    int      `json:"unused_count"`
		Rows           []struct {
			Name         string `json:"name"`
			NameSource   string `json:"name_source"`
			NameMismatch bool   `json:"name_mismatch"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
		t.Fatalf("invalid unused JSON: %v (%s)", err, stdout.String())
	}
	if value.View != "unused" || len(value.Roots) != 1 || value.Roots[0] != root {
		t.Fatalf("unused scope = %#v", value)
	}
	if value.InstalledCount != 3 || value.UnusedCount != 2 || len(value.Rows) != 2 {
		t.Fatalf("unused counts = %#v", value)
	}
	if value.Rows[0].Name != "canonical-review" || value.Rows[0].NameSource != "frontmatter" || !value.Rows[0].NameMismatch {
		t.Fatalf("frontmatter row = %#v", value.Rows[0])
	}
	if value.Rows[1].Name != "data-analytics:router" || value.Rows[1].NameSource != "directory" {
		t.Fatalf("plugin row = %#v", value.Rows[1])
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected warning: %s", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"skills", "--root", root, "--unused", "--color", "never"}, &stdout, &stderr); code != 0 {
		t.Fatalf("unused human exit=%d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{"UNUSED SKILLS", "canonical-review", "data-analytics:router", "Strict: false", "2 unused skills, 3 installed skills total"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("unused human missing %q: %s", want, stdout.String())
		}
	}
}

func TestRunUnusedSkillsUsesDefaultRoot(t *testing.T) {
	testHome(t)
	userHome := os.Getenv("HOME")
	writeTestSkill(t, filepath.Join(userHome, ".agents", "skills", "default-skill"), "default-skill")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"skills", "--unused", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("default root exit=%d stderr=%s", code, stderr.String())
	}
	var value struct {
		Roots          []string `json:"roots"`
		InstalledCount int      `json:"installed_count"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	wantRoot := filepath.Join(userHome, ".agents", "skills")
	if len(value.Roots) != 1 || value.Roots[0] != wantRoot || value.InstalledCount != 1 {
		t.Fatalf("default root = %#v, want %q with one entry", value, wantRoot)
	}
}

func TestRunUnusedSkillsSupportsRepeatableRoots(t *testing.T) {
	testHome(t)
	first := t.TempDir()
	second := t.TempDir()
	writeTestSkill(t, filepath.Join(first, ".agents", "skills", "first"), "first")
	writeTestSkill(t, filepath.Join(second, ".codex", "skills", "second"), "second")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"skills", "--unused", "--root", second, "--root", first, "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("multiple roots exit=%d stderr=%s", code, stderr.String())
	}
	var value struct {
		Roots          []string `json:"roots"`
		InstalledCount int      `json:"installed_count"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if len(value.Roots) != 2 || value.Roots[0] != first || value.Roots[1] != second || value.InstalledCount != 2 {
		t.Fatalf("multiple roots = %#v", value)
	}
}

func TestRunUnusedSkillsKeepsWarningsSeparate(t *testing.T) {
	home := testHome(t)
	history := filepath.Join(home, "sessions", "2026", "one.jsonl")
	file, err := os.OpenFile(history, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("not-json\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeTestSkill(t, filepath.Join(root, ".agents", "skills", "report"), "report")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"skills", "--root", root, "--unused", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("warning JSON exit=%d stderr=%s", code, stderr.String())
	}
	var value map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
		t.Fatalf("stdout is not JSON: %v (%s)", err, stdout.String())
	}
	if strings.Contains(stdout.String(), "malformed_json") || strings.Contains(stdout.String(), "warning:") || strings.Contains(stdout.String(), "\x1b[") {
		t.Fatalf("warning or ANSI leaked into JSON: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "warning: skipped 1 record") {
		t.Fatalf("warning summary missing from stderr: %s", stderr.String())
	}
	if strings.Contains(stderr.String(), "malformed_json") || strings.Contains(stderr.String(), "/one.jsonl") {
		t.Fatalf("warning details leaked into summary: %s", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"skills", "--root", root, "--unused", "--json", "--strict-input"}, &stdout, &stderr); code != 1 {
		t.Fatalf("strict-input exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
		t.Fatalf("strict stdout is not JSON: %v (%s)", err, stdout.String())
	}
	if !strings.Contains(stderr.String(), "strict-input") {
		t.Fatalf("strict-input diagnostic missing: %s", stderr.String())
	}
}

func TestRunUnusedSkillsAppliesStrictAndDays(t *testing.T) {
	root := t.TempDir()
	writeTestSkill(t, filepath.Join(root, ".agents", "skills", "report"), "report")

	usageHomeAt(t, time.Now().UTC().Add(-24*time.Hour))
	var stdout, stderr bytes.Buffer
	if code := run([]string{"skills", "--root", root, "--unused", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("recent default exit=%d stderr=%s", code, stderr.String())
	}
	var value struct {
		Strict      bool `json:"strict"`
		UnusedCount int  `json:"unused_count"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if value.Strict || value.UnusedCount != 0 {
		t.Fatalf("recent default = %#v", value)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"skills", "--root", root, "--unused", "--strict", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("strict exit=%d stderr=%s", code, stderr.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if !value.Strict || value.UnusedCount != 1 {
		t.Fatalf("strict = %#v", value)
	}

	usageHomeAt(t, time.Now().UTC().Add(-40*24*time.Hour))
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"skills", "--root", root, "--unused", "--days", "30", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("days exit=%d stderr=%s", code, stderr.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if value.UnusedCount != 1 {
		t.Fatalf("days = %#v", value)
	}
}

func TestRunRejectsInvalidArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"unknown"}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("unknown command exit=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"stats", "--csv"}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "flag provided but not defined") {
		t.Fatalf("removed csv option exit=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"stats", "--days", "0"}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "days") {
		t.Fatalf("days validation exit=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"stats", "extra"}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "unexpected argument") {
		t.Fatalf("extra argument exit=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"skills", "--group-by", "event"}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "invalid --group-by") {
		t.Fatalf("invalid group-by exit=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"skills", "--view", "invalid"}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "invalid --view") {
		t.Fatalf("invalid view exit=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"tools", "--view", "mode"}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "--view is only valid for skills") {
		t.Fatalf("tools view exit=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"tools", "--group-by", "session"}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "only valid for skills") {
		t.Fatalf("tools group-by exit=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"stats", "--group-by", "session"}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "only valid for skills") {
		t.Fatalf("stats group-by exit=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"skills", "--root", t.TempDir()}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "--root") {
		t.Fatalf("root without unused exit=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"stats", "--unused"}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "--unused") {
		t.Fatalf("stats unused exit=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"tools", "--unused"}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "--unused") {
		t.Fatalf("tools unused exit=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	missingRoot := filepath.Join(t.TempDir(), "missing")
	testHome(t)
	if code := run([]string{"skills", "--unused", "--root", missingRoot, "--json"}, &stdout, &stderr); code == 0 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "scan skill roots") {
		t.Fatalf("missing root exit=%d stdout=%q stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestRunSkillsViewOptionAndAutoContext(t *testing.T) {
	testHome(t)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"skills", "--color", "never"}, &stdout, &stderr); code != 0 {
		t.Fatalf("auto view exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "View: auto (selected: mode)") {
		t.Fatalf("auto view was not reported:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"skills", "--view", "state", "--color", "never"}, &stdout, &stderr); code != 0 {
		t.Fatalf("state view exit=%d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{"View: state", "Confirmed", "Inferred", "Unconfirmed"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("state view missing %q:\n%s", want, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), "Last Used") {
		t.Fatalf("state view contains Last Used:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"skills", "--view", "all", "--color", "never"}, &stdout, &stderr); code != 0 {
		t.Fatalf("all view exit=%d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{"View: all", "ACTIVATION MODE", "EVIDENCE STATE", "Explicit", "Confirmed", "Last Used"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("all view missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestRunKeepsWarningsOffMachineReadableStdout(t *testing.T) {
	home := testHome(t)
	path := filepath.Join(home, "sessions", "2026", "one.jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("not-json\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"stats", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var value map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
		t.Fatalf("stdout is not JSON: %v (%s)", err, stdout.String())
	}
	if !strings.Contains(stderr.String(), "warning: skipped 1 record") {
		t.Fatalf("warning summary missing from stderr: %s", stderr.String())
	}
	if strings.Contains(stderr.String(), "malformed_json") || strings.Contains(stderr.String(), "/one.jsonl") {
		t.Fatalf("warning details leaked into summary: %s", stderr.String())
	}
}

func TestWriteWarningsAggregatesByReasonAndType(t *testing.T) {
	warnings := []usage.Warning{
		{Reason: "unknown_type", Type: "future_a", Path: "/one.jsonl", Line: 1, Count: 2},
		{Reason: "unknown_type", Type: "future_b", Path: "/one.jsonl", Line: 3, Count: 1},
		{Reason: "malformed_json", Path: "/two.jsonl", Line: 4, Count: 1},
	}
	var output bytes.Buffer
	writeWarnings(&output, warnings, false)
	got := output.String()
	if strings.Count(got, "warning:") != 1 {
		t.Fatalf("summary should contain one warning: %q", got)
	}
	for _, want := range []string{"warning: skipped 4 records", "across 2 files", "--verbose"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "/one.jsonl") || strings.Contains(got, "/two.jsonl") || strings.Contains(got, "unknown_type") || strings.Contains(got, "malformed_json") {
		t.Fatalf("summary leaked file paths: %q", got)
	}
}

func TestWriteWarningsVerboseIncludesDetails(t *testing.T) {
	warnings := []usage.Warning{
		{Reason: "unknown_type", Type: "future_a", Path: "/one.jsonl", Line: 1, Count: 2},
		{Reason: "malformed_json", Path: "/two.jsonl", Line: 4, Count: 1},
	}
	var output bytes.Buffer
	writeWarnings(&output, warnings, true)
	got := output.String()
	for _, want := range []string{"warning: skipped unknown record type type=future_a at /one.jsonl:1", "warning: skipped malformed JSON record at /two.jsonl:4"} {
		if !strings.Contains(got, want) {
			t.Errorf("verbose warning missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "unknown_type") || strings.Contains(got, "malformed_json") {
		t.Fatalf("verbose output used internal warning names: %q", got)
	}
}

func TestWriteWarningsVerboseIncludesSourceAndAction(t *testing.T) {
	warnings := []usage.Warning{{Reason: "unknown_type", Type: "system.message", Source: usage.SourceCodex, Count: 4}}
	var output bytes.Buffer
	writeWarnings(&output, warnings, true)
	got := output.String()
	for _, want := range []string{
		"warning: [Codex] skipped unknown record type type=system.message (4)",
		"statistics may be incomplete",
		"update catsift or report this record type",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("verbose warning missing %q: %q", want, got)
		}
	}
}

func TestWriteWarningSummaryIncludesSourceAndAction(t *testing.T) {
	warnings := []usage.Warning{{Reason: "unknown_type", Type: "system.message", Source: usage.SourceCodex, Path: "/one.jsonl", Count: 4}}
	var output bytes.Buffer
	writeWarnings(&output, warnings, false)
	got := output.String()
	for _, want := range []string{"from Codex", "statistics may be incomplete", "update catsift or report this record type"} {
		if !strings.Contains(got, want) {
			t.Errorf("warning summary missing %q: %q", want, got)
		}
	}
}

func TestWriteWarningSummaryDoesNotTreatIgnoredDatabasesAsRecords(t *testing.T) {
	warnings := []usage.Warning{{
		Reason: "opencode_multiple_databases",
		Type:   "database",
		Source: usage.SourceOpenCode,
		Path:   "/opencode",
		Count:  2,
	}}
	var output bytes.Buffer
	writeWarnings(&output, warnings, false)
	got := output.String()
	for _, want := range []string{
		"ignored 2 OpenCode databases",
		"from OpenCode",
		"only the selected database was read",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("database warning summary missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "skipped 2 records") {
		t.Fatalf("database warning was rendered as skipped records: %q", got)
	}
}

func TestWriteWarningSummaryExcludesDatabaseRootFromFileCount(t *testing.T) {
	warnings := []usage.Warning{
		{Reason: "opencode_malformed_message", Source: usage.SourceOpenCode, Path: "/opencode/opencode-a.db", Count: 1},
		{Reason: "opencode_multiple_databases", Source: usage.SourceOpenCode, Path: "/opencode", Count: 1},
	}
	var output bytes.Buffer
	writeWarnings(&output, warnings, false)
	want := "skipped 1 record across 1 file; ignored 1 OpenCode database from OpenCode"
	if !strings.Contains(output.String(), want) {
		t.Fatalf("warning summary = %q, want %q", output.String(), want)
	}
}

func TestWriteWarningsTreatsOversizedRecordsAsInformational(t *testing.T) {
	warnings := []usage.Warning{{Reason: "large_line", Path: "/one.jsonl", Line: 220, Count: 1}}
	var output bytes.Buffer
	writeWarnings(&output, warnings, false)
	got := output.String()
	for _, want := range []string{"info: skipped 1 record across 1 file", "--verbose"} {
		if !strings.Contains(got, want) {
			t.Errorf("informational summary missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "warning:") || strings.Contains(got, "large_line") || strings.Contains(got, "/one.jsonl") {
		t.Fatalf("informational summary was too prominent or detailed: %q", got)
	}

	output.Reset()
	writeWarnings(&output, warnings, true)
	got = output.String()
	if !strings.Contains(got, "info: skipped oversized history record at /one.jsonl:220 (1)") {
		t.Fatalf("informational detail missing: %q", got)
	}
	for _, want := range []string{"statistics may be incomplete", "inspect the source history"} {
		if !strings.Contains(got, want) {
			t.Errorf("oversized record advice missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "warning:") || strings.Contains(got, "large_line") {
		t.Fatalf("oversized record was rendered as a warning: %q", got)
	}
}

func TestWriteWarningsSeparatesUnreadableFilesFromSkippedRecords(t *testing.T) {
	warnings := []usage.Warning{
		{Reason: "read_file", Path: "/one.jsonl", Count: 1},
		{Reason: "malformed_json", Path: "/two.jsonl", Count: 2},
	}
	var output bytes.Buffer
	writeWarnings(&output, warnings, false)
	got := output.String()
	for _, want := range []string{"warning: skipped 2 records", "could not read 1 file", "across 2 files"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q: %q", want, got)
		}
	}
}

func TestRunStrictInputReturnsNonZeroAfterRenderingReport(t *testing.T) {
	home := testHome(t)
	path := filepath.Join(home, "sessions", "2026", "one.jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("not-json\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"stats", "--strict-input"}, &stdout, &stderr); code == 0 {
		t.Fatalf("strict-input unexpectedly succeeded: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if stdout.Len() == 0 || !strings.Contains(stderr.String(), "strict-input") {
		t.Fatalf("strict-input diagnostics missing: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
}

func TestRunStylesDiagnosticPrefixesOnly(t *testing.T) {
	home := testHome(t)
	path := filepath.Join(home, "sessions", "2026", "one.jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("not-json\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"stats", "--color", "always"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	got := stderr.String()
	warningPrefix := "\x1b[1;93mwarning:\x1b[m "
	if !strings.HasPrefix(got, "\n"+warningPrefix) {
		t.Fatalf("warning prefix is not yellow and bold: %q", got)
	}
	if strings.Contains(strings.TrimPrefix(got, "\n"+warningPrefix), "\x1b[") {
		t.Fatalf("warning body is styled with its prefix: %q", got)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"stats", "--color", "always", "--verbose"}, &stdout, &stderr); code != 0 {
		t.Fatalf("verbose exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	got = stderr.String()
	if !strings.Contains(got, "\n"+warningPrefix) {
		t.Fatalf("verbose warning is missing or not separated from the report: %q", got)
	}
	if strings.Contains(got, "\n\n") {
		t.Fatalf("verbose diagnostics contain an extra blank line: %q", got)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"stats", "--days", "0", "--color", "always"}, &stdout, &stderr); code == 0 {
		t.Fatalf("invalid days unexpectedly succeeded: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if got := stderr.String(); !strings.Contains(got, "\x1b[1;91merror:\x1b[m --days must be at least 1") {
		t.Fatalf("error prefix is not red and bold: %q", got)
	}
}

func TestRunKeepsDiagnosticsPlainForJSON(t *testing.T) {
	home := testHome(t)
	path := filepath.Join(home, "sessions", "2026", "one.jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("not-json\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"stats", "--json", "--color", "always"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "\x1b[") || strings.Contains(stderr.String(), "\x1b[") {
		t.Fatalf("JSON diagnostics contain ANSI: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}
