package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/xkumiyu/catsift/internal/aggregate"
	"github.com/xkumiyu/catsift/internal/skillinventory"
	"github.com/xkumiyu/catsift/internal/usage"
	"golang.org/x/term"
)

type ColorMode string

const (
	ColorAuto   ColorMode = "auto"
	ColorAlways ColorMode = "always"
	ColorNever  ColorMode = "never"
)

func (m ColorMode) Valid() bool { return m == ColorAuto || m == ColorAlways || m == ColorNever }

type TerminalCapabilities struct {
	IsTTY        bool
	Width        int
	ColorProfile string
	ColorMode    ColorMode
	NoColor      bool
}

// SkillView selects the report rendered by the skills command.
type SkillView string

const (
	SkillViewUsage  SkillView = "usage"
	SkillViewUnused SkillView = "unused"
)

// SkillUsageView selects the human-readable Skill usage table layout.
type SkillUsageView string

const (
	SkillUsageViewAuto    SkillUsageView = "auto"
	SkillUsageViewCompact SkillUsageView = "compact"
	SkillUsageViewMode    SkillUsageView = "mode"
	SkillUsageViewState   SkillUsageView = "state"
	SkillUsageViewAll     SkillUsageView = "all"
)

func (v SkillUsageView) Valid() bool {
	return v == SkillUsageViewAuto || v == SkillUsageViewCompact || v == SkillUsageViewMode || v == SkillUsageViewState || v == SkillUsageViewAll
}

var ansiSequenceRE = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

// DetectCapabilities gets terminal information without writing to the output.
// Tests can construct TerminalCapabilities directly for deterministic output.
func DetectCapabilities(file *os.File, mode ColorMode, noColor bool) TerminalCapabilities {
	capabilities := TerminalCapabilities{ColorMode: mode, NoColor: noColor, Width: 80}
	if file == nil {
		return capabilities
	}
	fd := int(file.Fd())
	capabilities.IsTTY = term.IsTerminal(fd)
	if width, _, err := term.GetSize(fd); err == nil && width > 0 {
		capabilities.Width = width
	}
	if capabilities.ColorMode == "" {
		capabilities.ColorMode = ColorAuto
	}
	return capabilities
}

func (c TerminalCapabilities) ColorsEnabled() bool {
	switch c.ColorMode {
	case ColorAlways:
		return true
	case ColorNever:
		return false
	default:
		return c.IsTTY && !c.NoColor
	}
}

type ReportContext struct {
	Source         usage.SourceKind
	Sources        []usage.SourceKind
	SourcePath     string
	SourcePaths    map[usage.SourceKind]string
	Agents         []string
	Agent          string
	Period         string
	PeriodInfo     string
	Layer          usage.ToolLayer
	SkillGroupBy   aggregate.SkillGroupBy
	SkillView      SkillView
	SkillUsageView SkillUsageView
	SkillRoots     []string
	Strict         bool
	ReferenceTime  time.Time
	Location       *time.Location
}

// FormatSourceContext returns the source display name and its configured path
// using the same formatting as human-readable reports.
func FormatSourceContext(source usage.SourceKind, sourcePath string) string {
	return (ReportContext{Source: source, SourcePath: sourcePath}).sourceContext()
}

func (c ReportContext) sourceKinds() []usage.SourceKind {
	if len(c.Sources) > 0 {
		result := orderedSourceKinds(c.Sources)
		if len(result) > 0 {
			return result
		}
	}
	if c.Source.Valid() {
		return []usage.SourceKind{c.Source}
	}
	return []usage.SourceKind{usage.SourceCodex}
}

func (c ReportContext) sourceKind() usage.SourceKind {
	sources := c.sourceKinds()
	if len(sources) == 1 {
		return sources[0]
	}
	return ""
}

func (c ReportContext) sourceContext() string {
	sources := c.sourceKinds()
	values := make([]string, 0, len(sources))
	for _, source := range sources {
		path := c.SourcePath
		if len(sources) > 1 {
			path = ""
		}
		if configured, ok := c.SourcePaths[source]; ok {
			path = configured
		}
		values = append(values, formatSourceContext(source, path))
	}
	return strings.Join(values, ", ")
}

func formatSourceContext(source usage.SourceKind, sourcePath string) string {
	label := string(source)
	switch source {
	case usage.SourceCodex:
		label = "Codex"
	case usage.SourceOpenCode:
		label = "OpenCode"
	}
	path := strings.TrimSpace(sourcePath)
	if path == "" && source == usage.SourceCodex {
		path = "~/.codex"
	}
	if path == "" {
		return label
	}
	return label + " (" + displaySourcePath(path) + ")"
}

func orderedSourceKinds(values []usage.SourceKind) []usage.SourceKind {
	seen := make(map[usage.SourceKind]struct{}, len(values))
	for _, value := range values {
		if value.Valid() {
			seen[value] = struct{}{}
		}
	}
	result := make([]usage.SourceKind, 0, len(seen))
	for _, value := range usage.AllSourceKinds() {
		if _, ok := seen[value]; ok {
			result = append(result, value)
		}
	}
	return result
}

func sourceListContains(values []usage.SourceKind, wanted usage.SourceKind) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func displaySourcePath(path string) string {
	path = safeDisplay(path)
	if path == "" || strings.HasPrefix(path, "~") || !filepath.IsAbs(path) {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return path
	}
	relative, err := filepath.Rel(home, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return path
	}
	if relative == "." {
		return "~"
	}
	return "~" + string(filepath.Separator) + relative
}

