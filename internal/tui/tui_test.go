package tui

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/xkumiyu/catsift/internal/query"
	"github.com/xkumiyu/catsift/internal/usage"
)

func explorerInput() query.Input {
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	source := usage.NewCodexSourceRef("/private/history.jsonl", 1, "1")
	session := usage.NewSession("s", source)
	turn := usage.NewTurn("s", "turn", 1, source)
	turn.StartedAt = when
	turn.EndedAt = when.Add(time.Minute)
	turn.UserPrompts = 1
	turn.UserPromptTimes = []time.Time{when}
	turn.ObserveModelAt(usage.NewModelRef("codex", "gpt-example"), when, source)
	turn.AddTokenUsageForModelAt(usage.NewModelRef("codex", "gpt-example"), when, usage.TokenUsage{TotalTokens: 7})
	turn.SkillEvidence = []usage.SkillEvidence{{SkillName: "review", Mode: usage.ModeExplicit, Method: usage.MethodExplicitRequest, State: usage.StateConfirmed, Timestamp: when, Source: source}}
	return query.Input{Turns: []usage.Turn{turn}, Sessions: []usage.Session{session}, Source: usage.SourceCodex, Warnings: []usage.Warning{{Reason: "malformed_json", Count: 1}}}
}

func longExplorerInput() query.Input {
	input := explorerInput()
	source := input.Turns[0].Source
	for i := 0; i < 24; i++ {
		turn := usage.NewTurn("s", "turn-extra-"+string(rune('a'+i)), i+2, source)
		turn.StartedAt = time.Date(2026, 1, 3, 0, i, 0, 0, time.UTC)
		turn.EndedAt = turn.StartedAt.Add(time.Minute)
		turn.ObserveModelAt(usage.NewModelRef("codex", "gpt-example"), turn.StartedAt, source)
		input.Turns = append(input.Turns, turn)
	}
	return input
}

func TestOverviewUsesStatsContextAndTokenBreakdown(t *testing.T) {
	input := explorerInput()
	input.Agents = []string{"codex"}
	tokens := usage.TokenUsage{
		InputTokens:           10,
		CachedInputTokens:     4,
		CacheWriteInputTokens: 2,
		OutputTokens:          3,
		ReasoningOutputTokens: 1,
		TotalTokens:           13,
	}
	input.Turns[0].TokenUsage = &tokens
	input.Turns[0].TokenUsageEvents[0].Usage = tokens

	state := NewState(input, query.Filter{}, nil)
	state.Width = 120
	state.Height = 40
	view := strings.Join(append(state.headerLines(), state.overviewLines()...), "\n")

	for _, want := range []string{
		"catsift",
		"Source:",
		"Agents:",
		"Codex",
		"Period:",
		"2026-01-02 to 2026-01-02",
		"User Prompts",
		"Tool Calls",
		"Total Tokens",
		"Input Tokens",
		"Cached Tokens",
		"Cache Write Input Tokens",
		"Output Tokens",
		"Reasoning Tokens",
		"Skill Uses",
		"Recent Sessions",
		"Top Skills",
		"Top Models",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("overview missing %q: %s", want, view)
		}
	}
	for _, unwanted := range []string{"Usage Explorer", "Agent: unknown", "Project: —", "1 turns / 1 sessions"} {
		if strings.Contains(view, unwanted) {
			t.Errorf("overview contains obsolete text %q: %s", unwanted, view)
		}
	}
}

func TestOverviewUsesNamedSummarySections(t *testing.T) {
	state := NewState(explorerInput(), query.Filter{}, nil)
	state.Width = 120
	state.Height = 40

	view := strings.Join(state.overviewLines(), "\n")
	for _, want := range []string{"Usage summary", "Skill Uses", "Recent Activity", "Recent Sessions", "Top Skills", "Top Models"} {
		if !strings.Contains(view, want) {
			t.Fatalf("overview missing %q: %s", want, view)
		}
	}
	for _, unwanted := range []string{"Highlights", "Recent sessions:", "Top skills:", "Top models:", "Daily activity", "Skill Usage", "By turn", "By session"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("overview contains activity detail %q: %s", unwanted, view)
		}
	}
}

func TestOverviewSectionsFollowSemanticGroups(t *testing.T) {
	state := NewState(explorerInput(), query.Filter{}, nil)
	state.Width = 120

	lines := state.overviewLines()
	lastIndex := -1
	for _, section := range []string{
		"Usage summary",
		"Skill Uses",
		"Token Usage",
		"Top Models",
		"Recent Activity",
		"Recent Sessions",
		"Top Skills",
	} {
		index := -1
		for lineIndex, line := range lines {
			if strings.Contains(line, section) {
				index = lineIndex
				break
			}
		}
		if index <= lastIndex {
			t.Fatalf("overview section order = %#v, want %q after line %d", lines, section, lastIndex)
		}
		lastIndex = index
	}
}

func TestJKNavigationWrapsFocusedRowsOnly(t *testing.T) {
	state := NewState(explorerInput(), query.Filter{}, nil)
	state.ReadModel.Models = []query.ModelSummary{
		{Model: usage.NewModelRef("codex", "first")},
		{Model: usage.NewModelRef("codex", "last")},
	}
	state.Route = RouteModels
	state.Selected, state.Offset = 0, 0
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if state.Selected != 1 {
		t.Fatalf("k at the first model should wrap to the last: selected=%d", state.Selected)
	}
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if state.Selected != 0 {
		t.Fatalf("j at the last model should wrap to the first: selected=%d", state.Selected)
	}

	state.Route = RouteOverview
	state.Height = 10
	maxOffset := state.rowCount() - state.visibleHeight()
	if maxOffset <= 0 {
		t.Fatalf("overview should be scrollable for the wrap test: rows=%d visible=%d", state.rowCount(), state.visibleHeight())
	}
	state.Selected, state.Offset = maxOffset, maxOffset
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if state.Offset != maxOffset {
		t.Fatalf("j at the bottom of Overview should stop: offset=%d want=%d", state.Offset, maxOffset)
	}
	state.Selected, state.Offset = 0, 0
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if state.Offset != 0 {
		t.Fatalf("k at the top of Overview should stop: offset=%d", state.Offset)
	}

	state = NewState(longExplorerInput(), query.Filter{}, nil)
	state.Route = RouteActivity
	state.Height = 10
	maxOffset = state.rowCount() - state.visibleHeight()
	if maxOffset <= 0 {
		t.Fatalf("activity should be scrollable for the boundary test: rows=%d visible=%d", state.rowCount(), state.visibleHeight())
	}
	state.Selected, state.Offset = maxOffset, maxOffset
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if state.Offset != maxOffset {
		t.Fatalf("j at the bottom of Activity should stop: offset=%d want=%d", state.Offset, maxOffset)
	}
	state.Selected, state.Offset = 0, 0
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if state.Offset != 0 {
		t.Fatalf("k at the top of Activity should stop: offset=%d", state.Offset)
	}
}

func TestOverviewSummaryUsesSeparatedStyledRows(t *testing.T) {
	model := query.ReadModel{
		Sessions: []query.SessionSummary{
			{Title: "first session", ID: "session-1", EndedAt: time.Now().UTC().Add(-2 * time.Hour)},
			{Title: "second session", ID: "session-2", EndedAt: time.Now().UTC().Add(-3 * time.Hour)},
			{Title: "third session", ID: "session-3", EndedAt: time.Now().UTC().Add(-4 * time.Hour)},
		},
		Skills: []query.SkillSummary{
			{Name: "review", Uses: 3},
			{Name: "deploy", Uses: 2},
			{Name: "docs", Uses: 1},
		},
		Models: []query.ModelSummary{
			{Model: usage.NewModelRef("openai", "token-heavy"), TokenUsage: usage.TokenUsage{TotalTokens: 1_200}, TokenUsageAvailable: true},
			{Model: usage.NewModelRef("openai", "turn-heavy"), TokenUsage: usage.TokenUsage{TotalTokens: 100}, TokenUsageAvailable: true},
			{Model: usage.NewModelRef("openai", "small"), TokenUsage: usage.TokenUsage{TotalTokens: 50}, TokenUsageAvailable: true},
		},
	}

	lines := overviewTopModelsLines(model, 100)
	lines = append(lines, "")
	lines = append(lines, overviewRecentSessionLines(model, 100)...)
	lines = append(lines, "")
	lines = append(lines, overviewTopSkillsLines(model, 100)...)
	if len(lines) != 14 || lines[4] != "" || lines[9] != "" {
		t.Fatalf("overview summary layout = %#v", lines)
	}
	for _, want := range []string{
		sectionStyle.Render("Recent Sessions"),
		sectionStyle.Render("Top Skills"),
		sectionStyle.Render("Top Models"),
		identityStyle.Render("first session"),
		"2h ago",
		identityStyle.Render("review"),
		identityStyle.Render("openai/token-heavy"),
		"1.2k tokens",
	} {
		if !strings.Contains(strings.Join(lines, "\n"), want) {
			t.Fatalf("overview summary missing styled value %q: %#v", want, lines)
		}
	}
	if strings.Contains(strings.Join(lines, "\n"), ", ") {
		t.Fatalf("overview summary should use one item per row: %#v", lines)
	}
	if strings.Contains(strings.Join(lines, "\n"), mutedStyle.Render("2h ago")) {
		t.Fatalf("overview summary values should use the normal color: %#v", lines)
	}
	if !strings.Contains(lines[1], "token-heavy") {
		t.Fatalf("top models should be ordered by token usage: %#v", lines)
	}
	rowWidth := -1
	for _, line := range lines {
		if !strings.HasPrefix(line, "  ") {
			continue
		}
		if rowWidth == -1 {
			rowWidth = lipgloss.Width(line)
		} else if lipgloss.Width(line) != rowWidth {
			t.Fatalf("overview summary rows should align values: %#v", lines)
		}
	}
}

