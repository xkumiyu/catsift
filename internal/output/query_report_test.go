package output

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/xkumiyu/catsift/internal/aggregate"
	"github.com/xkumiyu/catsift/internal/query"
	"github.com/xkumiyu/catsift/internal/usage"
)

func queryReportFixture() query.ReadModel {
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	source := usage.NewCodexSourceRef("/private/history.jsonl", 1, "1")
	model := usage.NewModelRef("codex", "a-very-long-model-name")
	turn := usage.NewTurn("session-001", "turn-001", 1, source)
	turn.StartedAt = when
	turn.EndedAt = when.Add(time.Minute)
	turn.UserPrompts = 1
	turn.UserPromptTimes = []time.Time{when}
	turn.AddTokenUsageForModelAt(model, when, usage.TokenUsage{
		InputTokens:           5,
		CachedInputTokens:     2,
		CacheWriteInputTokens: 1,
		OutputTokens:          3,
		ReasoningOutputTokens: 1,
		TotalTokens:           8,
	})
	turn.RuntimeTools = []usage.ToolObservation{{RawName: "exec", CanonicalName: "shell", Arguments: "private-command", Timestamp: when, Layer: usage.LayerRuntime, Source: source}}
	turn.SkillEvidence = []usage.SkillEvidence{usage.NewSkillEvidence("session-001", "turn-001", "review", usage.ModeExplicit, usage.MethodStructuredTool, usage.StateConfirmed, when, source)}
	session := usage.NewSession("session-001", source)
	session.Title = "A very long session title"
	session.ProjectName = "owner/project"
	session.ProjectPath = "/private/project"
	return query.Build(query.SanitizeInput(query.Input{Turns: []usage.Turn{turn}, Sessions: []usage.Session{session}, Source: usage.SourceCodex, Agents: []string{"codex"}}), query.Filter{Source: usage.SourceCodex})
}

func TestRenderQueryHumanFitsNarrowTerminal(t *testing.T) {
	model := queryReportFixture()
	got := RenderQueryHuman("models", ReportContext{Source: usage.SourceCodex, Agent: "codex", Period: "all time", Location: time.UTC}, model, "", TerminalCapabilities{Width: 32, ColorMode: ColorNever})
	for _, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
		if width := lipgloss.Width(line); width > 32 {
			t.Fatalf("line width=%d > 32: %q\n%s", width, line, got)
		}
	}
}

func TestRenderQueryHumanSessionDetailShowsTokenBreakdown(t *testing.T) {
	model := queryReportFixture()
	got := RenderQueryHuman("sessions", ReportContext{Source: usage.SourceCodex, Agent: "codex", Period: "all time", Location: time.UTC}, model, model.Sessions[0].Key, TerminalCapabilities{Width: 120, ColorMode: ColorNever})

	for _, metric := range []struct {
		label string
		value string
	}{
		{label: "Total Tokens", value: "8"},
		{label: "Input Tokens", value: "5"},
		{label: "Cached Tokens", value: "2"},
		{label: "Cache Write Input Tokens", value: "1"},
		{label: "Output Tokens", value: "3"},
		{label: "Reasoning Tokens", value: "1"},
	} {
		found := false
		for _, line := range strings.Split(got, "\n") {
			if strings.Contains(line, metric.label) && strings.HasSuffix(strings.TrimSpace(line), metric.value) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("session detail missing %s=%s:\n%s", metric.label, metric.value, got)
		}
	}
}

func TestRenderQueryTableAlignsColumns(t *testing.T) {
	lines := renderQueryTable(
		[]string{"Model", "Sessions", "Turns", "Tokens", "Last Used"},
		[][]string{
			{"openai/gpt-5.6-luna", "247", "1,674", "4.05B", "2026-09-15 10:42 JST"},
			{"openai/gpt-5.6-sol", "39", "421", "428M", "2026-09-02 01:08 JST"},
		},
		[]bool{false, true, true, true, false},
		100,
		false,
		"empty",
	)

	if len(lines) != 4 {
		t.Fatalf("table lines = %#v", lines)
	}
	for _, value := range []string{"Sessions", "Turns", "Tokens"} {
		headerEnd := strings.Index(lines[0], value) + len(value)
		if headerEnd != strings.Index(lines[2], valueForColumn(value))+len(valueForColumn(value)) {
			t.Fatalf("%s column is not aligned: header=%q row=%q", value, lines[0], lines[2])
		}
	}
	if strings.Index(lines[0], "Last Used") != strings.Index(lines[2], "2026-09-15") {
		t.Fatalf("last used column is not aligned: header=%q row=%q", lines[0], lines[2])
	}
}

func valueForColumn(header string) string {
	values := map[string]string{
		"Sessions": "247",
		"Turns":    "1,674",
		"Tokens":   "4.05B",
	}
	return values[header]
}

func TestRenderQueryJSONUsesPublicSafeSchema(t *testing.T) {
	model := queryReportFixture()
	data, err := RenderQueryJSON("sessions", ReportContext{Source: usage.SourceCodex, Agent: "codex", Period: "all time", ReferenceTime: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)}, model, model.Sessions[0].Key)
	if err != nil {
		t.Fatal(err)
	}
	var value struct {
		Summary struct {
			ID          string `json:"id"`
			ProjectName string `json:"project_name"`
			ProjectPath string `json:"project_path"`
		} `json:"summary"`
		Turns []struct {
			Tools []string `json:"tools"`
		} `json:"turns"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	if value.Summary.ID != "session-001" || value.Summary.ProjectName != "owner/project" || value.Summary.ProjectPath != "/private/project" || len(value.Turns) != 1 || value.Turns[0].Tools[0] != "shell" {
		t.Fatalf("public session JSON = %#v", value)
	}
	for _, forbidden := range []string{"private-command", "history.jsonl", "session\u0000"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("public JSON contains %q: %s", forbidden, data)
		}
	}
}

func TestRenderActivityShowsDailyRows(t *testing.T) {
	trend := []query.UsageTrend{{Date: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Sessions: 1, Turns: 2}}
	got := RenderHuman("activity", ReportContext{Agent: "codex", Period: "all time", Trend: trend}, aggregate.Report{}, TerminalCapabilities{Width: 80, ColorMode: ColorNever})
	if !strings.Contains(got, "ACTIVITY") || !strings.Contains(got, "Daily Trend") || !strings.Contains(got, "2026-01-02") || strings.Contains(got, "USAGE OVERVIEW") {
		t.Fatalf("activity output mismatch: %q", got)
	}
}