func (c ReportContext) agentIDs() []string {
	values := c.Agents
	if len(values) == 0 && strings.TrimSpace(c.Agent) != "" {
		values = strings.Split(c.Agent, ",")
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		id := usage.CanonicalAgentID(value)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	if len(result) == 0 && sourceListContains(c.sourceKinds(), usage.SourceCodex) {
		return []string{"codex"}
	}
	sort.Strings(result)
	return result
}

func (c ReportContext) agent() string {
	agents := c.agentIDs()
	names := make([]string, 0, len(agents))
	for _, agent := range agents {
		names = append(names, usage.AgentDisplayName(agent))
	}
	if len(names) == 0 {
		return "-"
	}
	return strings.Join(names, ", ")
}

func (c ReportContext) agentID() string {
	agents := c.agentIDs()
	if len(agents) == 0 {
		return ""
	}
	return strings.Join(agents, ",")
}

func (c ReportContext) period() string {
	if strings.TrimSpace(c.Period) == "" {
		return "all time"
	}
	return c.Period
}

func (c ReportContext) jsonSources() []usage.SourceKind {
	if len(c.Sources) <= 1 {
		return nil
	}
	return c.sourceKinds()
}

// RenderHuman renders a static, non-interactive report. kind is one of stats,
// tools, or skills.
func RenderHuman(kind string, ctx ReportContext, report aggregate.Report, capabilities TerminalCapabilities) string {
	width := capabilities.Width
	if width <= 0 {
		width = 80
	}
	if ctx.SkillView == SkillViewUnused && len(ctx.SkillRoots) == 0 {
		ctx.SkillRoots = append([]string{}, report.UnusedRoots...)
	}
	effectiveSkillUsageView := SkillUsageView("")
	if kind == "skills" && ctx.SkillView != SkillViewUnused {
		effectiveSkillUsageView = selectSkillUsageView(ctx.SkillUsageView, report.Skills, width)
	}
	styled := capabilities.ColorsEnabled()
	heading := reportHeading(kind, ctx)
	if heading == "" {
		return ""
	}
	lines := []string{styleHeading(heading, styled)}
	lines = append(lines, contextLines(kind, ctx, effectiveSkillUsageView, styled)...)
	infoMessages := reportInfoMessages(kind, ctx, report)
	switch kind {
	case "stats":
		lines = append(lines, "", renderStats(report.Overview, styled))
	case "tools":
		lines = append(lines, "")
		if body := renderTools(report.Tools, ctx, width, styled); body != "" {
			lines = append(lines, body, "")
		}
		lines = append(lines, styleFooter(toolFooter(report.Tools), styled))
	case "skills":
		if ctx.SkillView == SkillViewUnused {
			lines = append(lines, "")
			if body := renderUnusedSkills(report.UnusedSkills, report.InstalledSkills, width, styled); body != "" {
				lines = append(lines, body, "")
			}
			lines = append(lines, styleFooter(unusedSkillFooter(report.UnusedSkills, report.InstalledSkills), styled))
		} else {
			lines = append(lines, "")
			if body := renderSkills(report.Skills, ctx, effectiveSkillUsageView, width, styled); body != "" {
				lines = append(lines, body, "")
			}
			lines = append(lines, styleFooter(skillFooter(report.Skills), styled))
		}
	default:
		return ""
	}
	if len(infoMessages) > 0 {
		lines = append(lines, "")
		for _, message := range infoMessages {
			lines = append(lines, styleInfo("info: "+message, styled))
		}
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
}

func reportHeading(kind string, ctx ReportContext) string {
	switch kind {
	case "stats":
		return "USAGE OVERVIEW"
	case "tools":
		return "TOOL USAGE"
	case "skills":
		if ctx.SkillView == SkillViewUnused {
			return "UNUSED SKILLS"
		}
		return "SKILL USAGE"
	default:
		return ""
	}
}

func contextLines(kind string, ctx ReportContext, effectiveSkillUsageView SkillUsageView, styled bool) []string {
	parts := []string{"Source: " + ctx.sourceContext(), "Agents: " + ctx.agent(), "Period: " + ctx.period()}
	switch kind {
	case "tools":
		layer := ctx.Layer
		if layer == "" {
			layer = usage.LayerEffective
		}
		parts = append(parts, "Layer: "+string(layer))
	case "skills":
		if ctx.SkillView == SkillViewUnused {
			parts = append(parts, "Group by: "+string(contextSkillGroupBy(ctx)))
			parts = append(parts, "Strict: "+strconv.FormatBool(ctx.Strict))
			parts = append(parts, "View: unused")
			if len(ctx.SkillRoots) > 0 {
				roots := make([]string, 0, len(ctx.SkillRoots))
				for _, root := range ctx.SkillRoots {
					roots = append(roots, safeDisplay(root))
				}
				parts = append(parts, "Roots: "+strings.Join(roots, ", "))
			}
			break
		}
		parts = append(parts, "Group by: "+string(contextSkillGroupBy(ctx)))
		parts = append(parts, "Strict: "+strconv.FormatBool(ctx.Strict))
		parts = append(parts, "View: "+skillUsageViewLabel(ctx.SkillUsageView, effectiveSkillUsageView))
	}
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		lines = append(lines, styleContext(part, styled))
	}
	return lines
}

func reportInfoMessages(kind string, ctx ReportContext, report aggregate.Report) []string {
	messages := make([]string, 0, 2)
	if periodInfo := strings.TrimSpace(ctx.PeriodInfo); periodInfo != "" {
		messages = append(messages, periodInfo)
	}
	switch kind {
	case "stats":
		if overviewIsEmpty(report.Overview) {
			messages = append(messages, "No usage found for the selected period.")
		}
	case "tools":
		if len(report.Tools) == 0 {
			messages = append(messages, "No tool usage found for the selected period and layer.")
		}
	case "skills":
		if ctx.SkillView != SkillViewUnused && len(report.Skills) == 0 {
			messages = append(messages, "No skill usage found for the selected period and filter.")
		}
	}
	return messages
}

func contextSkillGroupBy(ctx ReportContext) aggregate.SkillGroupBy {
	if ctx.SkillGroupBy.Valid() {
		return ctx.SkillGroupBy
	}
	return aggregate.SkillGroupByTurn
}

func skillUsageViewLabel(requested, effective SkillUsageView) string {
	if requested == "" {
		requested = SkillUsageViewAuto
	}
	if requested == SkillUsageViewAuto {
		return string(requested) + " (selected: " + string(effective) + ")"
	}
	return string(effective)
}

type statMetric struct {
	label  string
	value  string
	indent int
}

type statSection struct {
	title   string
	metrics []statMetric
}

func renderStats(summary aggregate.Overview, styled bool) string {
	sections := []statSection{
		{title: "Activity", metrics: []statMetric{
			{label: "Sessions", value: formatCount(summary.Sessions)},
			{label: "Turns", value: formatCount(summary.Turns)},
			{label: "User Prompts", value: formatCount(summary.UserPrompts)},
			{label: "Tool Calls", value: formatCount(summary.ToolCalls)},
		}},
		{title: "Skill Usage", metrics: []statMetric{
			{label: "By turn", value: formatCount(summary.SkillUsesTurn)},
			{label: "By session", value: formatCount(summary.SkillUsesSession)},
		}},
		{title: "Token Usage", metrics: tokenMetrics(summary)},
	}
	lines := renderStatSections(sections, styled)
	return strings.Join(lines, "\n")
}

func tokenMetrics(summary aggregate.Overview) []statMetric {
	if !summary.TokenUsageAvailable && summary.TokenUsage == (usage.TokenUsage{}) {
		return []statMetric{{label: "Status", value: "not available"}}
	}
	metrics := []statMetric{
		{label: "Total Tokens", value: formatCompactCount64(summary.TokenUsage.TotalTokens)},
		{label: "Input Tokens", value: formatCompactCount64(summary.TokenUsage.InputTokens), indent: 1},
		{label: "Cached Tokens", value: formatCompactCount64(summary.TokenUsage.CachedInputTokens), indent: 2},
	}
	if summary.TokenUsage.CacheWriteInputTokens != 0 {
		metrics = append(metrics, statMetric{label: "Cache Write Input Tokens", value: formatCompactCount64(summary.TokenUsage.CacheWriteInputTokens), indent: 2})
	}
	metrics = append(metrics,
		statMetric{label: "Output Tokens", value: formatCompactCount64(summary.TokenUsage.OutputTokens), indent: 1},
		statMetric{label: "Reasoning Tokens", value: formatCompactCount64(summary.TokenUsage.ReasoningOutputTokens), indent: 2},
	)
	return metrics
}

func renderStatSections(sections []statSection, styled bool) []string {
	lines := make([]string, 0, len(sections)*2)
	for sectionIndex, section := range sections {
		if sectionIndex > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, styleHeader(section.title, styled))
		labelWidth, valueWidth := 0, 0
		for _, metric := range section.metrics {
			label := statMetricLabel(metric)
			if width := lipgloss.Width(label); width > labelWidth {
				labelWidth = width
			}
			if width := lipgloss.Width(metric.value); width > valueWidth {
				valueWidth = width
			}
		}
		for _, metric := range section.metrics {
			value := metric.value
			if styled {
				value = lipgloss.NewStyle().Bold(true).Render(value)
			}
			lines = append(lines, "  "+padRight(styleLabel(statMetricLabel(metric), styled), labelWidth)+" "+padLeft(value, valueWidth))
		}
	}
	return lines
}