func TestOverviewActivityPreviewShowsRecentSevenDays(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	trend := make([]query.UsageTrend, 8)
	for index := range trend {
		trend[index] = query.UsageTrend{
			Date:     start.AddDate(0, 0, index),
			Turns:    index + 1,
			Sessions: 1,
		}
	}

	lines := overviewActivityPreview(query.OverviewView{Trend: trend}, 100)
	if len(lines) != 9 {
		t.Fatalf("overview activity preview rows = %d, want heading, header, and seven days: %#v", len(lines), lines)
	}
	view := strings.Join(lines, "\n")
	for _, want := range []string{"Recent Activity", "2026-01-02", "2026-01-08"} {
		if !strings.Contains(view, want) {
			t.Fatalf("overview activity preview missing %q: %s", want, view)
		}
	}
	if strings.Contains(view, "2026-01-01") {
		t.Fatalf("overview activity preview should keep the latest seven days: %s", view)
	}
}

func TestModelsDefaultSortUsesTokenUsage(t *testing.T) {
	input := explorerInput()
	when := time.Date(2026, 1, 2, 4, 4, 5, 0, time.UTC)
	source := input.Turns[0].Source
	low := usage.NewModelRef("codex", "low-token")
	high := usage.NewModelRef("codex", "high-token")
	input.Turns[0].ModelObservations = nil
	input.Turns[0].TokenUsage = nil
	input.Turns[0].TokenUsageEvents = nil
	input.Turns[0].ObserveModelAt(low, when, source)
	input.Turns[0].AddTokenUsageForModelAt(low, when, usage.TokenUsage{TotalTokens: 100})

	turn := usage.NewTurn("s", "turn-2", 2, source)
	turn.StartedAt = when.Add(time.Minute)
	turn.EndedAt = turn.StartedAt.Add(time.Minute)
	turn.ObserveModelAt(high, turn.StartedAt, source)
	turn.AddTokenUsageForModelAt(high, turn.StartedAt, usage.TokenUsage{TotalTokens: 1_000})
	input.Turns = append(input.Turns, turn)

	state := NewState(input, query.Filter{}, nil)
	state.Route = RouteModels
	rows := state.filteredModels()
	if len(rows) < 2 || rows[0].Model.Name != "high-token" {
		t.Fatalf("models default sort = %#v, want token usage order", rows)
	}
	if !strings.Contains(state.View(), "Models [Token Usage]") {
		t.Fatalf("models default sort label = %s", state.View())
	}
}

func TestActivityViewSwitchesDailyAndMonthly(t *testing.T) {
	state := NewState(longExplorerInput(), query.Filter{}, nil)
	state.Width = 120
	state.Height = 40
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})

	if state.Route != RouteActivity {
		t.Fatalf("activity route = %q", state.Route)
	}
	daily := state.View()
	for _, want := range []string{"Activity [Daily]", "DATE", "SESSIONS", "2026-01-02", "2026-01-03"} {
		if !strings.Contains(daily, want) {
			t.Fatalf("daily activity missing %q: %s", want, daily)
		}
	}

	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	monthly := state.View()
	if !strings.Contains(monthly, "Activity [Monthly]") || !strings.Contains(monthly, "2026-01") {
		t.Fatalf("monthly activity = %s", monthly)
	}
	monthlyRows := strings.Join(state.viewActivity(40), "\n")
	if strings.Contains(monthlyRows, "2026-01-02") || strings.Contains(monthlyRows, "2026-01-03") {
		t.Fatalf("monthly activity still uses daily dates: %s", monthlyRows)
	}
}

func TestOverviewRightAlignsMetricValues(t *testing.T) {
	view := query.OverviewView{
		Sessions:         260,
		Turns:            3_800,
		UserPrompts:      3_000,
		ToolCalls:        31_900,
		SkillUses:        1_800,
		SkillUsesSession: 1_000,
		TokenUsage: usage.TokenUsage{
			TotalTokens:           6_900_000_000,
			InputTokens:           6_900_000_000,
			CachedInputTokens:     6_700_000_000,
			OutputTokens:          27_600_000,
			ReasoningOutputTokens: 12_700_000,
		},
		TokenUsageAvailable: true,
	}

	for _, width := range []int{50, 120} {
		assertOverviewMetricValuesRightAligned(t, overviewActivityLines(view, width))
		assertOverviewMetricValuesRightAligned(t, overviewTokenLines(view, width))
	}
}

func assertOverviewMetricValuesRightAligned(t *testing.T, lines []string) {
	t.Helper()
	wantWidth := -1
	metricCount := 0
	for _, line := range lines {
		if !strings.HasPrefix(line, "  ") {
			continue
		}
		metricCount++
		gotWidth := lipgloss.Width(line)
		if wantWidth == -1 {
			wantWidth = gotWidth
		} else if gotWidth != wantWidth {
			t.Fatalf("overview metric values are not right-aligned: widths=%d,%d lines=%q", wantWidth, gotWidth, lines)
		}
	}
	if metricCount == 0 {
		t.Fatalf("overview contains no metric lines: %q", lines)
	}
}

func TestOverviewRendersHumanReadableWarningSummary(t *testing.T) {
	input := explorerInput()
	input.Warnings = []usage.Warning{
		{Reason: "large_line", Path: "/one.jsonl", Count: 1},
		{Reason: "large_line", Path: "/two.jsonl", Count: 1},
		{Reason: "empty_line", Count: 2},
		{Reason: "malformed_json", Count: 2},
	}

	state := NewState(input, query.Filter{}, nil)
	view := strings.Join(state.overviewLines(), "\n")
	for _, want := range []string{
		"Input notes",
		"2 oversized history records skipped",
		"2 empty history lines skipped",
		"Warnings",
		"2 malformed JSON records skipped",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("overview missing %q: %s", want, view)
		}
	}
	for _, unwanted := range []string{"large_line", "empty_line", "malformed_json", "Warnings (", "Info ("} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("overview contains machine-oriented warning text %q: %s", unwanted, view)
		}
	}
	header := strings.Join(state.headerLines(), "\n")
	if strings.Contains(header, "warning") || strings.Contains(header, "info") {
		t.Fatalf("header should not contain warning or info counts: %s", header)
	}
}

func TestHeaderIncludesCodexSourceContext(t *testing.T) {
	state := NewState(explorerInput(), query.Filter{}, nil)
	state.Width = 120

	header := strings.Join(state.headerLines(), "\n")
	if !strings.Contains(header, "Source:") || !strings.Contains(header, "Codex (~/.codex)") {
		t.Fatalf("header should include Codex source context: %s", header)
	}
}

func TestHeaderUsesConfiguredSourcePath(t *testing.T) {
	state := NewState(explorerInput(), query.Filter{}, nil)
	state.sourcePath = "/tmp/codex"
	state.Width = 120

	header := strings.Join(state.headerLines(), "\n")
	if !strings.Contains(header, "Source:") || !strings.Contains(header, "Codex (/tmp/codex)") {
		t.Fatalf("header should use configured Codex source path: %s", header)
	}
}

func TestHeaderCompactsMetadataWhenItFits(t *testing.T) {
	state := NewState(explorerInput(), query.Filter{}, nil)
	state.Width = 100

	var metadataLines []string
	for _, line := range state.headerLines() {
		if strings.Contains(line, "Source:") || strings.Contains(line, "Period:") {
			metadataLines = append(metadataLines, line)
		}
	}
	if len(metadataLines) != 1 {
		t.Fatalf("header should compact metadata into one line when it fits: %q", metadataLines)
	}
	for _, want := range []string{"Source:", "Agents:", "Period:"} {
		if !strings.Contains(metadataLines[0], want) {
			t.Fatalf("compact header missing %q: %q", want, metadataLines[0])
		}
	}
	if strings.Contains(metadataLines[0], "Filters:") {
		t.Fatalf("compact header should not contain Filters: %q", metadataLines[0])
	}
}

func TestHeaderKeepsMetadataRowsWhenTheyDoNotFit(t *testing.T) {
	state := NewState(explorerInput(), query.Filter{}, nil)
	state.Width = 60

	var metadataLines []string
	for _, line := range state.headerLines() {
		if strings.Contains(line, "Source:") || strings.Contains(line, "Period:") {
			metadataLines = append(metadataLines, line)
		}
	}
	if len(metadataLines) != 2 {
		t.Fatalf("header should keep metadata on two lines when compact form does not fit: %q", metadataLines)
	}
}