func statMetricLabel(metric statMetric) string {
	return strings.Repeat("  ", metric.indent) + metric.label
}

func overviewIsEmpty(summary aggregate.Overview) bool {
	return summary.Sessions == 0 && summary.Turns == 0 && summary.UserPrompts == 0 && summary.ToolCalls == 0 && summary.SkillUsesTurn == 0 && summary.SkillUsesSession == 0 && summary.TokenUsage == (usage.TokenUsage{})
}

func renderTools(rows []aggregate.ToolRow, ctx ReportContext, width int, styled bool) string {
	if len(rows) == 0 {
		return ""
	}
	compact := width < 70
	standard := width < 100
	if compact {
		nameWidth := maxNameWidth(width-9, rows, func(row aggregate.ToolRow) string { return safeDisplay(row.Name) })
		header := padRight(styleHeader("Tool", styled), nameWidth) + "  " + padLeft(styleHeader("Calls", styled), 7)
		lines := []string{header, tableRule(lipgloss.Width(header), styled)}
		for _, row := range rows {
			name := styleIdentity(truncate(safeDisplay(row.Name), nameWidth), styled)
			lines = append(lines, padRight(name, nameWidth)+"  "+padLeft(formatCount(row.Calls), 7))
		}
		return strings.Join(lines, "\n")
	}
	nameWidth := maxNameWidth(width-44, rows, func(row aggregate.ToolRow) string { return safeDisplay(row.Name) })
	if standard {
		header := padRight(styleHeader("Tool", styled), nameWidth) + "  " + padLeft(styleHeader("Calls", styled), 8) + "  " + padLeft(styleHeader("Failures", styled), 8) + "  " + padLeft(styleHeader("Last Used", styled), 22)
		lines := []string{header, tableRule(lipgloss.Width(header), styled)}
		for _, row := range rows {
			failure := formatCount(row.Failures)
			lastUsed := styleSecondary(formatLocalTime(row.LastUsed, ctx.Location), styled)
			name := styleIdentity(truncate(safeDisplay(row.Name), nameWidth), styled)
			lines = append(lines, padRight(name, nameWidth)+"  "+padLeft(formatCount(row.Calls), 8)+"  "+padLeft(failure, 8)+"  "+padLeft(lastUsed, 22))
		}
		return strings.Join(lines, "\n")
	}
	header := padRight(styleHeader("Tool", styled), nameWidth) + "  " + padLeft(styleHeader("Calls", styled), 8) + "  " + padLeft(styleHeader("Failures", styled), 8) + "  " + padLeft(styleHeader("Last Used", styled), 22)
	lines := []string{header, tableRule(lipgloss.Width(header), styled)}
	for _, row := range rows {
		failure := formatCount(row.Failures)
		lastUsed := styleSecondary(formatLocalTime(row.LastUsed, ctx.Location), styled)
		name := styleIdentity(truncate(safeDisplay(row.Name), nameWidth), styled)
		lines = append(lines, padRight(name, nameWidth)+"  "+padLeft(formatCount(row.Calls), 8)+"  "+padLeft(failure, 8)+"  "+padLeft(lastUsed, 22))
	}
	return strings.Join(lines, "\n")
}

func selectSkillUsageView(view SkillUsageView, rows []aggregate.SkillRow, width int) SkillUsageView {
	if view.Valid() && view != SkillUsageViewAuto {
		return view
	}
	if width < 70 {
		return SkillUsageViewCompact
	}
	if width < 100 || width-97 < maxSkillNameContentWidth(rows) {
		return SkillUsageViewMode
	}
	return SkillUsageViewAll
}

func renderSkills(rows []aggregate.SkillRow, ctx ReportContext, view SkillUsageView, width int, styled bool) string {
	if len(rows) == 0 {
		return ""
	}
	switch view {
	case SkillUsageViewCompact:
		return renderSkillCompact(rows, width, styled)
	case SkillUsageViewMode:
		return renderSkillMode(rows, width, styled)
	case SkillUsageViewState:
		return renderSkillState(rows, ctx, width, styled)
	case SkillUsageViewAll:
		if ctx.SkillUsageView == SkillUsageViewAll {
			return renderSkillAllSections(rows, ctx, width, styled)
		}
		return renderSkillAllTable(rows, ctx, width, styled)
	default:
		return renderSkillMode(rows, width, styled)
	}
}

func renderSkillCompact(rows []aggregate.SkillRow, width int, styled bool) string {
	nameWidth := maxSkillNameWidth(width-9, rows)
	header := padRight(styleHeader("Skill", styled), nameWidth) + "  " + padLeft(styleHeader("Total", styled), 7)
	lines := []string{header, tableRule(lipgloss.Width(header), styled)}
	for _, row := range rows {
		name := styleIdentity(truncate(safeDisplay(row.Name), nameWidth), styled)
		lines = append(lines, padRight(name, nameWidth)+"  "+padLeft(formatCount(row.Total), 7))
	}
	return strings.Join(lines, "\n")
}

func renderSkillMode(rows []aggregate.SkillRow, width int, styled bool) string {
	if width < 47 {
		return renderSkillModeDetails(rows, width, styled)
	}
	nameWidth := maxSkillNameWidth(width-39, rows)
	header := padRight(styleHeader("Skill", styled), nameWidth) + "  " + padLeft(styleHeader("Explicit", styled), 8) + "  " + padLeft(styleHeader("Implicit", styled), 8) + "  " + padLeft(styleHeader("Unknown", styled), 8) + "  " + padLeft(styleHeader("Total", styled), 7)
	lines := []string{header, tableRule(lipgloss.Width(header), styled)}
	for _, row := range rows {
		name := styleIdentity(truncate(safeDisplay(row.Name), nameWidth), styled)
		lines = append(lines, padRight(name, nameWidth)+"  "+padLeft(formatCount(row.Explicit), 8)+"  "+padLeft(formatCount(row.Implicit), 8)+"  "+padLeft(formatCount(row.Unknown), 8)+"  "+padLeft(formatCount(row.Total), 7))
	}
	return strings.Join(lines, "\n")
}

func renderSkillState(rows []aggregate.SkillRow, ctx ReportContext, width int, styled bool) string {
	return renderSkillStateTable(rows, ctx, width, styled, false)
}

func renderSkillStateWithLastUsed(rows []aggregate.SkillRow, ctx ReportContext, width int, styled bool) string {
	return renderSkillStateTable(rows, ctx, width, styled, true)
}

func renderSkillStateTable(rows []aggregate.SkillRow, ctx ReportContext, width int, styled, includeLastUsed bool) string {
	if !includeLastUsed {
		if width < 51 {
			return renderSkillStateDetails(rows, ctx, width, styled, false)
		}
		nameWidth := maxSkillNameWidth(width-43, rows)
		header := padRight(styleHeader("Skill", styled), nameWidth) + "  " + padLeft(styleHeader("Confirmed", styled), 9) + "  " + padLeft(styleHeader("Inferred", styled), 8) + "  " + padLeft(styleHeader("Unconfirmed", styled), 11) + "  " + padLeft(styleHeader("Total", styled), 7)
		lines := []string{header, tableRule(lipgloss.Width(header), styled)}
		for _, row := range rows {
			name := styleIdentity(truncate(safeDisplay(row.Name), nameWidth), styled)
			lines = append(lines, padRight(name, nameWidth)+"  "+padLeft(formatCount(row.Confirmed), 9)+"  "+padLeft(formatCount(row.Inferred), 8)+"  "+padLeft(formatCount(row.Unconfirmed), 11)+"  "+padLeft(formatCount(row.Total), 7))
		}
		return strings.Join(lines, "\n")
	}
	if width < 75 {
		return renderSkillStateDetails(rows, ctx, width, styled, true)
	}
	nameWidth := maxSkillNameWidth(width-67, rows)
	header := padRight(styleHeader("Skill", styled), nameWidth) + "  " + padLeft(styleHeader("Confirmed", styled), 9) + "  " + padLeft(styleHeader("Inferred", styled), 8) + "  " + padLeft(styleHeader("Unconfirmed", styled), 11) + "  " + padLeft(styleHeader("Total", styled), 7) + "  " + padLeft(styleHeader("Last Used", styled), 22)
	lines := []string{header, tableRule(lipgloss.Width(header), styled)}
	for _, row := range rows {
		lastUsed := styleSecondary(formatLocalTime(row.LastUsed, ctx.Location), styled)
		name := styleIdentity(truncate(safeDisplay(row.Name), nameWidth), styled)
		lines = append(lines, padRight(name, nameWidth)+"  "+padLeft(formatCount(row.Confirmed), 9)+"  "+padLeft(formatCount(row.Inferred), 8)+"  "+padLeft(formatCount(row.Unconfirmed), 11)+"  "+padLeft(formatCount(row.Total), 7)+"  "+padLeft(lastUsed, 22))
	}
	return strings.Join(lines, "\n")
}