func TestSourceFilterTogglesSourcesIndependently(t *testing.T) {
	input := explorerInput()
	opencodeSource := usage.NewOpenCodeSourceRef("/private/opencode.db", "")
	opencodeSession := usage.NewSession("opencode-session", opencodeSource)
	opencodeTurn := usage.NewTurn("opencode-session", "turn", 1, opencodeSource)
	opencodeTurn.StartedAt = time.Date(2026, 1, 3, 3, 4, 5, 0, time.UTC)
	opencodeTurn.EndedAt = opencodeTurn.StartedAt.Add(time.Minute)
	input.Turns = append(input.Turns, opencodeTurn)
	input.Sessions = append(input.Sessions, opencodeSession)
	input.Agents = []string{"codex", "opencode"}
	input.Source = ""
	input.Sources = usage.AllSourceKinds()

	state := NewState(input, query.Filter{Sources: usage.AllSourceKinds()}, nil)
	state.Width = 100
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if !strings.Contains(state.View(), "Sources") || !strings.Contains(state.View(), "[x] Codex") {
		t.Fatalf("source filter view = %s", state.View())
	}
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	state.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if len(state.Filter.Sources) != 2 || state.Filter.Sources[0] != usage.SourceCtx || state.Filter.Sources[1] != usage.SourceOpenCode {
		t.Fatalf("source visibility = %#v", state.Filter.Sources)
	}
	if state.ReadModel.Overview.Turns != 1 || state.ReadModel.Overview.Sources[0] != usage.SourceCtx {
		t.Fatalf("source-filtered overview = %#v", state.ReadModel.Overview)
	}
	if strings.Contains(state.View(), "Codex (~/.codex)") {
		t.Fatalf("disabled source remains in header: %s", state.View())
	}
}

func TestOverviewUsesSingleColumnSections(t *testing.T) {
	state := NewState(explorerInput(), query.Filter{}, nil)
	for _, width := range []int{80, 120} {
		state.Width = width
		lines := state.overviewLines()
		for _, line := range lines {
			if strings.Contains(line, "Usage summary") && strings.Contains(line, "Token Usage") {
				t.Fatalf("overview sections should stay stacked at width %d: %s", width, strings.Join(lines, "\n"))
			}
		}
		recentIndex := -1
		for index, line := range lines {
			if strings.Contains(line, "Recent Sessions") {
				recentIndex = index
				break
			}
		}
		if recentIndex < 1 || lines[recentIndex-1] != "" {
			t.Fatalf("overview should separate Recent Sessions from the previous section at width %d: %#v", width, lines)
		}
	}
}

func TestTUILayoutUsesSpacingInsteadOfPipeSeparators(t *testing.T) {
	state := NewState(explorerInput(), query.Filter{}, nil)
	state.Width = 120
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	state.Update(tea.KeyMsg{Type: tea.KeyEnter})

	view := state.View()
	if strings.Contains(view, " | ") {
		t.Fatalf("TUI should use spacing instead of pipe separators: %s", view)
	}
	if strings.Contains(view, "CLI:") {
		t.Fatalf("session detail should not display CLI version: %s", view)
	}
	separatorLines := 0
	for _, line := range strings.Split(view, "\n") {
		if strings.Count(line, "-") >= 20 {
			separatorLines++
		}
	}
	if separatorLines != 2 {
		t.Fatalf("header and footer should use frame rules, not table rows: separators=%d view=%s", separatorLines, view)
	}
	lines := strings.Split(view, "\n")
	if len(lines) < 2 || strings.Count(lines[len(lines)-2], "-") < 20 {
		t.Fatalf("footer should be preceded by a frame rule: %s", view)
	}
}

func TestSessionTableKeepsLastColumnAtMediumWidth(t *testing.T) {
	header := renderTableHeader(89, sessionCells(89, nil))
	if !strings.Contains(header, "LAST USED") {
		t.Fatalf("session table should keep LAST USED column at medium width: %s", header)
	}
}

func TestSessionTableGivesProjectMoreWidth(t *testing.T) {
	project := "/home/kumi/src/github.com/xkumiyu/catsift"
	cells := sessionCells(100, nil)
	if cells[0].width >= cells[1].width {
		t.Fatalf("session table should give PROJECT more width: cells=%#v", cells)
	}

	row := query.SessionSummary{ProjectPath: project}
	line := renderTableRow(100, false, sessionCellsForRow(100, row))
	if !strings.Contains(line, project) {
		t.Fatalf("session table truncated PROJECT path: cells=%#v line=%q", sessionCellsForRow(100, row), line)
	}
}

func TestSessionTableReservesRightMarginForRelativeTime(t *testing.T) {
	row := query.SessionSummary{
		ProjectPath: "/home/kumi/src/github.com/xkumiyu/catsift",
		EndedAt:     time.Now().UTC().Add(-time.Minute),
	}
	cells := sessionCellsForRow(100, row)
	if got := cells[len(cells)-1].width; got != 12 {
		t.Fatalf("session table should use a compact relative-time column: width=%d cells=%#v", got, cells)
	}
	line := renderTableRow(100, false, cells)
	if lipgloss.Width(line) >= 100 {
		t.Fatalf("session table should reserve one trailing column: rendered=%d line=%q", lipgloss.Width(line), line)
	}
	if !strings.HasSuffix(strings.TrimRight(line, " "), "ago") {
		t.Fatalf("session table should keep the complete relative-time suffix: %q", line)
	}
}

func TestSessionTableKeepsRelativeLastUsedValue(t *testing.T) {
	row := query.Build(explorerInput(), query.Filter{}).Sessions[0]
	want := formatRelativeTime(row.EndedAt)
	for _, width := range []int{80, 89, 90, 99, 100, 120} {
		cells := sessionCellsForRow(width, row)
		line := renderTableRow(width, false, cells)
		if cells[len(cells)-1].width < relativeTimeColumnWidth {
			t.Fatalf("session table did not reserve LAST USED width at %d: cells=%#v", width, cells)
		}
		if !strings.Contains(line, want) {
			t.Fatalf("session table truncated LAST USED value at width %d (rendered=%d cells=%#v): %q", width, lipgloss.Width(line), cells, line)
		}
	}
}