func renderSkillAllSections(rows []aggregate.SkillRow, ctx ReportContext, width int, styled bool) string {
	return strings.Join([]string{
		styleHeading("ACTIVATION MODE", styled),
		renderSkillMode(rows, width, styled),
		"",
		styleHeading("EVIDENCE STATE", styled),
		renderSkillStateWithLastUsed(rows, ctx, width, styled),
	}, "\n")
}

func renderSkillAllTable(rows []aggregate.SkillRow, ctx ReportContext, width int, styled bool) string {
	nameWidth := maxSkillNameWidth(width-97, rows)
	header := padRight(styleHeader("Skill", styled), nameWidth) + "  " + padLeft(styleHeader("Explicit", styled), 8) + "  " + padLeft(styleHeader("Implicit", styled), 8) + "  " + padLeft(styleHeader("Unknown", styled), 8) + "  " + padLeft(styleHeader("Confirmed", styled), 9) + "  " + padLeft(styleHeader("Inferred", styled), 8) + "  " + padLeft(styleHeader("Unconfirmed", styled), 11) + "  " + padLeft(styleHeader("Total", styled), 7) + "  " + padLeft(styleHeader("Last Used", styled), 22)
	lines := []string{header, tableRule(lipgloss.Width(header), styled)}
	for _, row := range rows {
		lastUsed := styleSecondary(formatLocalTime(row.LastUsed, ctx.Location), styled)
		name := styleIdentity(truncate(safeDisplay(row.Name), nameWidth), styled)
		lines = append(lines, padRight(name, nameWidth)+"  "+padLeft(formatCount(row.Explicit), 8)+"  "+padLeft(formatCount(row.Implicit), 8)+"  "+padLeft(formatCount(row.Unknown), 8)+"  "+padLeft(formatCount(row.Confirmed), 9)+"  "+padLeft(formatCount(row.Inferred), 8)+"  "+padLeft(formatCount(row.Unconfirmed), 11)+"  "+padLeft(formatCount(row.Total), 7)+"  "+padLeft(lastUsed, 22))
	}
	return strings.Join(lines, "\n")
}

func renderSkillModeDetails(rows []aggregate.SkillRow, width int, styled bool) string {
	lines := make([]string, 0, len(rows)*5)
	for i, row := range rows {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, skillDetailName(row.Name, width, styled))
		lines = append(lines,
			skillDetailField("Explicit", formatCount(row.Explicit), styled),
			skillDetailField("Implicit", formatCount(row.Implicit), styled),
			skillDetailField("Unknown", formatCount(row.Unknown), styled),
			skillDetailField("Total", formatCount(row.Total), styled),
		)
	}
	return strings.Join(lines, "\n")
}

func renderSkillStateDetails(rows []aggregate.SkillRow, ctx ReportContext, width int, styled, includeLastUsed bool) string {
	lines := make([]string, 0, len(rows)*6)
	for i, row := range rows {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, skillDetailName(row.Name, width, styled))
		fields := []string{
			skillDetailField("Confirmed", formatCount(row.Confirmed), styled),
			skillDetailField("Inferred", formatCount(row.Inferred), styled),
			skillDetailField("Unconfirmed", formatCount(row.Unconfirmed), styled),
			skillDetailField("Total", formatCount(row.Total), styled),
		}
		if includeLastUsed {
			fields = append(fields, skillDetailField("Last Used", formatLocalTime(row.LastUsed, ctx.Location), styled))
		}
		lines = append(lines, fields...)
	}
	return strings.Join(lines, "\n")
}

func skillDetailName(name string, width int, styled bool) string {
	prefix := "Skill: "
	return prefix + styleIdentity(truncate(safeDisplay(name), maxInt(1, width-lipgloss.Width(prefix))), styled)
}

func skillDetailField(label, value string, styled bool) string {
	return "  " + padRight(styleLabel(label+":", styled), 13) + " " + value
}