func TestSessionListRowsMatchSessionDisplayFormat(t *testing.T) {
	const title = "Implement usage explorer"
	const id = "01a094e0"

	tests := []struct {
		name          string
		titledCells   []tableCell
		untitledCells []tableCell
	}{
		{
			name:          "sessions",
			titledCells:   sessionCellsForRow(120, query.SessionSummary{Title: title, ID: "01a094e0-0e59-7493-a4b0-3681af2c63e3"}),
			untitledCells: sessionCellsForRow(120, query.SessionSummary{ID: "01a094e0-0e59-7493-a4b0-3681af2c63e3"}),
		},
		{
			name: "model detail",
			titledCells: modelSessionCellsForRow(120, query.ModelSessionUsage{
				Title: title, ID: "01a094e0-0e59-7493-a4b0-3681af2c63e3", Source: usage.SourceCodex, Agent: "codex",
			}),
			untitledCells: modelSessionCellsForRow(120, query.ModelSessionUsage{
				ID: "01a094e0-0e59-7493-a4b0-3681af2c63e3", Source: usage.SourceCodex, Agent: "codex",
			}),
		},
		{
			name: "skill detail",
			titledCells: skillSessionCellsForRow(120, query.SkillSessionUsage{
				Title: title, SessionID: "01a094e0-0e59-7493-a4b0-3681af2c63e3", Source: usage.SourceCodex, Agent: "codex",
			}),
			untitledCells: skillSessionCellsForRow(120, query.SkillSessionUsage{
				SessionID: "01a094e0-0e59-7493-a4b0-3681af2c63e3", Source: usage.SourceCodex, Agent: "codex",
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			titledLine := renderTableRow(120, false, tt.titledCells)
			if !strings.Contains(titledLine, title) || strings.Contains(titledLine, "("+id+")") {
				t.Fatalf("titled session row = %q, want title without ID", titledLine)
			}
			untitledLine := renderTableRow(120, false, tt.untitledCells)
			if !strings.Contains(untitledLine, mutedStyle.Render("("+id+")")) {
				t.Fatalf("untitled session row = %q, want muted short ID", untitledLine)
			}
		})
	}
}

func TestSelectedSessionRowHighlightsEntireLine(t *testing.T) {
	const width = 80
	cells := sessionCellsForRow(width, query.SessionSummary{ID: "01a094e0-0e59-7493-a4b0-3681af2c63e3"})
	unmutedCells := append([]tableCell(nil), cells...)
	unmutedCells[0].muted = false

	line := renderTableRow(width, true, cells)
	if got := lipgloss.Width(line); got != width {
		t.Fatalf("selected session row width = %d, want %d: %q", got, width, line)
	}
	if want := renderTableRow(width, true, unmutedCells); line != want {
		t.Fatalf("selected session row should keep row highlight across ID: got %q, want %q", line, want)
	}
}

func TestSessionTableKeepsLastUsedAwayFromRightEdge(t *testing.T) {
	row := query.SessionSummary{
		Title:       "feat/add-tui-usage-explorer をレビューする",
		ProjectPath: "/home/kumi/src/github.com/xkumiyu/catsift.feat-add-tui-usage-explorer",
		EndedAt:     time.Now().UTC().Add(-3 * time.Hour),
	}
	line := renderTableRow(100, false, sessionCellsForRow(100, row))
	if lipgloss.Width(line) > 98 {
		t.Fatalf("session table should leave two trailing columns: rendered=%d line=%q", lipgloss.Width(line), line)
	}
	if !strings.HasSuffix(strings.TrimRight(line, " "), "3h ago") {
		t.Fatalf("session table should keep the complete relative-time value: %q", line)
	}
}

func TestTurnTableOmitsRedundantOrdinalColumn(t *testing.T) {
	for _, width := range []int{80, 120} {
		cells := turnCells(width, nil)
		if len(cells) == 0 || cells[0].value != "TURN" {
			t.Fatalf("turn table should start with TURN at width %d: %#v", width, cells)
		}
		for _, cell := range cells {
			if cell.value == "#" {
				t.Fatalf("turn table should not include redundant ordinal column at width %d: %#v", width, cells)
			}
		}
	}
}

func TestTurnTableShowsTurnContext(t *testing.T) {
	row := query.TurnSummary{
		ID:                  "turn-1",
		Ordinal:             3,
		StartedAt:           time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Aborted:             true,
		Models:              []usage.ModelRef{{Provider: "codex", Name: "gpt-example"}},
		TokenUsage:          usage.TokenUsage{TotalTokens: 1200},
		TokenUsageAvailable: true,
		Tools:               []string{"exec", "shell"},
		Skills:              []string{"git"},
	}
	for _, width := range []int{80, 120} {
		header := renderTableHeader(width, turnCellsForSession(width, nil, []query.TurnSummary{row}))
		for _, want := range []string{"MODEL", "TOKENS", "TOOLS", "SKILLS", "STATUS", "TIME"} {
			if !strings.Contains(header, want) {
				t.Fatalf("turn table header omitted %q at width %d: %s", want, width, header)
			}
		}
	}
	cells := turnCellsForSession(120, &row, []query.TurnSummary{row})
	if cells[0].value != "03" || cells[3].value != "2" || cells[4].value != "1" || cells[6].value != "03:04" || !cells[6].right {
		t.Fatalf("turn table context = %#v", cells)
	}
	doneRow := row
	doneRow.Aborted = false
	doneCells := turnCellsForSession(120, &doneRow, []query.TurnSummary{doneRow})
	if doneCells[5].value != "done" {
		t.Fatalf("normal turn status = %q, want done", doneCells[5].value)
	}
	line := renderTableRow(120, false, cells)
	for _, want := range []string{"gpt-example", "aborted"} {
		if !strings.Contains(line, want) {
			t.Fatalf("turn table row omitted %q: %s", want, line)
		}
	}
	for _, unwanted := range []string{"exec", "shell", "git"} {
		if strings.Contains(line, unwanted) {
			t.Fatalf("turn table should defer %q to turn detail: %s", unwanted, line)
		}
	}
}

func TestTurnTableUsesDateWhenSessionSpansDays(t *testing.T) {
	rows := []query.TurnSummary{
		{Ordinal: 1, StartedAt: time.Date(2026, 9, 12, 23, 58, 0, 0, time.UTC)},
		{Ordinal: 2, StartedAt: time.Date(2026, 9, 13, 0, 12, 0, 0, time.UTC)},
	}
	for i, want := range []string{"09/12 23:58", "09/13 00:12"} {
		cells := turnCellsForSession(120, &rows[i], rows)
		if cells[6].value != want {
			t.Fatalf("turn time = %q, want %q", cells[6].value, want)
		}
	}

	rows[1].StartedAt = time.Date(2027, 1, 1, 0, 12, 0, 0, time.UTC)
	cells := turnCellsForSession(120, &rows[1], rows)
	if cells[6].value != "2027-01-01 00:12" {
		t.Fatalf("cross-year turn time = %q", cells[6].value)
	}
}

func TestTurnTableReservesRightMarginAndFillsSelectedWidth(t *testing.T) {
	for _, width := range []int{70, 80, 100, 120} {
		line := renderTableHeader(width, turnCells(width, nil))
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("turn table header width = %d at width %d, want <= %d: %q", got, width, width, line)
		}
		selected := renderTableRow(width, true, turnCells(width, nil))
		if got := lipgloss.Width(selected); got != width {
			t.Fatalf("selected turn table header width = %d at width %d, want %d: %q", got, width, width, selected)
		}
	}
}

func TestTableHeadersUseMutedBoldStyle(t *testing.T) {
	if !headerStyle.GetBold() {
		t.Fatal("table headers should remain bold")
	}
	if !reflect.DeepEqual(headerStyle.GetForeground(), mutedStyle.GetForeground()) {
		t.Fatalf("table headers should use muted foreground: header=%v muted=%v", headerStyle.GetForeground(), mutedStyle.GetForeground())
	}
}

func TestSearchFiltersSkillRows(t *testing.T) {
	input := explorerInput()
	when := input.Turns[0].StartedAt
	input.Turns[0].SkillEvidence = append(input.Turns[0].SkillEvidence, usage.SkillEvidence{
		SkillName: "git",
		Mode:      usage.ModeExplicit,
		Method:    usage.MethodExplicitRequest,
		State:     usage.StateConfirmed,
		Timestamp: when,
		Source:    input.Turns[0].Source,
	})
	extra := usage.NewTurn("s", "deploy-turn", 2, input.Turns[0].Source)
	extra.StartedAt = when.Add(time.Minute)
	extra.EndedAt = extra.StartedAt.Add(time.Minute)
	extra.SkillEvidence = []usage.SkillEvidence{{
		SkillName: "deploy",
		Mode:      usage.ModeExplicit,
		Method:    usage.MethodExplicitRequest,
		State:     usage.StateConfirmed,
		Timestamp: extra.StartedAt,
		Source:    extra.Source,
	}}
	input.Turns = append(input.Turns, extra)

	state := NewState(input, query.Filter{}, nil)
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g', 'i', 't'}})
	state.Update(tea.KeyMsg{Type: tea.KeyEnter})

	view := state.View()
	if len(state.ReadModel.Skills) != 3 {
		t.Fatalf("row search should not narrow the underlying read model: skills=%d", len(state.ReadModel.Skills))
	}
	if !strings.Contains(view, "git") {
		t.Fatalf("filtered skills view missing git row: %s", view)
	}
	if strings.Contains(view, "review") {
		t.Fatalf("filtered skills view contains unrelated review row: %s", view)
	}
	state.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if state.Route != RouteSkillDetail || state.Filter.Search != "" {
		t.Fatalf("skill detail should not inherit the list search: route=%q search=%q", state.Route, state.Filter.Search)
	}
	state.Width = 120
	detailView := state.View()
	for _, want := range []string{"Skill detail", "Skill: git", "Sessions", "1/1", "TURNS", "LAST USED"} {
		if !strings.Contains(detailView, want) {
			t.Fatalf("skill detail missing %q: %s", want, detailView)
		}
	}
	for _, unwanted := range []string{"Skills >", "Sessions 1  |", "FIRST USED", "USES", "Uses 1"} {
		if strings.Contains(detailView, unwanted) {
			t.Fatalf("skill detail contains obsolete text %q: %s", unwanted, detailView)
		}
	}
	if !strings.Contains(detailView, "Usage mode:") || !strings.Contains(detailView, "Explicit 1") || !strings.Contains(detailView, "Evidence:") || !strings.Contains(detailView, "Confirmed 1") || strings.Contains(detailView, "=") {
		t.Fatalf("skill detail should use human-readable counts: %s", detailView)
	}
	state.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if state.Route != RouteSkills || state.Filter.Search != "git" {
		t.Fatalf("returning to skills should restore the list search: route=%q search=%q", state.Route, state.Filter.Search)
	}
}

func TestDetailHeaderKeepsTopLevelTab(t *testing.T) {
	state := NewState(explorerInput(), query.Filter{}, nil)
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	state.Update(tea.KeyMsg{Type: tea.KeyEnter})

	view := state.View()
	if strings.Contains(view, "catsift / Skill detail") {
		t.Fatalf("detail route should not replace the top-level header: %s", view)
	}
	if !strings.Contains(view, "[4] Skills") {
		t.Fatalf("detail route should keep Skills as active top-level tab: %s", view)
	}
}

func TestSessionFiltersCanSelectAgentAndProject(t *testing.T) {
	input := explorerInput()
	input.Sessions[0].ProjectPath = "/workspace/project"
	for _, test := range []struct {
		key  rune
		want func(query.Filter) string
	}{
		{key: 'a', want: func(filter query.Filter) string { return filter.Agent }},
		{key: 'p', want: func(filter query.Filter) string { return filter.Project }},
	} {
		state := NewState(input, query.Filter{}, nil)
		state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
		state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{test.key}})
		if state.Route != RouteOverview || test.want(state.Filter) == "" {
			t.Fatalf("session filter %q = route:%q filter:%#v", test.key, state.Route, state.Filter)
		}
	}
}

func TestPeriodInputParsesRelativeAndCalendarRanges(t *testing.T) {
	now := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		input string
		from  time.Time
		to    time.Time
	}{
		{input: "all"},
		{input: "7", from: now.Add(-7 * 24 * time.Hour), to: now},
		{input: "2026-01-02", from: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), to: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)},
		{input: "2026-01-02..2026-01-04", from: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), to: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)},
		{input: "2026-01-02..", from: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
		{input: "..2026-01-04", to: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			from, to, err := parsePeriodInput(tt.input, now)
			if err != nil {
				t.Fatalf("parsePeriodInput(%q) error = %v", tt.input, err)
			}
			if !from.Equal(tt.from) || !to.Equal(tt.to) {
				t.Fatalf("parsePeriodInput(%q) = %v, %v; want %v, %v", tt.input, from, to, tt.from, tt.to)
			}
		})
	}
}

func TestPeriodInputRejectsInvalidRanges(t *testing.T) {
	now := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	for _, input := range []string{"0", "yesterday", "2026-01-04..2026-01-02", "2026-01-01..2026-01-02..2026-01-03"} {
		if _, _, err := parsePeriodInput(input, now); err == nil {
			t.Errorf("parsePeriodInput(%q) unexpectedly succeeded", input)
		}
	}
}

func TestPeriodKeyAppliesAndClearsFilter(t *testing.T) {
	now := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	state := NewState(explorerInput(), query.Filter{}, nil)
	state.now = now
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if !state.periodEditing {
		t.Fatal("d should start period input")
	}
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("7")})
	state.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if state.periodEditing || !state.Filter.From.Equal(now.Add(-7*24*time.Hour)) || !state.Filter.To.Equal(now) {
		t.Fatalf("period filter = editing:%v filter:%#v", state.periodEditing, state.Filter)
	}
	overview := strings.Join(state.overviewLines(), "\n")
	if state.ReadModel.Overview.Turns != 0 || strings.Contains(state.View(), "Filters:") || !strings.Contains(overview, "Input notes") || !strings.Contains(overview, "No usage found for the selected period.") {
		t.Fatalf("period filter was not applied: overview=%#v view=%s", state.ReadModel.Overview, state.View())
	}

	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	state.periodInput = "all"
	state.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !state.Filter.From.IsZero() || !state.Filter.To.IsZero() || state.ReadModel.Overview.Turns != 1 {
		t.Fatalf("cleared period filter = %#v overview=%#v", state.Filter, state.ReadModel.Overview)
	}
}

func TestPeriodInfoReportsRequestedAndActualBoundaries(t *testing.T) {
	state := NewState(explorerInput(), query.Filter{
		From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2026, 1, 11, 0, 0, 0, 0, time.UTC),
	}, nil)
	state.now = time.Date(2026, 1, 11, 0, 0, 0, 0, time.UTC)
	header := strings.Join(state.headerLines(), "\n")
	overview := strings.Join(state.overviewLines(), "\n")
	if strings.Contains(header, "info:") {
		t.Fatalf("period info should not be in header: %s", header)
	}
	for _, want := range []string{
		"Input notes",
		"2026-01-02 to 2026-01-02",
		"selected period starts before the first usage record (2026-01-02)",
		"selected period ends after the last usage record (2026-01-02)",
	} {
		if !strings.Contains(header+"\n"+overview, want) {
			t.Fatalf("period view missing %q: header=%s overview=%s", want, header, overview)
		}
	}
	if strings.Contains(header, "Filters:") || strings.Contains(header, "period=") || strings.Contains(overview, "period=") {
		t.Fatalf("period view contains obsolete filter summary: header=%s overview=%s", header, overview)
	}
}

func TestPeriodInfoIsHiddenWhenActualRangeMatches(t *testing.T) {
	state := NewState(explorerInput(), query.Filter{
		From: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC),
	}, nil)
	view := strings.Join(append(state.headerLines(), state.overviewLines()...), "\n")
	for _, note := range []string{"selected period starts before", "selected period ends after", "No usage found for the selected period."} {
		if strings.Contains(view, note) {
			t.Fatalf("period view reported a false mismatch: %s", view)
		}
	}
}

func TestPeriodApplyShowsInlineNotice(t *testing.T) {
	state := NewState(explorerInput(), query.Filter{}, nil)
	state.Width = 80
	state.now = time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	state.periodInput = "2026-01-01..2026-01-11"
	_, cmd := state.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("period mismatch notice should not use a timer")
	}
	headerLines := state.headerLines()
	header := strings.Join(headerLines, "\n")
	for _, want := range []string{
		"Notice: Period partially covered",
		"requested: 2026-01-01 to 2026-01-11;",
		"actual:",
		"2026-01-02 to 2026-01-02)",
	} {
		if !strings.Contains(header, want) {
			t.Fatalf("period notice missing %q: %s", want, header)
		}
	}
	if strings.Contains(header, "clears when") || strings.Contains(header, "press Tab") || strings.Contains(header, "dismiss") {
		t.Fatalf("period notice should not contain operation instructions: %s", header)
	}
	tabsIndex, noticeIndex := -1, -1
	dividerIndex := -1
	for index, line := range headerLines {
		if strings.Contains(line, "Overview") && strings.Contains(line, "Models") {
			tabsIndex = index
		}
		if strings.Contains(line, "Notice:") {
			noticeIndex = index
		}
		if strings.Contains(line, strings.Repeat("-", state.Width)) {
			dividerIndex = index
		}
	}
	if noticeIndex != tabsIndex+1 {
		t.Fatalf("period notice should be directly below tabs: tabs=%d notice=%d header=%s", tabsIndex, noticeIndex, header)
	}
	if dividerIndex-noticeIndex < 2 {
		t.Fatalf("period notice should wrap without truncation: %s", header)
	}
	for _, line := range headerLines[noticeIndex:dividerIndex] {
		if strings.HasPrefix(ansi.Strip(line), "  ") {
			t.Fatalf("period notice continuation should not be indented: %q", line)
		}
		if lipgloss.Width(line) > state.Width || strings.Contains(line, "…") {
			t.Fatalf("period notice line was truncated or exceeded width: %q", line)
		}
	}
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	if header := strings.Join(state.headerLines(), "\n"); strings.Contains(header, "Notice:") {
		t.Fatalf("period notice survived tab movement: %s", header)
	}
}

func TestPeriodApplyNoDataNoticeStaysVisible(t *testing.T) {
	state := NewState(explorerInput(), query.Filter{}, nil)
	state.Width = 120
	state.now = time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	state.periodInput = "2027-01-01"
	_, cmd := state.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("no-data notice should remain visible")
	}
	header := strings.Join(state.headerLines(), "\n")
	if !strings.Contains(header, "Notice: No usage found for the selected period.") {
		t.Fatalf("no-data period notice missing: %s", header)
	}
}

func TestClearTUIFiltersClearsPeriod(t *testing.T) {
	state := NewState(explorerInput(), query.Filter{
		From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC),
	}, nil)
	state.periodNotice = "info: stale period notice"
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if !state.Filter.From.IsZero() || !state.Filter.To.IsZero() {
		t.Fatalf("clear retained period filter: %#v", state.Filter)
	}
	if state.periodNotice != "" {
		t.Fatalf("clear retained period notice: %q", state.periodNotice)
	}
}

func TestBoundLinesTruncatesWithoutWrapping(t *testing.T) {
	line := titleStyle.Render(strings.Repeat("long-value ", 10))
	view := boundView([]string{line}, 12, 2)
	lines := strings.Split(view, "\n")
	if len(lines) != 1 || lipgloss.Width(lines[0]) > 12 {
		t.Fatalf("bounded line wrapped or exceeded width: %#v", lines)
	}
}

func TestActivityScrollsThroughAllDailyRows(t *testing.T) {
	input := explorerInput()
	source := input.Turns[0].Source
	for i := 0; i < 18; i++ {
		turn := usage.NewTurn("s", "daily-"+string(rune('a'+i)), i+2, source)
		turn.StartedAt = time.Date(2026, 1, 3+i, 0, 0, 0, 0, time.UTC)
		turn.EndedAt = turn.StartedAt.Add(time.Minute)
		input.Turns = append(input.Turns, turn)
	}

	state := NewState(input, query.Filter{}, nil)
	state.Update(tea.WindowSizeMsg{Width: 90, Height: 14})
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	if state.rowCount() <= state.visibleHeight() {
		t.Fatalf("activity should be scrollable: rows=%d visible=%d", state.rowCount(), state.visibleHeight())
	}
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if state.Offset == 0 {
		t.Fatalf("one j key should scroll activity: offset=%d", state.Offset)
	}
	state.Update(tea.KeyMsg{Type: tea.KeyEnd})
	maxOffset := state.rowCount() - state.visibleHeight()
	if state.Offset != maxOffset {
		t.Fatalf("end should move to the bottom of the overview: offset=%d want=%d", state.Offset, maxOffset)
	}
	state.Update(tea.KeyMsg{Type: tea.KeyHome})
	if state.Offset != 0 {
		t.Fatalf("home should move to the top of the overview: offset=%d", state.Offset)
	}

	for i := 0; i < state.rowCount(); i++ {
		state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	}
	if state.Offset == 0 || !strings.Contains(strings.Join(state.viewActivity(30), "\n"), "2026-01-20") {
		t.Fatalf("end did not reveal the latest daily row: offset=%d view=%s", state.Offset, state.View())
	}

	state.Selected, state.Offset = 0, 0
	seenFirstDailyRow := false
	for i := 0; i < state.rowCount(); i++ {
		state.Update(tea.KeyMsg{Type: tea.KeyDown})
		if strings.Contains(strings.Join(state.viewActivity(30), "\n"), "2026-01-03") {
			seenFirstDailyRow = true
			break
		}
	}
	if !seenFirstDailyRow {
		t.Fatalf("scrolling activity never revealed the first daily row: %s", state.View())
	}
}