func renderUnusedSkills(rows []skillinventory.InventoryEntry, installed int, width int, styled bool) string {
	if len(rows) == 0 {
		if installed == 0 {
			return styleNotice("No installed skills found for the selected scope.", styled)
		}
		return styleNotice("No unused skills found for the selected scope and history filter.", styled)
	}
	if width < 70 {
		nameWidth := minDisplayWidth(maxUnusedNameWidth(rows), maxInt(8, (width-10)/2))
		pathWidth := maxInt(8, width-nameWidth-2)
		header := padRight(styleHeader("Skill", styled), nameWidth) + "  " + padRight(styleHeader("Path", styled), pathWidth)
		lines := []string{header, tableRule(lipgloss.Width(header), styled)}
		for _, row := range rows {
			name := styleIdentity(truncate(unusedNameDisplay(row), nameWidth), styled)
			path := styleSecondary(truncate(safeDisplay(row.Path), pathWidth), styled)
			lines = append(lines, padRight(name, nameWidth)+"  "+padRight(path, pathWidth))
		}
		return strings.Join(appendUnusedMismatchNote(lines, rows, styled), "\n")
	}

	if width < 110 {
		nameWidth := minDisplayWidth(maxUnusedNameWidth(rows), 32)
		nameWidth, pathWidth := fitUnusedColumns(width, nameWidth, 0, 1)
		header := padRight(styleHeader("Skill", styled), nameWidth) + "  " + padRight(styleHeader("Path", styled), pathWidth)
		lines := []string{header, tableRule(lipgloss.Width(header), styled)}
		for _, row := range rows {
			name := styleIdentity(truncate(unusedNameDisplay(row), nameWidth), styled)
			path := styleSecondary(truncate(safeDisplay(row.Path), pathWidth), styled)
			lines = append(lines, padRight(name, nameWidth)+"  "+padRight(path, pathWidth))
		}
		return strings.Join(appendUnusedMismatchNote(lines, rows, styled), "\n")
	}

	nameWidth := minDisplayWidth(maxUnusedNameWidth(rows), 40)
	sourceWidth := len(string(skillinventory.NameSourceFrontmatter))
	nameWidth, pathWidth := fitUnusedColumns(width, nameWidth, sourceWidth, 2)
	header := padRight(styleHeader("Skill", styled), nameWidth) + "  " + padRight(styleHeader("Path", styled), pathWidth) + "  " + padRight(styleHeader("Source", styled), sourceWidth)
	lines := []string{header, tableRule(lipgloss.Width(header), styled)}
	for _, row := range rows {
		name := styleIdentity(truncate(unusedNameDisplay(row), nameWidth), styled)
		path := styleSecondary(truncate(safeDisplay(row.Path), pathWidth), styled)
		source := styleSecondary(string(row.NameSource), styled)
		lines = append(lines, padRight(name, nameWidth)+"  "+padRight(path, pathWidth)+"  "+padRight(source, sourceWidth))
	}
	return strings.Join(appendUnusedMismatchNote(lines, rows, styled), "\n")
}

func maxUnusedNameWidth(rows []skillinventory.InventoryEntry) int {
	max := 8
	for _, row := range rows {
		if width := lipgloss.Width(unusedNameDisplay(row)); width > max {
			max = width
		}
	}
	return max
}

func unusedNameDisplay(row skillinventory.InventoryEntry) string {
	name := safeDisplay(row.Name)
	if row.NameMismatch {
		name += "*"
	}
	return name
}

func fitUnusedColumns(width, nameWidth, sourceWidth, gaps int) (int, int) {
	pathWidth := width - nameWidth - sourceWidth - gaps*2
	for pathWidth < 8 && nameWidth > 8 {
		nameWidth--
		pathWidth++
	}
	if pathWidth < 8 {
		pathWidth = 8
	}
	return nameWidth, pathWidth
}

func appendUnusedMismatchNote(lines []string, rows []skillinventory.InventoryEntry, styled bool) []string {
	for _, row := range rows {
		if row.NameMismatch {
			return append(lines, styleNotice("* frontmatter name differs from directory name", styled))
		}
	}
	return lines
}

func minDisplayWidth(value, limit int) int {
	if value > limit {
		return limit
	}
	return value
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func maxNameWidth(available int, rows []aggregate.ToolRow, name func(aggregate.ToolRow) string) int {
	if available < 8 {
		available = 8
	}
	max := 8
	for _, row := range rows {
		if w := lipgloss.Width(name(row)); w > max {
			max = w
		}
	}
	if max > available {
		return available
	}
	return max
}

func maxSkillNameWidth(available int, rows []aggregate.SkillRow) int {
	if available < 8 {
		available = 8
	}
	max := maxSkillNameContentWidth(rows)
	if max > available {
		return available
	}
	return max
}

func maxSkillNameContentWidth(rows []aggregate.SkillRow) int {
	max := 8
	for _, row := range rows {
		if w := lipgloss.Width(safeDisplay(row.Name)); w > max {
			max = w
		}
	}
	return max
}

func safeDisplay(value string) string {
	value = ansiSequenceRE.ReplaceAllString(value, "")
	var builder strings.Builder
	for _, r := range value {
		switch r {
		case '\n', '\r', '\t':
			builder.WriteRune(' ')
		default:
			if r >= 0x20 && r != 0x7f {
				builder.WriteRune(r)
			}
		}
	}
	return builder.String()
}

func truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	var result []rune
	for _, r := range value {
		candidate := string(result) + string(r) + "…"
		if lipgloss.Width(candidate) > width {
			break
		}
		result = append(result, r)
	}
	return string(result) + "…"
}

func padRight(value string, width int) string {
	padding := width - lipgloss.Width(value)
	if padding <= 0 {
		return value
	}
	return value + strings.Repeat(" ", padding)
}

func padLeft(value string, width int) string {
	padding := width - lipgloss.Width(value)
	if padding <= 0 {
		return value
	}
	return strings.Repeat(" ", padding) + value
}

func formatCount(value int) string {
	return formatCount64(int64(value))
}