func TestStateRoutesDetailsAndMaintainsViewport(t *testing.T) {
	state := NewState(explorerInput(), query.Filter{}, nil)
	state.Update(tea.WindowSizeMsg{Width: 60, Height: 8})
	if state.Width != 60 || state.Height != 8 {
		t.Fatalf("window size = %dx%d", state.Width, state.Height)
	}
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	if state.Route != RouteModels || state.Selected != 0 {
		t.Fatalf("models route = %#v", state)
	}
	state.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if state.Route != RouteModelDetail || state.ParentRoute != RouteModels {
		t.Fatalf("model detail route = %#v", state)
	}
	state.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if state.Route != RouteModels {
		t.Fatalf("back route = %q", state.Route)
	}

	for i := 0; i < 20; i++ {
		state.updateSelection(1)
	}
	if state.Selected != 0 || state.Offset != 0 {
		t.Fatalf("selection should stay within one row: selected=%d offset=%d", state.Selected, state.Offset)
	}

	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	if state.Filter.Skill != "review" || state.ReadModel.Overview.SkillUses != 1 {
		t.Fatalf("skill filter = %#v overview=%#v", state.Filter, state.ReadModel.Overview)
	}
}

func TestStateWindowsLongModelListAndKeepsSelectedRowVisible(t *testing.T) {
	input := explorerInput()
	source := input.Turns[0].Source
	for i := 0; i < 12; i++ {
		turn := usage.NewTurn("s", "extra-"+string(rune('a'+i)), i+2, source)
		turn.StartedAt = time.Date(2026, 1, 3, 0, i, 0, 0, time.UTC)
		turn.EndedAt = turn.StartedAt.Add(time.Minute)
		turn.ObserveModelAt(usage.NewModelRef("codex", "model-"+string(rune('a'+i))), turn.StartedAt, source)
		input.Turns = append(input.Turns, turn)
	}
	state := NewState(input, query.Filter{}, nil)
	state.Update(tea.WindowSizeMsg{Width: 50, Height: 10})
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	for i := 0; i < 8; i++ {
		state.updateSelection(1)
	}
	if state.Selected == 0 || state.Offset == 0 {
		t.Fatalf("viewport did not advance: selected=%d offset=%d", state.Selected, state.Offset)
	}
	lines := strings.Split(state.View(), "\n")
	if len(lines) > state.Height {
		t.Fatalf("view exceeds terminal height: %d > %d", len(lines), state.Height)
	}
}

func TestStateSearchReloadAndBoundedView(t *testing.T) {
	input := explorerInput()
	input.Turns[0].RuntimeTools = []usage.ToolObservation{{Arguments: "secret", Source: input.Turns[0].Source}}
	var reloaded bool
	state := NewState(input, query.Filter{}, func() (query.Input, error) {
		reloaded = true
		return input, nil
	})
	state.Update(tea.WindowSizeMsg{Width: 32, Height: 6})
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g', 'p', 't'}})
	state.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if state.Filter.Search != "gpt" || len(state.ReadModel.Models) != 1 {
		t.Fatalf("search filter = %#v models=%#v", state.Filter, state.ReadModel.Models)
	}
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if !state.Loading {
		t.Fatal("reload should keep the current view while loading")
	}
	if cmd := state.pendingReload; cmd == nil {
		t.Fatal("reload command was not scheduled")
	} else if msg := cmd(); msg == nil {
		t.Fatal("reload command returned no message")
	}
	if !reloaded {
		t.Fatal("reload callback was not called")
	}
	state.Update(ReloadResultMsg{Input: input})
	if state.Loading || state.Status != "" {
		t.Fatalf("reload state = loading:%v status:%q", state.Loading, state.Status)
	}
	if state.Input.Turns[0].Source.Path != "" || state.Input.Turns[0].RuntimeTools[0].Arguments != "" {
		t.Fatalf("renderer state retained raw input: %#v", state.Input.Turns[0])
	}

	for _, line := range strings.Split(state.View(), "\n") {
		if lipgloss.Width(line) > state.Width {
			t.Fatalf("view line exceeds width: %q", line)
		}
	}
	if strings.Contains(state.View(), "history.jsonl") || strings.Contains(state.View(), "arguments") {
		t.Fatalf("view leaked raw source data: %s", state.View())
	}
}

func TestDetailViewKeepsRowsAndFooterWithinTheFrame(t *testing.T) {
	state := NewState(longExplorerInput(), query.Filter{}, nil)
	state.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	state.Update(tea.KeyMsg{Type: tea.KeyEnter})
	view := state.View()
	if !strings.Contains(view, "Turns") || strings.Contains(view, "1/25") || strings.Contains(view, "Turns 25  |") {
		t.Fatalf("session detail omitted the turn section: %s", view)
	}
	if !strings.Contains(view, "j/k move") || !strings.Contains(view, "Enter open") || !strings.Contains(view, "b/Esc back") || !strings.Contains(view, "q quit") {
		t.Fatalf("session detail footer is not visible: %s", view)
	}
	if !strings.Contains(view, "Enter open") || !strings.Contains(view, "> ") {
		t.Fatalf("session detail should advertise turn detail action: %s", view)
	}
	body := view[strings.Index(view, "Session detail"):]
	if !strings.Contains(body, "Provider:") || !strings.Contains(body, "Project:") || strings.Contains(body, "Source:") || strings.Contains(body, "Agent:") {
		t.Fatalf("session detail metadata is duplicated or misaligned: %s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > state.Width {
			t.Fatalf("detail line exceeds width: %q", line)
		}
	}
}

func TestTurnDetailShowsMetadataAndReturnsToSession(t *testing.T) {
	input := explorerInput()
	input.Turns[0].RuntimeTools = []usage.ToolObservation{{CanonicalName: "exec", Arguments: "secret", Source: input.Turns[0].Source}}
	state := NewState(input, query.Filter{}, nil)
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	state.Update(tea.KeyMsg{Type: tea.KeyEnter})
	state.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if state.Route != RouteTurnDetail {
		t.Fatalf("turn detail route = %q", state.Route)
	}
	view := state.View()
	for _, want := range []string{"Turn detail", "Turn: turn", "Session: (s)", "Model:", "codex/gpt-example", "Tokens:", "7", "Tools:", "exec", "Skills:", "review", "Status:", "done", "b/Esc back"} {
		if !strings.Contains(view, want) {
			t.Fatalf("turn detail omitted %q: %s", want, view)
		}
	}
	for _, unwanted := range []string{"secret", "arguments", "Enter open"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("turn detail exposed or advertised %q: %s", unwanted, view)
		}
	}

	state.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if state.Route != RouteSessionDetail || state.Selected != 0 {
		t.Fatalf("return to session detail = route:%q selected:%d", state.Route, state.Selected)
	}
}