func formatCount64(value int64) string {
	if value < 0 {
		return "-" + formatCount64(-value)
	}
	s := strconv.FormatInt(value, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func formatCompactCount64(value int64) string {
	if value < 0 {
		return "-" + formatCompactCount64(-value)
	}
	units := []struct {
		value  int64
		suffix string
	}{
		{value: 1_000_000_000_000, suffix: "T"},
		{value: 1_000_000_000, suffix: "B"},
		{value: 1_000_000, suffix: "M"},
		{value: 1_000, suffix: "K"},
	}
	unitIndex := -1
	for i, unit := range units {
		if value >= unit.value {
			unitIndex = i
			break
		}
	}
	if unitIndex == -1 {
		return formatCount64(value)
	}
	scaled := float64(value) / float64(units[unitIndex].value)
	if scaled >= 999.5 && unitIndex > 0 {
		unitIndex--
		scaled = float64(value) / float64(units[unitIndex].value)
	}
	decimals := 0
	if scaled < 10 {
		decimals = 2
	} else if scaled < 100 {
		decimals = 1
	}
	formatted := strconv.FormatFloat(scaled, 'f', decimals, 64)
	if strings.Contains(formatted, ".") {
		formatted = strings.TrimRight(strings.TrimRight(formatted, "0"), ".")
	}
	return formatted + units[unitIndex].suffix
}

func formatLocalTime(value time.Time, location *time.Location) string {
	if value.IsZero() {
		return "-"
	}
	if location == nil {
		location = time.Local
	}
	return value.In(location).Format("2006-01-02 15:04 MST")
}

func formatMachineTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func styleContext(value string, enabled bool) string {
	return styleSecondary(value, enabled)
}

func styleInfo(value string, enabled bool) string {
	return styleSecondary(value, enabled)
}

func styleHeading(value string, enabled bool) string {
	if !enabled {
		return value
	}
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")).Render(value)
}

// DiagnosticPrefix styles a diagnostic label according to terminal capabilities.
func DiagnosticPrefix(level string, capabilities TerminalCapabilities) string {
	level = strings.TrimSuffix(strings.TrimSpace(level), ":")
	label := level + ":"
	if !capabilities.ColorsEnabled() {
		return label
	}
	color := ""
	switch strings.ToLower(level) {
	case "warning":
		color = "11"
	case "error":
		color = "9"
	case "info", "debug":
		return styleInfo(label, true)
	default:
		return label
	}
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(color)).Render(label)
}

// DiagnosticMessage de-emphasizes informational and debug diagnostics while
// leaving warnings and errors readable at the default terminal emphasis.
func DiagnosticMessage(level, message string, capabilities TerminalCapabilities) string {
	level = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(level), ":"))
	if level == "info" || level == "debug" {
		return styleInfo(message, capabilities.ColorsEnabled())
	}
	return message
}

func styleHeader(value string, enabled bool) string {
	if !enabled {
		return value
	}
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11")).Render(value)
}

func styleIdentity(value string, enabled bool) string {
	if !enabled {
		return value
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Render(value)
}

func tableRule(width int, enabled bool) string {
	return styleSecondary(strings.Repeat("─", width), enabled)
}

func styleLabel(value string, enabled bool) string {
	if !enabled {
		return value
	}
	return lipgloss.NewStyle().Bold(true).Render(value)
}

func styleFooter(value string, enabled bool) string {
	return styleSecondary(value, enabled)
}

func styleNotice(value string, enabled bool) string {
	if !enabled {
		return value
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(value)
}

func styleSecondary(value string, enabled bool) string {
	if !enabled {
		return value
	}
	return lipgloss.NewStyle().Faint(true).Render(value)
}

func formatQuantity(value int, singular, plural string) string {
	unit := plural
	if value == 1 {
		unit = singular
	}
	return formatCount(value) + " " + unit
}

func toolFooter(rows []aggregate.ToolRow) string {
	return formatQuantity(len(rows), "tool", "tools") + ", " + formatQuantity(totalToolCalls(rows), "call", "calls") + " total"
}

func skillFooter(rows []aggregate.SkillRow) string {
	return formatQuantity(len(rows), "skill", "skills") + ", " + formatQuantity(totalSkillUses(rows), "use", "uses") + " total"
}

func unusedSkillFooter(rows []skillinventory.InventoryEntry, installed int) string {
	return formatQuantity(len(rows), "unused skill", "unused skills") + ", " + formatQuantity(installed, "installed skill", "installed skills") + " total"
}

func totalToolCalls(rows []aggregate.ToolRow) int {
	result := 0
	for _, row := range rows {
		result += row.Calls
	}
	return result
}

func totalSkillUses(rows []aggregate.SkillRow) int {
	result := 0
	for _, row := range rows {
		result += row.Total
	}
	return result
}

// RenderJSON returns one stable JSON document per command.
func RenderJSON(kind string, ctx ReportContext, report aggregate.Report) ([]byte, error) {
	base := struct {
		SchemaVersion int                `json:"schema_version"`
		Agent         string             `json:"agent"`
		Source        string             `json:"source"`
		Sources       []usage.SourceKind `json:"sources,omitempty"`
		Agents        []string           `json:"agents"`
		Period        string             `json:"period"`
	}{1, ctx.agentID(), string(ctx.sourceKind()), ctx.jsonSources(), ctx.agentIDs(), ctx.period()}
	switch kind {
	case "stats":
		value := struct {
			SchemaVersion         int                `json:"schema_version"`
			Agent                 string             `json:"agent"`
			Source                string             `json:"source"`
			Sources               []usage.SourceKind `json:"sources,omitempty"`
			Agents                []string           `json:"agents"`
			Period                string             `json:"period"`
			GeneratedAt           string             `json:"generated_at"`
			Sessions              int                `json:"sessions"`
			Turns                 int                `json:"turns"`
			UserPrompts           int                `json:"user_prompts"`
			ToolCalls             int                `json:"tool_calls"`
			SkillUsesTurn         int                `json:"skill_uses_turn"`
			SkillUsesSession      int                `json:"skill_uses_session"`
			TokenUsageAvailable   bool               `json:"token_usage_available"`
			InputTokens           int64              `json:"input_tokens"`
			CachedInputTokens     int64              `json:"cached_input_tokens"`
			CacheWriteInputTokens int64              `json:"cache_write_input_tokens"`
			OutputTokens          int64              `json:"output_tokens"`
			ReasoningOutputTokens int64              `json:"reasoning_output_tokens"`
			TotalTokens           int64              `json:"total_tokens"`
		}{base.SchemaVersion, base.Agent, base.Source, base.Sources, base.Agents, base.Period, formatMachineTime(ctx.ReferenceTime), report.Overview.Sessions, report.Overview.Turns, report.Overview.UserPrompts, report.Overview.ToolCalls, report.Overview.SkillUsesTurn, report.Overview.SkillUsesSession, report.Overview.TokenUsageAvailable || report.Overview.TokenUsage != (usage.TokenUsage{}), report.Overview.TokenUsage.InputTokens, report.Overview.TokenUsage.CachedInputTokens, report.Overview.TokenUsage.CacheWriteInputTokens, report.Overview.TokenUsage.OutputTokens, report.Overview.TokenUsage.ReasoningOutputTokens, report.Overview.TokenUsage.TotalTokens}
		return json.MarshalIndent(value, "", "  ")
	case "tools":
		rows := make([]toolJSON, 0, len(report.Tools))
		for _, row := range report.Tools {
			rows = append(rows, toolJSON{row.Name, row.Calls, row.Failures, formatMachineTime(row.LastUsed)})
		}
		value := struct {
			SchemaVersion int                `json:"schema_version"`
			Agent         string             `json:"agent"`
			Source        string             `json:"source"`
			Sources       []usage.SourceKind `json:"sources,omitempty"`
			Agents        []string           `json:"agents"`
			Period        string             `json:"period"`
			GeneratedAt   string             `json:"generated_at"`
			Layer         string             `json:"layer"`
			Rows          []toolJSON         `json:"rows"`
		}{base.SchemaVersion, base.Agent, base.Source, base.Sources, base.Agents, base.Period, formatMachineTime(ctx.ReferenceTime), string(contextLayer(ctx)), rows}
		return json.MarshalIndent(value, "", "  ")
	case "skills":
		if ctx.SkillView == SkillViewUnused {
			rows := make([]unusedSkillJSON, 0, len(report.UnusedSkills))
			for _, row := range report.UnusedSkills {
				rows = append(rows, unusedSkillJSON{row.Name, row.Path, string(row.NameSource), row.NameMismatch})
			}
			roots := append([]string{}, report.UnusedRoots...)
			value := struct {
				SchemaVersion  int                `json:"schema_version"`
				Agent          string             `json:"agent"`
				Source         string             `json:"source"`
				Sources        []usage.SourceKind `json:"sources,omitempty"`
				Agents         []string           `json:"agents"`
				Period         string             `json:"period"`
				GeneratedAt    string             `json:"generated_at"`
				Strict         bool               `json:"strict"`
				GroupBy        string             `json:"group_by"`
				View           string             `json:"view"`
				Roots          []string           `json:"roots"`
				InstalledCount int                `json:"installed_count"`
				UnusedCount    int                `json:"unused_count"`
				Rows           []unusedSkillJSON  `json:"rows"`
			}{base.SchemaVersion, base.Agent, base.Source, base.Sources, base.Agents, base.Period, formatMachineTime(ctx.ReferenceTime), ctx.Strict, string(contextSkillGroupBy(ctx)), string(SkillViewUnused), roots, report.InstalledSkills, len(rows), rows}
			return json.MarshalIndent(value, "", "  ")
		}
		rows := make([]skillJSON, 0, len(report.Skills))
		for _, row := range report.Skills {
			rows = append(rows, skillJSON{row.Name, row.Explicit, row.Implicit, row.Unknown, row.Confirmed, row.Inferred, row.Unconfirmed, row.Total, formatMachineTime(row.LastUsed)})
		}
		value := struct {
			SchemaVersion int                `json:"schema_version"`
			Agent         string             `json:"agent"`
			Source        string             `json:"source"`
			Sources       []usage.SourceKind `json:"sources,omitempty"`
			Agents        []string           `json:"agents"`
			Period        string             `json:"period"`
			GeneratedAt   string             `json:"generated_at"`
			Strict        bool               `json:"strict"`
			GroupBy       string             `json:"group_by"`
			Rows          []skillJSON        `json:"rows"`
		}{base.SchemaVersion, base.Agent, base.Source, base.Sources, base.Agents, base.Period, formatMachineTime(ctx.ReferenceTime), ctx.Strict, string(contextSkillGroupBy(ctx)), rows}
		return json.MarshalIndent(value, "", "  ")
	default:
		return nil, fmt.Errorf("unknown report kind %q", kind)
	}
}

func contextLayer(ctx ReportContext) usage.ToolLayer {
	if ctx.Layer == "" {
		return usage.LayerEffective
	}
	return ctx.Layer
}

type toolJSON struct {
	Name     string `json:"name"`
	Calls    int    `json:"calls"`
	Failures int    `json:"failures"`
	LastUsed string `json:"last_used"`
}

type skillJSON struct {
	Name        string `json:"name"`
	Explicit    int    `json:"explicit"`
	Implicit    int    `json:"implicit"`
	Unknown     int    `json:"unknown"`
	Confirmed   int    `json:"confirmed"`
	Inferred    int    `json:"inferred"`
	Unconfirmed int    `json:"unconfirmed"`
	Total       int    `json:"total"`
	LastUsed    string `json:"last_used"`
}

type unusedSkillJSON struct {
	Name         string `json:"name"`
	Path         string `json:"path"`
	NameSource   string `json:"name_source"`
	NameMismatch bool   `json:"name_mismatch"`
}

// WriteJSON writes machine-readable output without adding ANSI sequences.
func WriteJSON(w io.Writer, kind string, ctx ReportContext, report aggregate.Report) error {
	data, err := RenderJSON(kind, ctx, report)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}

// SortMethods is kept here for callers serializing evidence outside reports.
func SortMethods(methods []usage.SkillEvidenceMethod) []usage.SkillEvidenceMethod {
	result := append([]usage.SkillEvidenceMethod(nil), methods...)
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