func TestSessionDetailMovesSelectionAndScrollsViewport(t *testing.T) {
	state := NewState(longExplorerInput(), query.Filter{}, nil)
	state.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	state.Update(tea.KeyMsg{Type: tea.KeyEnter})

	state.Update(tea.KeyMsg{Type: tea.KeyDown})
	if state.Selected != 1 || state.Offset != 0 {
		t.Fatalf("session detail down should move selection: selected=%d offset=%d", state.Selected, state.Offset)
	}
	visible := state.visibleHeight()
	for state.Selected < visible {
		state.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if state.Selected != visible || state.Offset != 1 {
		t.Fatalf("session detail should scroll after selection leaves viewport: selected=%d offset=%d visible=%d", state.Selected, state.Offset, visible)
	}

	input := longExplorerInput()
	input.Turns = input.Turns[:2]
	state = NewState(input, query.Filter{}, nil)
	state.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	state.Update(tea.KeyMsg{Type: tea.KeyEnter})
	state.Update(tea.KeyMsg{Type: tea.KeyDown})
	if state.Selected != 1 || state.Offset != 0 {
		t.Fatalf("session detail should move selection when all rows fit: selected=%d offset=%d", state.Selected, state.Offset)
	}
}

func TestNestedDetailsReturnToThePreviousSelectionContext(t *testing.T) {
	state := NewState(explorerInput(), query.Filter{}, nil)
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	state.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if state.Route != RouteModelDetail {
		t.Fatalf("model detail route = %q", state.Route)
	}
	if !strings.Contains(state.View(), "Model: codex/gpt-example") {
		t.Fatalf("model detail should label selected model: %s", state.View())
	}
	if strings.Contains(state.View(), "Models >") {
		t.Fatalf("model detail should not display a breadcrumb: %s", state.View())
	}
	if strings.Contains(state.View(), "Sessions 1  |") || !strings.Contains(state.View(), "Sessions") || !strings.Contains(state.View(), "1/1") {
		t.Fatalf("model detail duplicated session count: %s", state.View())
	}
	state.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if state.Route != RouteModelDetail {
		t.Fatalf("left arrow should not leave detail view: route=%q", state.Route)
	}
	state.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if state.Route != RouteSessionDetail || len(state.history) != 2 {
		t.Fatalf("nested session detail = route:%q history:%d", state.Route, len(state.history))
	}
	if !strings.Contains(state.View(), "Session name: —") || !strings.Contains(state.View(), "Session ID: s") || strings.Contains(state.View(), "Sessions >") {
		t.Fatalf("session detail should not display a breadcrumb: %s", state.View())
	}
	state.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if state.Route != RouteTurnDetail || len(state.history) != 3 {
		t.Fatalf("nested turn detail = route:%q history:%d", state.Route, len(state.history))
	}
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	if state.Route != RouteSessionDetail || state.selectedKey == "" {
		t.Fatalf("return to session detail lost context: route:%q key:%q", state.Route, state.selectedKey)
	}
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	if state.Route != RouteModelDetail || state.selectedKey == "" {
		t.Fatalf("return to model detail lost context: route:%q key:%q", state.Route, state.selectedKey)
	}
	state.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if state.Route != RouteModels || state.Selected != 0 {
		t.Fatalf("return to models lost selection: route:%q selected:%d", state.Route, state.Selected)
	}
}

func TestHelpAndTabNavigation(t *testing.T) {
	state := NewState(explorerInput(), query.Filter{}, nil)
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if !strings.Contains(state.View(), "Navigation") {
		t.Fatalf("help view missing navigation section: %s", state.View())
	}
	state.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if state.help {
		t.Fatal("escape should close help")
	}
	state.Update(tea.KeyMsg{Type: tea.KeyTab})
	if state.Route != RouteActivity {
		t.Fatalf("tab route = %q, want activity", state.Route)
	}
	state.Update(tea.KeyMsg{Type: tea.KeyTab})
	if state.Route != RouteModels {
		t.Fatalf("second tab route = %q, want models", state.Route)
	}
	state.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if state.Route != RouteActivity {
		t.Fatalf("shift-tab route = %q, want activity", state.Route)
	}
	state.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if state.Route != RouteOverview {
		t.Fatalf("second shift-tab route = %q, want overview", state.Route)
	}
}

func TestSortKeyCyclesListPresets(t *testing.T) {
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	middle := old.Add(24 * time.Hour)
	newer := middle.Add(24 * time.Hour)
	key := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}

	state := NewState(explorerInput(), query.Filter{}, nil)
	state.ReadModel.Models = []query.ModelSummary{
		{Model: usage.NewModelRef("codex", "old"), Turns: 1, Sessions: 1, LastUsed: old},
		{Model: usage.NewModelRef("codex", "new"), Turns: 1, Sessions: 1, LastUsed: newer},
		{Model: usage.NewModelRef("codex", "middle"), Turns: 1, Sessions: 1, LastUsed: middle},
	}
	state.Route, state.Selected, state.Offset = RouteModels, 2, 2
	state.Update(key)
	if got := []string{state.filteredModels()[0].Model.Name, state.filteredModels()[1].Model.Name, state.filteredModels()[2].Model.Name}; !reflect.DeepEqual(got, []string{"new", "middle", "old"}) {
		t.Fatalf("models last-used sort = %#v", got)
	}
	if state.Selected != 0 || state.Offset != 0 || !strings.Contains(state.View(), "Models [Last Used]") {
		t.Fatalf("models sort state = selected:%d offset:%d view:%s", state.Selected, state.Offset, state.View())
	}
	state.Update(key)
	if got := []string{state.filteredModels()[0].Model.Name, state.filteredModels()[1].Model.Name, state.filteredModels()[2].Model.Name}; !reflect.DeepEqual(got, []string{"middle", "new", "old"}) {
		t.Fatalf("models name sort = %#v", got)
	}
	state.Update(key)
	if got := []string{state.filteredModels()[0].Model.Name, state.filteredModels()[1].Model.Name, state.filteredModels()[2].Model.Name}; !reflect.DeepEqual(got, []string{"old", "new", "middle"}) {
		t.Fatalf("models default sort = %#v", got)
	}
	if !strings.Contains(state.View(), "Models [Token Usage]") {
		t.Fatalf("models default sort label = %s", state.View())
	}

	state = NewState(explorerInput(), query.Filter{}, nil)
	state.ReadModel.Skills = []query.SkillSummary{
		{Name: "zeta", Uses: 1, LastUsed: newer},
		{Name: "alpha", Uses: 1, LastUsed: old},
		{Name: "beta", Uses: 1, LastUsed: middle},
	}
	state.Route = RouteSkills
	state.Update(key)
	if got := []string{state.filteredSkills()[0].Name, state.filteredSkills()[1].Name, state.filteredSkills()[2].Name}; !reflect.DeepEqual(got, []string{"zeta", "beta", "alpha"}) {
		t.Fatalf("skills last-used sort = %#v", got)
	}
	state.Update(key)
	if got := []string{state.filteredSkills()[0].Name, state.filteredSkills()[1].Name, state.filteredSkills()[2].Name}; !reflect.DeepEqual(got, []string{"alpha", "beta", "zeta"}) {
		t.Fatalf("skills name sort = %#v", got)
	}

	state = NewState(explorerInput(), query.Filter{}, nil)
	state.ReadModel.Sessions = []query.SessionSummary{
		{Key: "z", ID: "z", Title: "zeta", Turns: 1, EndedAt: old},
		{Key: "a", ID: "a", Title: "alpha", Turns: 2, EndedAt: newer},
		{Key: "b", ID: "b", Title: "beta", Turns: 3, EndedAt: middle},
	}
	state.Route = RouteSessions
	state.Update(key)
	if got := []string{state.filteredSessions()[0].Title, state.filteredSessions()[1].Title, state.filteredSessions()[2].Title}; !reflect.DeepEqual(got, []string{"alpha", "beta", "zeta"}) {
		t.Fatalf("sessions name sort = %#v", got)
	}
	state.Update(key)
	if got := []string{state.filteredSessions()[0].Title, state.filteredSessions()[1].Title, state.filteredSessions()[2].Title}; !reflect.DeepEqual(got, []string{"beta", "alpha", "zeta"}) {
		t.Fatalf("sessions turns sort = %#v", got)
	}
}

func TestSortKeyReordersDetailSessions(t *testing.T) {
	source := usage.NewCodexSourceRef("history.jsonl", 1, "")
	model := usage.NewModelRef("codex", "gpt-example")
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := old.Add(24 * time.Hour)

	oldTurn := usage.NewTurn("old", "old-turn", 1, source)
	oldTurn.StartedAt = old
	oldTurn.EndedAt = old.Add(time.Minute)
	oldTurn.ObserveModelAt(model, old, source)
	secondOldTurn := usage.NewTurn("old", "second-old-turn", 2, source)
	secondOldTurn.StartedAt = old.Add(time.Hour)
	secondOldTurn.EndedAt = secondOldTurn.StartedAt.Add(time.Minute)
	secondOldTurn.ObserveModelAt(model, secondOldTurn.StartedAt, source)
	newTurn := usage.NewTurn("new", "new-turn", 1, source)
	newTurn.StartedAt = newer
	newTurn.EndedAt = newer.Add(time.Minute)
	newTurn.ObserveModelAt(model, newer, source)
	for _, turn := range []*usage.Turn{&oldTurn, &secondOldTurn, &newTurn} {
		turn.SkillEvidence = []usage.SkillEvidence{usage.NewSkillEvidence(turn.SessionID, turn.ID, "review", usage.ModeExplicit, usage.MethodExplicitRequest, usage.StateConfirmed, turn.StartedAt, source)}
	}
	input := query.Input{
		Turns:    []usage.Turn{oldTurn, secondOldTurn, newTurn},
		Sessions: []usage.Session{usage.NewSession("old", source), usage.NewSession("new", source)},
	}

	state := NewState(input, query.Filter{}, nil)
	state.Route, state.ParentRoute, state.selectedKey = RouteModelDetail, RouteModels, model.Key()
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	view := state.View()
	if strings.Index(view, "codex/old") > strings.Index(view, "codex/new") || !strings.Contains(view, "Sessions [Turns]") {
		t.Fatalf("model detail sort = %s", view)
	}

	state = NewState(input, query.Filter{}, nil)
	state.Route, state.ParentRoute, state.selectedKey = RouteSkillDetail, RouteSkills, "review"
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	view = state.View()
	if strings.Index(view, "codex/old") > strings.Index(view, "codex/new") || !strings.Contains(view, "Sessions [First Used]") {
		t.Fatalf("skill detail sort = %s", view)
	}
}

func TestHelpAdvertisesListSorting(t *testing.T) {
	state := NewState(explorerInput(), query.Filter{}, nil)
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if !strings.Contains(state.View(), "s         cycle list sort") {
		t.Fatalf("help omitted sort action: %s", state.View())
	}
}

func TestQuitKeysHaveConsistentTUISemantics(t *testing.T) {
	quit := func(t *testing.T, state *State, key tea.KeyMsg) {
		t.Helper()
		_, cmd := state.Update(key)
		if cmd == nil {
			t.Fatal("quit key returned no command")
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("quit key returned %T, want tea.QuitMsg", cmd())
		}
	}

	quit(t, NewState(explorerInput(), query.Filter{}, nil), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	state := NewState(explorerInput(), query.Filter{}, nil)
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	quit(t, state, tea.KeyMsg{Type: tea.KeyCtrlC})

	state = NewState(explorerInput(), query.Filter{}, nil)
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	quit(t, state, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	state = NewState(explorerInput(), query.Filter{}, nil)
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if !state.searching || state.searchInput != "q" {
		t.Fatalf("search input q = searching:%v input:%q", state.searching, state.searchInput)
	}
	quit(t, state, tea.KeyMsg{Type: tea.KeyCtrlC})
}

func TestFooterHidesUnavailableFilterAction(t *testing.T) {
	state := NewState(explorerInput(), query.Filter{}, nil)
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	if strings.Contains(state.View(), "f filter") {
		t.Fatalf("sessions footer advertises unavailable filter action: %s", state.View())
	}

	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	if !strings.Contains(state.View(), "f filter") {
		t.Fatalf("models footer omitted available filter action: %s", state.View())
	}
}

func TestListHeadingsUsePositionWithoutParenthesizedCount(t *testing.T) {
	state := NewState(explorerInput(), query.Filter{}, nil)
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	view := state.View()
	if strings.Contains(view, "Skills (") || !strings.Contains(view, "Skills") || !strings.Contains(view, "1/1") {
		t.Fatalf("skills heading should show name and position only: %s", view)
	}
}

func TestSessionsListPrefersSourceSessionTitle(t *testing.T) {
	input := explorerInput()
	input.Sessions[0].Title = "Implement usage explorer"
	input.Turns[0].Aborted = true
	state := NewState(input, query.Filter{}, nil)
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	view := state.View()
	if !strings.Contains(view, "Implement usage explorer") || !strings.Contains(view, "SESSION") || !strings.Contains(view, "PROJECT") || !strings.Contains(view, "LAST") || strings.Contains(view, "SESSION NAME") {
		t.Fatalf("sessions list omitted compact metadata: %s", view)
	}
	for _, unwanted := range []string{"codex/s", "TURNS", "TOKENS", "! Implement usage explorer"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("sessions list contains %q: %s", unwanted, view)
		}
	}
}

func TestSessionsSearchMatchesFullSessionID(t *testing.T) {
	input := explorerInput()
	input.Sessions[0].ID = "01a094e0-0e59-7493-a4b0-3681af2c63e3"
	input.Sessions[0].Key = ""
	input.Turns[0].SessionID = input.Sessions[0].ID
	state := NewState(input, query.Filter{}, nil)
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("01a094e0-0e59-7493-a4b0-3681af2c63e3")})
	state.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(state.View(), "(01a094e0)") {
		t.Fatalf("session ID search omitted matching session: %s", state.View())
	}
}

func TestSessionsListFitsNarrowTerminal(t *testing.T) {
	state := NewState(explorerInput(), query.Filter{}, nil)
	state.Update(tea.WindowSizeMsg{Width: 32, Height: 10})
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	view := state.View()
	if !strings.Contains(view, "SESSION") || !strings.Contains(view, "LAST USED") || strings.Contains(view, "SESSION NAME") {
		t.Fatalf("narrow sessions list omitted compact columns: %s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > state.Width {
			t.Fatalf("sessions list line exceeds width: %q", line)
		}
	}
}

func TestSessionDetailShowsFullIDAndAbortedStatus(t *testing.T) {
	input := explorerInput()
	input.Sessions[0].Title = "Implement usage explorer"
	input.Turns[0].Aborted = true
	state := NewState(input, query.Filter{}, nil)
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	state.Update(tea.KeyMsg{Type: tea.KeyEnter})
	view := state.View()
	for _, want := range []string{"Session name: Implement usage explorer", "Session ID: s", "Status: aborted"} {
		if !strings.Contains(view, want) {
			t.Fatalf("session detail omitted %q: %s", want, view)
		}
	}
}

func TestFormatRelativeTime(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		age  time.Duration
		want string
	}{
		{name: "zero", want: "—"},
		{name: "now", age: 30 * time.Second, want: "now"},
		{name: "minutes", age: 12 * time.Minute, want: "12m ago"},
		{name: "hours", age: 3 * time.Hour, want: "3h ago"},
		{name: "days", age: 14 * 24 * time.Hour, want: "14d ago"},
		{name: "months", age: 90 * 24 * time.Hour, want: "3mo ago"},
		{name: "years", age: 365 * 24 * time.Hour, want: "1y ago"},
		{name: "future", age: -time.Minute, want: "now"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := time.Time{}
			if tt.name != "zero" {
				value = now.Add(-tt.age)
			}
			if got := formatRelativeTimeAt(value, now); got != tt.want {
				t.Fatalf("formatRelativeTimeAt(%v) = %q, want %q", value, got, tt.want)
			}
		})
	}
}

func TestListLastUsedUsesRelativeTime(t *testing.T) {
	lastUsed := time.Now().UTC().Add(-14 * 24 * time.Hour)

	modelCells := modelCells(120, &query.ModelSummary{LastUsed: lastUsed})
	if got := modelCells[len(modelCells)-1].value; got != "14d ago" {
		t.Fatalf("model LAST USED = %q, want relative time", got)
	}

	skillListCells := skillCells(120, &query.SkillSummary{LastUsed: lastUsed})
	if got := skillListCells[len(skillListCells)-2].value; got != "14d ago" {
		t.Fatalf("skill LAST USED = %q, want relative time", got)
	}
	if got := skillCells(120, nil)[len(skillCells(120, nil))-2].value; got != "LAST USED" {
		t.Fatalf("skill LAST USED header = %q", got)
	}
	for _, cell := range skillCells(120, nil) {
		if cell.value == "CONFIRMED" || cell.value == "INFERRED" {
			t.Fatalf("skill list contains removed evidence column %q", cell.value)
		}
	}

	sessionCells := sessionCells(120, &query.SessionSummary{EndedAt: lastUsed})
	if got := sessionCells[len(sessionCells)-1].value; got != "14d ago" {
		t.Fatalf("session LAST = %q, want relative time", got)
	}

	skillSessionRowCells := skillSessionCells(120, &query.SkillSessionUsage{FirstUsed: lastUsed.Add(-time.Hour), LastUsed: lastUsed})
	if got := skillSessionRowCells[len(skillSessionRowCells)-1].value; got != "14d ago" {
		t.Fatalf("skill session LAST USED = %q, want relative time in final column", got)
	}
	if got := skillSessionCells(120, nil)[len(skillSessionCells(120, nil))-1].value; got != "LAST USED" {
		t.Fatalf("skill session LAST USED header = %q, want final column", got)
	}
	for _, cell := range skillSessionCells(120, nil) {
		if cell.value == "FIRST USED" {
			t.Fatalf("skill detail contains removed column %q", cell.value)
		}
	}
}

func TestViewsExposeUsageMetadataWithoutInternalSessionSeparator(t *testing.T) {
	state := NewState(explorerInput(), query.Filter{}, nil)
	state.Width = 240
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	if view := state.View(); !strings.Contains(view, "explicit-request 1") || strings.Contains(view, "explicit-request=1") {
		t.Fatalf("skills view omitted method counts: %s", view)
	}
	state.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	state.Update(tea.KeyMsg{Type: tea.KeyEnter})
	view := state.View()
	if strings.Contains(view, "Provider session:") || strings.Contains(view, "ctx session:") || !strings.Contains(view, "Created:") || strings.Contains(view, "\x00") {
		t.Fatalf("session detail metadata or safe key display is wrong: %s", view)
	}
}

func TestSessionIdentityMetadataIsCtxOnly(t *testing.T) {
	summary := query.SessionSummary{ProviderSessionID: "provider", CtxSessionID: "ctx"}
	for _, source := range []usage.SourceKind{usage.SourceCodex, usage.SourceOpenCode} {
		summary.Source = source
		if showSessionIdentityMetadata(summary) {
			t.Fatalf("session identity metadata should be hidden for %q", source)
		}
	}
	summary.Source = usage.SourceCtx
	if !showSessionIdentityMetadata(summary) {
		t.Fatal("session identity metadata should be shown for ctx")
	}
}

func TestRunRejectsNonInteractiveStreams(t *testing.T) {
	err := Run(RunOptions{Input: explorerInput(), Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}})
	if !errors.Is(err, ErrNotInteractive) {
		t.Fatalf("Run error = %v, want ErrNotInteractive", err)
	}
}
