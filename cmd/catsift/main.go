package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/xkumiyu/catsift/internal/aggregate"
	"github.com/xkumiyu/catsift/internal/cache"
	"github.com/xkumiyu/catsift/internal/codex"
	ctxsource "github.com/xkumiyu/catsift/internal/ctx"
	"github.com/xkumiyu/catsift/internal/githubcopilot"
	"github.com/xkumiyu/catsift/internal/opencode"
	"github.com/xkumiyu/catsift/internal/output"
	"github.com/xkumiyu/catsift/internal/purpose"
	"github.com/xkumiyu/catsift/internal/query"
	"github.com/xkumiyu/catsift/internal/skillinventory"
	"github.com/xkumiyu/catsift/internal/tui"
	"github.com/xkumiyu/catsift/internal/usage"
	appversion "github.com/xkumiyu/catsift/internal/version"
)

const usageText = `Usage:
  catsift [options]
  catsift <command> [options]

Run catsift without a command in an interactive terminal to open the read-only TUI.
Report subcommands output JSON by default.

Commands:
  stats     Show an overview of agent usage
  activity  Show daily activity
  models    Show model usage and details
  tools     Show tool usage by canonical name
  skills    Show skill usage and evidence state
  sessions  Show session usage and details
  config    Initialize configuration

Options:
  --source SOURCE   codex, copilot, opencode, or ctx; repeatable or comma-separated (default: detected agent histories; ctx is opt-in)
  --root PATH       Scan installed skills under this root (repeatable; default: ~/.agents/skills)
  --strict          Count confirmed skill evidence only
  --verbose         Show input/cache diagnostics and TUI source-load timings
  --strict-input    Exit non-zero when input records are skipped
  --help            Show this help
  --version         Show the catsift version

Run "catsift <command> --help" for command-specific options.
`

const statsUsageText = `Usage: catsift stats [options]

Show an overview of agent usage.

Options:
  --source SOURCE   codex, copilot, opencode, or ctx; repeatable or comma-separated (default: detected agent histories; ctx is opt-in)
  --days N          Include the last N days (N >= 1; default: all time)
  --from DATE       Include records on or after DATE (YYYY-MM-DD)
  --to DATE         Include records before the day after DATE (YYYY-MM-DD)
  --color MODE      auto, always, or never (default: auto; accepted for compatibility; JSON is unchanged)
  --verbose         Show input and cache diagnostic details
  --strict-input    Exit non-zero when input records are skipped
  --json            Deprecated: JSON is default; removed in v0.7.0 (false also emits JSON)
  --help            Show this help
`

const activityUsageText = `Usage: catsift activity [options]

Show daily activity for agent usage.

Options:
  --source SOURCE   codex, copilot, opencode, or ctx; repeatable or comma-separated (default: detected agent histories; ctx is opt-in)
  --days N          Include the last N days (N >= 1; default: all time)
  --from DATE       Include records on or after DATE (YYYY-MM-DD)
  --to DATE         Include records before the day after DATE (YYYY-MM-DD)
  --color MODE      auto, always, or never (default: auto; accepted for compatibility; JSON is unchanged)
  --verbose         Show input and cache diagnostic details
  --strict-input    Exit non-zero when input records are skipped
  --json            Deprecated: JSON is default; removed in v0.7.0 (false also emits JSON)
  --help            Show this help
`

const toolsUsageText = `Usage: catsift tools [options]

Show tool usage by canonical name.

Options:
  --source SOURCE   codex, copilot, opencode, or ctx; repeatable or comma-separated (default: detected agent histories; ctx is opt-in)
  --days N          Include the last N days (N >= 1; default: all time)
  --from DATE       Include records on or after DATE (YYYY-MM-DD)
  --to DATE         Include records before the day after DATE (YYYY-MM-DD)
  --color MODE      auto, always, or never (default: auto; accepted for compatibility; JSON is unchanged)
  --layer LAYER     effective, runtime, or model (default: effective)
  --verbose         Show input and cache diagnostic details
  --strict-input    Exit non-zero when input records are skipped
  --json            Deprecated: JSON is default; removed in v0.7.0 (false also emits JSON)
  --help            Show this help
`

const modelsUsageText = `Usage: catsift models [options]

Show model usage by provider and model name.

Options:
  --source SOURCE   codex, copilot, opencode, or ctx; repeatable or comma-separated (default: detected agent histories; ctx is opt-in)
  --days N          Include the last N days (N >= 1; default: all time)
  --from DATE       Include records on or after DATE (YYYY-MM-DD)
  --to DATE         Include records before the day after DATE (YYYY-MM-DD)
  --color MODE      auto, always, or never (default: auto; accepted for compatibility; JSON is unchanged)
  --verbose         Show input and cache diagnostic details
  --strict-input    Exit non-zero when input records are skipped
  --json            Deprecated: JSON is default; removed in v0.7.0 (false also emits JSON)
  --help            Show this help

Detail:
  catsift models detail PROVIDER/NAME [options]
`

const skillsUsageText = `Usage: catsift skills [options]

Show skill usage and evidence state.

Options:
  --source SOURCE   codex, copilot, opencode, or ctx; repeatable or comma-separated (default: detected agent histories; ctx is opt-in)
  --days N          Include the last N days (N >= 1; default: all time)
  --from DATE       Include records on or after DATE (YYYY-MM-DD)
  --to DATE         Include records before the day after DATE (YYYY-MM-DD)
  --color MODE      auto, always, or never (default: auto; accepted for compatibility; JSON is unchanged)
  --group-by UNIT   turn or session (default: turn; no effect on --unused)
  --strict          Count confirmed skill evidence only
  --view VIEW       auto, compact, mode, state, or all (default: auto; accepted for compatibility; JSON is unchanged)
  --unused          Show installed skills with no recorded usage
  --root PATH       Scan a scope root for .agents/skills, .codex/skills, plugin cache layouts (repeatable; only with --unused; default if omitted: ~/.agents/skills)
  --verbose         Show input and cache diagnostic details
  --strict-input    Exit non-zero when input records are skipped
  --json            Deprecated: JSON is default; removed in v0.7.0 (false also emits JSON)
  --help            Show this help

Detail:
  catsift skills detail NAME [options]
`

const sessionsUsageText = `Usage: catsift sessions [options]

Show session usage and turn details.

Options:
  --source SOURCE   codex, copilot, opencode, or ctx; repeatable or comma-separated (default: detected agent histories; ctx is opt-in)
  --days N          Include the last N days (N >= 1; default: all time)
  --from DATE       Include records on or after DATE (YYYY-MM-DD)
  --to DATE         Include records before the day after DATE (YYYY-MM-DD)
  --color MODE      auto, always, or never (default: auto; accepted for compatibility; JSON is unchanged)
  --verbose         Show input and cache diagnostic details
  --strict-input    Exit non-zero when input records are skipped
  --json            Deprecated: JSON is default; removed in v0.7.0 (false also emits JSON)
  --help            Show this help

Detail:
  catsift sessions detail ID [options]

When 20 or more new purpose requests are needed, Catsift asks before sending them.
Saved results do not count; non-interactive runs skip the batch.
`

const detailCommonOptionsText = `  --source SOURCE   codex, copilot, opencode, or ctx; repeatable or comma-separated (default: detected agent histories; ctx is opt-in)
  --days N          Include the last N days (N >= 1; default: all time)
  --from DATE       Include records on or after DATE (YYYY-MM-DD)
  --to DATE         Include records before the day after DATE (YYYY-MM-DD)
  --color MODE      auto, always, or never (default: auto; accepted for compatibility; JSON is unchanged)
  --verbose         Show input and cache diagnostic details
  --strict-input    Exit non-zero when input records are skipped
  --json            Deprecated: JSON is default; removed in v0.7.0 (false also emits JSON)
  --help            Show this help
`

const explorerUsageText = `Usage: catsift [options]

Explore model, usage, skill, and session details in an interactive terminal.
The TUI is read-only and does not display prompt text, tool arguments, or skill bodies.
When 20 or more new purpose requests are needed, Catsift asks before sending them.
TUI reload skips batches at this threshold.

Options:
  --source SOURCE   codex, copilot, opencode, or ctx; repeatable or comma-separated (default: detected agent histories; ctx is opt-in)
  --root PATH       Scan installed skills under this root (repeatable; default: ~/.agents/skills)
  --strict          Count confirmed skill evidence only
  --verbose         Show input/cache diagnostics and source-load timings
  --strict-input    Exit non-zero when input records are skipped
  --help            Show this help

Keys:
  1/2/3/4/5         Overview, Activity, Models, Skills, Sessions
  Tab/Shift+Tab      Move to the next/previous view
  Enter             Open detail; b/Esc returns to the previous view
  d                 Set period: all, N, YYYY-MM-DD[..YYYY-MM-DD]
  /                 Search safe metadata
  f                 Filter the selected model or skill
  a                 Filter Sessions by selected agent
  p                 Filter Sessions by selected project
  o                 Toggle source visibility
  u                 Switch Skills usage/unused
  r                 Reload the source and inventory snapshot
  s                 Cycle list sort
  c                 Clear TUI filters
  ?                 Show help
  q                 Quit
`

const dateLayout = "2006-01-02"
const usageExplorerKind = "__usage_explorer__"

var (
	helpHeadingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	helpCommandStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	helpOptionStyle  = helpCommandStyle
	helpMutedStyle   = lipgloss.NewStyle().Faint(true)
)

func renderHelp(text string, capabilities output.TerminalCapabilities) string {
	if !capabilities.ColorsEnabled() {
		return text
	}
	lines := strings.Split(text, "\n")
	inUsage, inCommands, inOptions, inDetail := false, false, false, false
	for index, line := range lines {
		if strings.HasPrefix(line, "Usage:") {
			lines[index] = helpHeadingStyle.Render("Usage:") + styleHelpInvocation(strings.TrimPrefix(line, "Usage:"))
			inUsage, inCommands, inOptions, inDetail = strings.TrimSpace(strings.TrimPrefix(line, "Usage:")) == "", false, false, false
			continue
		}
		switch line {
		case "Commands:":
			lines[index] = helpHeadingStyle.Render(line)
			inUsage, inCommands, inOptions, inDetail = false, true, false, false
			continue
		case "Options:":
			lines[index] = helpHeadingStyle.Render(line)
			inUsage, inCommands, inOptions, inDetail = false, false, true, false
			continue
		case "Detail:":
			lines[index] = helpHeadingStyle.Render(line)
			inUsage, inCommands, inOptions, inDetail = false, false, false, true
			continue
		case "Keys:":
			lines[index] = helpHeadingStyle.Render(line)
			inUsage, inCommands, inOptions, inDetail = false, false, false, false
			continue
		}
		if strings.TrimSpace(line) == "" {
			inUsage, inCommands, inOptions, inDetail = false, false, false, false
			continue
		}
		trimmed := strings.TrimLeft(line, " \t")
		if inUsage && strings.HasPrefix(trimmed, "catsift ") {
			lines[index] = styleHelpInvocation(line)
		} else if inCommands {
			lines[index] = styleHelpNamedLine(line, helpCommandStyle)
		} else if inOptions && strings.HasPrefix(trimmed, "-") {
			lines[index] = styleHelpOptionLine(line)
		} else if inDetail && strings.HasPrefix(trimmed, "catsift ") {
			lines[index] = styleHelpInvocation(line)
		} else {
			lines[index] = styleHelpMuted(line)
		}
	}
	return strings.Join(lines, "\n")
}

func styleHelpNamedLine(line string, style lipgloss.Style) string {
	trimmed := strings.TrimLeft(line, " \t")
	nameEnd := strings.IndexFunc(trimmed, unicode.IsSpace)
	if nameEnd < 0 {
		nameEnd = len(trimmed)
	}
	indent := len(line) - len(trimmed)
	return line[:indent] + style.Render(trimmed[:nameEnd]) + styleHelpMuted(trimmed[nameEnd:])
}

func styleHelpOptionLine(line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	nameEnd := strings.IndexFunc(trimmed, unicode.IsSpace)
	if nameEnd < 0 {
		nameEnd = len(trimmed)
	}
	indent := len(line) - len(trimmed)
	suffix := trimmed[nameEnd:]
	valueStart := strings.IndexFunc(suffix, func(r rune) bool { return !unicode.IsSpace(r) })
	if valueStart < 0 {
		return line[:indent] + helpOptionStyle.Render(trimmed[:nameEnd]) + styleHelpMuted(suffix)
	}
	valueEnd := strings.IndexFunc(suffix[valueStart:], unicode.IsSpace)
	if valueEnd < 0 {
		valueEnd = len(suffix)
	} else {
		valueEnd += valueStart
	}
	value := suffix[valueStart:valueEnd]
	if !isHelpPlaceholder(value) {
		return line[:indent] + helpOptionStyle.Render(trimmed[:nameEnd]) + styleHelpMuted(suffix)
	}
	return line[:indent] + helpOptionStyle.Render(trimmed[:nameEnd]) + suffix[:valueStart] + helpMutedStyle.Render(value) + styleHelpMuted(suffix[valueEnd:])
}

func isHelpPlaceholder(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' && r != '/' {
			return false
		}
	}
	return true
}

func styleHelpInvocation(line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	indent := len(line) - len(trimmed)
	const commandPrefix = "catsift "
	if !strings.HasPrefix(trimmed, commandPrefix) {
		return line[:indent] + styleHelpMuted(trimmed)
	}
	commands := []string{"stats", "activity", "models", "tools", "skills", "sessions"}
	for _, command := range commands {
		if trimmed == commandPrefix+command || strings.HasPrefix(trimmed, commandPrefix+command+" ") {
			nameEnd := len(commandPrefix) + len(command)
			return line[:indent] + helpCommandStyle.Render("catsift") + " " + helpCommandStyle.Render(command) + styleHelpMuted(trimmed[nameEnd:])
		}
	}
	return line[:indent] + helpCommandStyle.Render("catsift") + styleHelpMuted(trimmed[len("catsift"):])
}

func styleHelpMuted(text string) string {
	var styled strings.Builder
	position := 0
	for position < len(text) {
		start, end := nextHelpMutedSpan(text, position)
		if start < 0 {
			break
		}
		styled.WriteString(text[position:start])
		styled.WriteString(helpMutedStyle.Render(text[start:end]))
		position = end
	}
	if position == 0 {
		return text
	}
	styled.WriteString(text[position:])
	return styled.String()
}

func nextHelpMutedSpan(text string, from int) (int, int) {
	for index := from; index < len(text); index++ {
		var close byte
		switch text[index] {
		case '[':
			close = ']'
		case '<':
			close = '>'
		case '(':
			relativeEnd := strings.IndexByte(text[index+1:], ')')
			if relativeEnd < 0 {
				continue
			}
			end := index + 1 + relativeEnd
			if strings.Contains(strings.ToLower(text[index+1:end]), "default") {
				return index, end + 1
			}
		default:
			continue
		}
		if end := strings.IndexByte(text[index+1:], close); end >= 0 {
			return index, index + 2 + end
		}
	}
	return -1, -1
}

func writeHelp(w io.Writer, text string) {
	_, noColor := os.LookupEnv("NO_COLOR")
	capabilities := output.TerminalCapabilities{ColorMode: output.ColorAuto, NoColor: noColor}
	if file, ok := w.(*os.File); ok {
		capabilities = output.DetectCapabilities(file, output.ColorAuto, noColor)
	}
	_, _ = io.WriteString(w, renderHelp(text, capabilities))
}

func commandUsage(kind string) string {
	switch kind {
	case "stats":
		return statsUsageText
	case "activity":
		return activityUsageText
	case "models":
		return modelsUsageText
	case "tools":
		return toolsUsageText
	case "skills":
		return skillsUsageText
	case "sessions":
		return sessionsUsageText
	case usageExplorerKind:
		return explorerUsageText
	default:
		return usageText
	}
}

func detailUsageText(kind string) string {
	argument, description := "", ""
	switch kind {
	case "models":
		argument = "PROVIDER/NAME"
		description = "Show detail for one model."
	case "skills":
		argument = "NAME"
		description = "Show detail for one skill."
	case "sessions":
		argument = "ID"
		description = "Show detail for one session ID or unambiguous fragment."
	default:
		return usageText
	}
	options := detailCommonOptionsText
	if kind == "skills" {
		options = "  --strict          Count confirmed skill evidence only\n" + options
	}
	return fmt.Sprintf("Usage: catsift %s detail %s [options]\n\n%s\n\nOptions:\n%s", kind, argument, description, options)
}

func isDetailKind(kind string) bool {
	return kind == "models" || kind == "skills" || kind == "sessions"
}

func splitDetailCommand(kind string, args []string) (bool, string, []string, error) {
	if !isDetailKind(kind) || len(args) == 0 || !strings.EqualFold(args[0], "detail") {
		return false, "", args, nil
	}
	if len(args) == 1 {
		return true, "", nil, fmt.Errorf("catsift %s detail requires a selector", kind)
	}
	if args[1] == "--help" || args[1] == "-h" {
		return true, "", args[1:], nil
	}
	selector := strings.TrimSpace(args[1])
	if selector == "" || strings.HasPrefix(selector, "-") {
		return true, "", nil, fmt.Errorf("catsift %s detail requires a selector", kind)
	}
	return true, selector, args[2:], nil
}

func hasOption(args []string, option string) bool {
	for _, arg := range args {
		if arg == option || strings.HasPrefix(arg, option+"=") {
			return true
		}
	}
	return false
}

func parseDateOption(name, value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("%s must be a date in YYYY-MM-DD format", name)
	}
	parsed, err := time.ParseInLocation(dateLayout, value, time.UTC)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be a date in YYYY-MM-DD format", name)
	}
	return parsed, nil
}

func periodBounds(turns []usage.Turn) (from, to time.Time) {
	add := func(timestamp time.Time) {
		if timestamp.IsZero() {
			return
		}
		if from.IsZero() || timestamp.Before(from) {
			from = timestamp
		}
		if to.IsZero() || timestamp.After(to) {
			to = timestamp
		}
	}
	for _, turn := range turns {
		add(turn.StartedAt)
		add(turn.EndedAt)
		for _, timestamp := range turn.UserPromptTimes {
			add(timestamp)
		}
		for _, tool := range turn.ModelTools {
			add(tool.Timestamp)
		}
		for _, tool := range turn.RuntimeTools {
			add(tool.Timestamp)
		}
		for _, skill := range turn.SkillEvidence {
			add(skill.Timestamp)
		}
		for _, event := range turn.TokenUsageEvents {
			add(event.Timestamp)
		}
	}
	return from, to
}

func formatPeriod(turns []usage.Turn) string {
	from, to := periodBounds(turns)
	if from.IsZero() {
		return "no data"
	}
	return from.UTC().Format(dateLayout) + " to " + to.UTC().Format(dateLayout)
}

func formatPeriodInfo(turns []usage.Turn, daysSet bool, days int, from, to, now time.Time) string {
	if !daysSet && from.IsZero() && to.IsZero() {
		return ""
	}
	actualFrom, actualTo := periodBounds(turns)
	if actualFrom.IsZero() {
		return ""
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	requestedFrom, requestedTo := from, to
	if daysSet {
		requestedFrom = now.Add(-time.Duration(days) * 24 * time.Hour)
		requestedTo = now
	} else if requestedTo.IsZero() {
		requestedTo = now
	} else {
		requestedTo = requestedTo.AddDate(0, 0, -1)
	}
	var messages []string
	if !requestedFrom.IsZero() && dateBefore(requestedFrom, actualFrom) {
		messages = append(messages, fmt.Sprintf("selected period starts before the first usage record (%s)", actualFrom.UTC().Format(dateLayout)))
	}
	if !requestedTo.IsZero() && dateBefore(actualTo, requestedTo) {
		messages = append(messages, fmt.Sprintf("selected period ends after the last usage record (%s)", actualTo.UTC().Format(dateLayout)))
	}
	return strings.Join(messages, "; ")
}

func dateBefore(left, right time.Time) bool {
	left = left.UTC()
	right = right.UTC()
	left = time.Date(left.Year(), left.Month(), left.Day(), 0, 0, 0, 0, time.UTC)
	right = time.Date(right.Year(), right.Month(), right.Day(), 0, 0, 0, 0, time.UTC)
	return left.Before(right)
}

type stringList []string

func (values *stringList) String() string {
	return strings.Join(*values, ",")
}

func (values *stringList) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("skill root is empty")
	}
	*values = append(*values, value)
	return nil
}

type sourceList []usage.SourceKind

func (values *sourceList) String() string {
	items := make([]string, 0, len(*values))
	for _, value := range *values {
		items = append(items, string(value))
	}
	return strings.Join(items, ",")
}

func (values *sourceList) Set(value string) error {
	selected := []usage.SourceKind(*values)
	if err := appendSourceSelection(&selected, value); err != nil {
		return err
	}
	*values = sourceList(selected)
	return nil
}

func parseSourceSelection(values []string) ([]usage.SourceKind, error) {
	selected := make([]usage.SourceKind, 0, len(values))
	for _, value := range values {
		if err := appendSourceSelection(&selected, value); err != nil {
			return nil, err
		}
	}
	return orderedSourceKinds(selected), nil
}

func appendSourceSelection(selected *[]usage.SourceKind, value string) error {
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		source := usage.SourceKind(strings.ToLower(part))
		if !source.Valid() {
			return fmt.Errorf("invalid --source %q (want codex, copilot, opencode, or ctx)", part)
		}
		duplicate := false
		for _, existing := range *selected {
			if existing == source {
				duplicate = true
				break
			}
		}
		if !duplicate {
			*selected = append(*selected, source)
		}
	}
	return nil
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

func parseModelSelector(value string) (usage.ModelRef, error) {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return usage.ModelRef{}, errors.New("models detail selector must use provider/name")
	}
	return usage.NewModelRef(parts[0], parts[1]), nil
}

func queryFilterForHistory(history loadedHistory, sources []usage.SourceKind, daysSet bool, days int, from, to, now time.Time) query.Filter {
	filter := query.Filter{Sources: append([]usage.SourceKind(nil), sources...)}
	if len(sources) == 1 {
		filter.Source = sources[0]
	}
	if daysSet {
		filter.From = now.Add(-time.Duration(days) * 24 * time.Hour)
		filter.To = now
	} else {
		filter.From = from
		filter.To = to
	}
	if filter.Source == "" && len(filter.Sources) == 0 {
		filter.Source = history.Source
	}
	return filter
}

func selectSession(readModel query.ReadModel, selector string) (query.SessionSummary, error) {
	selector = strings.ToLower(strings.TrimSpace(selector))
	if selector == "" {
		return query.SessionSummary{}, errors.New("sessions detail selector must not be empty")
	}
	var exact []query.SessionSummary
	var partial []query.SessionSummary
	for _, row := range readModel.Sessions {
		id := strings.ToLower(strings.TrimSpace(row.ID))
		if id == selector {
			exact = append(exact, row)
			continue
		}
		if strings.Contains(id, selector) {
			partial = append(partial, row)
		}
	}
	candidates := exact
	if len(candidates) == 0 {
		candidates = partial
	}
	switch len(candidates) {
	case 1:
		return candidates[0], nil
	case 0:
		return query.SessionSummary{}, fmt.Errorf("session %q was not found in the selected scope", selector)
	default:
		labels := make([]string, 0, len(candidates))
		for _, row := range candidates {
			label := string(row.Source) + "/" + row.ID
			if row.Agent != "" && row.Agent != "unknown" {
				label += " agent=" + row.Agent
			}
			labels = append(labels, label)
		}
		return query.SessionSummary{}, fmt.Errorf("session %q is ambiguous; candidates: %s", selector, strings.Join(labels, ", "))
	}
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func defaultExplorerArgs(args []string) []string {
	if len(args) == 0 {
		return []string{usageExplorerKind}
	}
	if strings.HasPrefix(args[0], "-") && args[0] != "--help" && args[0] != "-h" && args[0] != "--version" {
		return append([]string{usageExplorerKind}, args...)
	}
	return args
}

func legacyAgentValue(agents []string) string {
	if len(agents) == 0 {
		return ""
	}
	return strings.Join(agents, ",")
}

type ctxHistoryLoader func(string, ctxsource.IngestOptions) (ctxsource.IngestResult, error)

type loadedHistory struct {
	query.Input
	SourcePath  string
	SourcePaths map[usage.SourceKind]string
}

type historyLoadOptions struct {
	Source      usage.SourceKind
	Sources     []usage.SourceKind
	AutoDetect  bool
	Days        int
	DaysSet     bool
	From        time.Time
	To          time.Time
	Now         time.Time
	CacheDir    string
	Files       []string
	Verbose     bool
	Diagnostics diagnosticWriter
	LoadCtx     ctxHistoryLoader
}

func loadHistory(options historyLoadOptions) (loadedHistory, error) {
	result := loadedHistory{Input: query.Input{Source: options.Source, From: options.From, To: options.To}}
	switch options.Source {
	case usage.SourceCtx:
		ctxOptions := ctxsource.IngestOptions{Days: options.Days, DaysSet: options.DaysSet, From: options.From, To: options.To, Now: options.Now, CacheDir: options.CacheDir}
		if options.Verbose {
			ctxOptions.Diagnostic = func(message string) { options.Diagnostics.write("debug", message) }
		}
		loader := options.LoadCtx
		if loader == nil {
			loader = ctxsource.Load
		}
		input, err := loader("", ctxOptions)
		if err != nil {
			return loadedHistory{}, fmt.Errorf("read ctx history: %w", err)
		}
		result.Input = query.Input{Turns: input.Turns, Sessions: input.Sessions, Warnings: input.Warnings, Agents: input.Agents, Source: options.Source, From: options.From, To: options.To}
	case usage.SourceOpenCode:
		home, err := opencode.ResolveHome("")
		if err != nil {
			return loadedHistory{}, fmt.Errorf("resolve OpenCode data root: %w", err)
		}
		result.SourcePath = home
		input, err := opencode.Load(home, opencode.IngestOptions{Days: options.Days, DaysSet: options.DaysSet, From: options.From, To: options.To, Now: options.Now, CacheDir: options.CacheDir, Diagnostic: func(message string) {
			if options.Verbose {
				options.Diagnostics.write("debug", message)
			}
		}})
		if err != nil {
			return loadedHistory{}, fmt.Errorf("read OpenCode history %q: %w", home, err)
		}
		result.SourcePath = home
		result.Input = query.Input{Turns: input.Turns, Sessions: input.Sessions, Warnings: input.Warnings, Agents: input.Agents, Source: options.Source, From: options.From, To: options.To}
	case usage.SourceCodex:
		home, err := codex.ResolveHome("")
		if err != nil {
			return loadedHistory{}, fmt.Errorf("resolve Codex home: %w", err)
		}
		result.SourcePath = home
		codexOptions := codex.IngestOptions{Days: options.Days, DaysSet: options.DaysSet, From: options.From, To: options.To, Now: options.Now, CacheDir: options.CacheDir, Files: options.Files}
		if options.Verbose {
			codexOptions.Diagnostic = func(message string) { options.Diagnostics.write("debug", message) }
		}
		input, err := codex.Load(home, codexOptions)
		if err != nil {
			return loadedHistory{}, fmt.Errorf("read Codex history %q: %w", home, err)
		}
		result.Input = query.Input{Turns: input.Turns, Sessions: input.Sessions, Warnings: input.Warnings, Agents: []string{"codex"}, Source: options.Source, From: options.From, To: options.To}
	case usage.SourceCopilot:
		home, err := githubcopilot.ResolveHome("")
		if err != nil {
			return loadedHistory{}, fmt.Errorf("resolve GitHub Copilot data root: %w", err)
		}
		input, err := githubcopilot.Load(home, githubcopilot.IngestOptions{
			Days: options.Days, DaysSet: options.DaysSet, From: options.From, To: options.To, Now: options.Now, CacheDir: options.CacheDir,
			Diagnostic: func(message string) {
				if options.Verbose {
					options.Diagnostics.write("debug", message)
				}
			},
		})
		if err != nil {
			return loadedHistory{}, fmt.Errorf("read GitHub Copilot history %q: %w", home, err)
		}
		result.SourcePath = home
		result.Input = query.Input{Turns: input.Turns, Sessions: input.Sessions, Warnings: input.Warnings, Agents: input.Agents, Source: options.Source, From: options.From, To: options.To}
	default:
		return loadedHistory{}, fmt.Errorf("unsupported history source %q", options.Source)
	}
	result.Sources = []usage.SourceKind{options.Source}
	result.SourcePaths = map[usage.SourceKind]string{options.Source: result.SourcePath}
	return result, nil
}

func loadPurposeSettings(configDir string) (purpose.Config, error) {
	config, err := purpose.LoadConfig(filepath.Join(configDir, "catsift", "config.yaml"))
	if err != nil {
		return purpose.Config{}, fmt.Errorf("read session purpose config: %w", err)
	}
	return config, nil
}

const purposeBatchConfirmationThreshold = 20

// purposeVerificationBudget bounds how many stored results are re-verified
// with message bodies per run. Verified results do not call the API.
const purposeVerificationBudget = 50

// purposeVerificationInterval is how long an evidence-based reuse is trusted
// before the budgeted sweep reads the bodies again.
const purposeVerificationInterval = 7 * 24 * time.Hour

func confirmPurposeClassification(count int, interactive bool, stdin io.Reader, stderr io.Writer) bool {
	if !interactive {
		return false
	}
	if _, err := fmt.Fprintf(stderr, "%d new session purpose requests to TypeSafe may incur API charges. Continue? [y/N]: ", count); err != nil {
		return false
	}
	answer, err := bufio.NewReader(stdin).ReadString('\n')
	_, _ = fmt.Fprintln(stderr)
	if err != nil && !errors.Is(err, io.EOF) {
		return false
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}

type purposeClassificationOptions struct {
	Config     purpose.Config
	Base       string
	APIKey     string
	Confirm    func(int) bool
	LoadBodies func([]usage.Session) (map[string][]string, error)
	Now        time.Time
}

type purposeCandidate struct {
	key      string
	title    string
	messages []string
	evidence string
}

type purposePlan struct {
	session  usage.Session
	key      string
	evidence string
	cached   purpose.Result
	exists   bool
	classify bool
}

func applyPurposeClassification(input *query.Input, options purposeClassificationOptions) error {
	if input == nil {
		return nil
	}
	apiURL := options.Config.SessionPurpose.APIURL
	storePath := filepath.Join(options.Base, "purposes.json")
	results, err := purpose.LoadStore(storePath)
	if err != nil {
		return fmt.Errorf("read session purpose results: %w", err)
	}
	now := options.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	input.Purposes = make(map[string]string)
	changed := false

	messagesBySession := make(map[string][]string)
	promptCounts := make(map[string]int)
	promptTimes := make(map[string][]time.Time)
	incompleteBodies := make(map[string]bool)
	for _, turn := range input.Turns {
		key := usage.NewSessionKey(turn.Source, turn.SessionID)
		promptCounts[key] += turn.UserPrompts
		promptTimes[key] = append(promptTimes[key], turn.UserPromptTimes...)
		if len(turn.UserMessages) != turn.UserPrompts {
			incompleteBodies[key] = true
		}
		if len(turn.UserMessages) > 0 {
			messagesBySession[key] = append(messagesBySession[key], turn.UserMessages...)
		}
	}

	plans := make([]purposePlan, 0)
	sweep := make([]purposePlan, 0)
	for _, session := range input.Sessions {
		key := usage.NewSessionKey(session.Source, session.ID)
		if promptCounts[key] == 0 && len(messagesBySession[key]) == 0 {
			continue
		}
		item := purposePlan{session: session, key: key, evidence: sessionPurposeEvidence(session, promptCounts[key], promptTimes[key])}
		cached, exists := results[key]
		item.cached = cached
		item.exists = exists
		switch {
		case !exists:
			item.classify = true
		case cached.Version != purpose.Version || cached.TaxonomyFingerprint != purpose.TaxonomyFingerprint():
			item.classify = true
		case purpose.Reusable(cached, item.evidence):
			input.Purposes[key] = cached.ModeName
			if cached.VerifiedAt.IsZero() || !cached.VerifiedAt.After(now.Add(-purposeVerificationInterval)) {
				sweep = append(sweep, item)
			}
			continue
		}
		plans = append(plans, item)
	}
	if len(sweep) > purposeVerificationBudget {
		sort.SliceStable(sweep, func(i, j int) bool {
			return sweep[i].cached.VerifiedAt.Before(sweep[j].cached.VerifiedAt)
		})
		sweep = sweep[:purposeVerificationBudget]
	}
	plans = append(plans, sweep...)

	var bodyLoadErr error
	if len(plans) > 0 && options.LoadBodies != nil {
		sessions := make([]usage.Session, 0, len(plans))
		for _, item := range plans {
			if len(messagesBySession[item.key]) == 0 || incompleteBodies[item.key] {
				sessions = append(sessions, item.session)
			}
		}
		if len(sessions) > 0 {
			loaded, loadErr := options.LoadBodies(sessions)
			if loadErr != nil {
				bodyLoadErr = fmt.Errorf("read session purpose input: %w", loadErr)
			}
			for _, session := range sessions {
				key := usage.NewSessionKey(session.Source, session.ID)
				messagesBySession[key] = loaded[key]
			}
		}
	}

	pending := make([]purposeCandidate, 0, len(plans))
	for _, item := range plans {
		messages := messagesBySession[item.key]
		if purpose.Input("", messages) == "" {
			continue
		}
		delete(input.Purposes, item.key)
		if !item.classify && item.exists && purpose.Current(item.cached, item.session.Title, messages) {
			item.cached.VerifiedAt = now
			item.cached.Evidence = item.evidence
			results[item.key] = item.cached
			input.Purposes[item.key] = item.cached.ModeName
			changed = true
			continue
		}
		pending = append(pending, purposeCandidate{key: item.key, title: item.session.Title, messages: messages, evidence: item.evidence})
	}

	if changed {
		if err := purpose.SaveStore(storePath, results); err != nil {
			return fmt.Errorf("save session purpose results: %w", err)
		}
		changed = false
	}
	if len(pending) >= purposeBatchConfirmationThreshold && (options.Confirm == nil || !options.Confirm(len(pending))) {
		return fmt.Errorf("classification of %d sessions requires confirmation; no API requests sent", len(pending))
	}
	failed := 0
	workers := min(4, len(pending))
	if workers > 0 {
		type outcome struct {
			candidate purposeCandidate
			result    purpose.Result
			err       error
		}
		jobs := make(chan purposeCandidate)
		completed := make(chan outcome, len(pending))
		for range workers {
			go func() {
				for job := range jobs {
					result, err := purpose.Classify(context.Background(), nil, apiURL, options.APIKey, job.title, job.messages)
					completed <- outcome{candidate: job, result: result, err: err}
				}
			}()
		}
		for _, job := range pending {
			jobs <- job
		}
		close(jobs)
		for range len(pending) {
			outcome := <-completed
			if outcome.err != nil {
				failed++
				continue
			}
			outcome.result.VerifiedAt = now
			outcome.result.Evidence = outcome.candidate.evidence
			results[outcome.candidate.key] = outcome.result
			changed = true
			input.Purposes[outcome.candidate.key] = outcome.result.ModeName
		}
	}
	if changed {
		if err := purpose.SaveStore(storePath, results); err != nil {
			return fmt.Errorf("save session purpose results: %w", err)
		}
	}
	if failed > 0 {
		return errors.Join(bodyLoadErr, fmt.Errorf("classification failed for %d session(s)", failed))
	}
	return bodyLoadErr
}

// sessionPurposeEvidence describes local metadata used to decide whether a
// stored classification still applies. It never reads message bodies.
func sessionPurposeEvidence(session usage.Session, promptCount int, promptTimes []time.Time) string {
	parts := []string{strconv.Itoa(promptCount), ""}
	if session.Source.Source == usage.SourceCodex {
		if path := strings.TrimSpace(session.Source.Path); path != "" {
			if info, err := os.Stat(path); err == nil {
				parts[1] = fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
			}
		}
	}
	// Selected prompt times detect window changes even when counts stay equal.
	for _, timestamp := range promptTimes {
		parts = append(parts, timestamp.UTC().Format(time.RFC3339Nano))
	}
	return purpose.Evidence(session.Title, session.UpdatedAt, strings.Join(parts, "\x00"))
}

// loadPurposeMessages reads message bodies only for the sessions that need
// verification. Codex limits the read to the matching history files.
func loadPurposeMessages(options historyLoadOptions, sessions []usage.Session) (map[string][]string, error) {
	bySource := make(map[usage.SourceKind][]usage.Session)
	order := make([]usage.SourceKind, 0, 2)
	for _, session := range sessions {
		source := session.Source.Source
		if !source.Valid() {
			continue
		}
		if _, ok := bySource[source]; !ok {
			order = append(order, source)
		}
		bySource[source] = append(bySource[source], session)
	}
	messages := make(map[string][]string)
	var loadErrors []error
	for _, source := range order {
		sourceOptions := options
		sourceOptions.Source = source
		sourceOptions.Sources = []usage.SourceKind{source}
		sourceOptions.CacheDir = ""
		sourceOptions.Files = nil
		if source == usage.SourceCodex {
			if files, complete := purposeCodexFiles(bySource[source]); complete {
				sourceOptions.Files = files
			}
		}
		history, err := loadHistory(sourceOptions)
		if err != nil {
			loadErrors = append(loadErrors, fmt.Errorf("%s: %w", source, err))
			continue
		}
		for _, turn := range history.Turns {
			key := usage.NewSessionKey(turn.Source, turn.SessionID)
			messages[key] = append(messages[key], turn.UserMessages...)
		}
	}
	return messages, errors.Join(loadErrors...)
}

func purposeCodexFiles(sessions []usage.Session) ([]string, bool) {
	seen := make(map[string]struct{}, len(sessions))
	files := make([]string, 0, len(sessions))
	for _, session := range sessions {
		path := strings.TrimSpace(session.Source.Path)
		if path == "" {
			return nil, false
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		files = append(files, path)
	}
	return files, true
}

func detectDefaultSourceKinds() ([]usage.SourceKind, error) {
	sources := make([]usage.SourceKind, 0)
	for _, source := range usage.DefaultSourceKinds() {
		var (
			available bool
			err       error
		)
		switch source {
		case usage.SourceCodex:
			var home string
			home, err = codex.ResolveHome("")
			if err == nil {
				available, err = codex.HasHistory(home)
			}
		case usage.SourceOpenCode:
			var root string
			root, err = opencode.ResolveHome("")
			if err == nil {
				available, err = opencode.HasHistory(root)
			}
		case usage.SourceCopilot:
			var root string
			root, err = githubcopilot.ResolveHome("")
			if err == nil {
				available, err = githubcopilot.HasHistory(root)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("detect %s history: %w", source, err)
		}
		if available {
			sources = append(sources, source)
		}
	}
	return sources, nil
}

func selectedHistorySources(options historyLoadOptions) ([]usage.SourceKind, error) {
	if len(options.Sources) > 0 {
		return orderedSourceKinds(options.Sources), nil
	}
	if options.Source.Valid() {
		return []usage.SourceKind{options.Source}, nil
	}
	if options.AutoDetect {
		return detectDefaultSourceKinds()
	}
	return usage.DefaultSourceKinds(), nil
}

func loadSelectedHistory(options historyLoadOptions) (loadedHistory, error) {
	sources, err := selectedHistorySources(options)
	if err != nil {
		return loadedHistory{}, err
	}
	if len(sources) == 0 {
		return loadedHistory{}, errors.New("no history source selected")
	}
	options.Sources = sources
	if len(sources) == 1 {
		options.Source = sources[0]
		return loadHistory(options)
	}
	return loadAllHistoryWith(options, loadHistory)
}

func sourceLoadLabel(sources []usage.SourceKind) string {
	if len(sources) != 1 {
		return "Reading history sources"
	}
	switch sources[0] {
	case usage.SourceCtx:
		return "Reading ctx history"
	case usage.SourceOpenCode:
		return "Reading OpenCode history"
	case usage.SourceCopilot:
		return "Reading GitHub Copilot history"
	default:
		return "Reading Codex history"
	}
}

type sourceLoadResult struct {
	source  usage.SourceKind
	history loadedHistory
	elapsed time.Duration
	err     error
}

func loadAllHistoryWith(options historyLoadOptions, loadSource func(historyLoadOptions) (loadedHistory, error)) (loadedHistory, error) {
	if loadSource == nil {
		loadSource = loadHistory
	}
	sources, err := selectedHistorySources(options)
	if err != nil {
		return loadedHistory{}, err
	}
	started := time.Now()
	if options.Verbose {
		defer func() {
			options.Diagnostics.write("debug", fmt.Sprintf("selected history sources loaded in %s", formatSpinnerElapsed(time.Since(started))))
		}()
	}
	results := make(chan sourceLoadResult, len(sources))
	var group sync.WaitGroup
	for _, source := range sources {
		source := source
		group.Add(1)
		go func() {
			defer group.Done()
			current := options
			current.Source = source
			loadStarted := time.Now()
			history, err := loadSource(current)
			results <- sourceLoadResult{source: source, history: history, elapsed: time.Since(loadStarted), err: err}
		}()
	}
	group.Wait()
	close(results)

	bySource := make(map[usage.SourceKind]sourceLoadResult, len(sources))
	for result := range results {
		bySource[result.source] = result
	}
	loaded := make([]loadedHistory, 0, len(sources))
	var failures []sourceLoadResult
	for _, source := range sources {
		result := bySource[source]
		if result.err != nil {
			if options.AutoDetect && errors.Is(result.err, os.ErrNotExist) {
				continue
			}
			failures = append(failures, result)
			if options.Verbose {
				options.Diagnostics.write("debug", fmt.Sprintf("%s source unavailable after %s: %v", source, formatSpinnerElapsed(result.elapsed), result.err))
			}
			continue
		}
		loaded = append(loaded, result.history)
		if options.Verbose {
			options.Diagnostics.write("debug", fmt.Sprintf("source %s loaded in %s", source, formatSpinnerElapsed(result.elapsed)))
		}
	}
	if len(loaded) == 0 {
		if len(failures) == 0 {
			return loadedHistory{}, errors.New("no history source could be loaded")
		}
		details := make([]string, 0, len(failures))
		for _, failure := range failures {
			details = append(details, fmt.Sprintf("%s: %v", failure.source, failure.err))
		}
		return loadedHistory{}, fmt.Errorf("no history source could be loaded: %s", strings.Join(details, "; "))
	}
	result := mergeLoadedHistories(loaded...)
	if !options.AutoDetect {
		result.Sources = append([]usage.SourceKind(nil), sources...)
		result.Source = ""
		if len(result.Sources) == 1 {
			result.Source = result.Sources[0]
		}
	}
	for _, failure := range failures {
		result.Warnings = append(result.Warnings, usage.Warning{Reason: "source_unavailable", Type: string(failure.source), Source: failure.source, Count: 1})
	}
	return result, nil
}

func mergeLoadedHistories(values ...loadedHistory) loadedHistory {
	result := loadedHistory{SourcePaths: make(map[usage.SourceKind]string)}
	seenSources := make(map[usage.SourceKind]struct{})
	for _, value := range values {
		result.Turns = append(result.Turns, value.Turns...)
		result.Sessions = append(result.Sessions, value.Sessions...)
		result.Agents = append(result.Agents, value.Agents...)
		result.Warnings = append(result.Warnings, value.Warnings...)
		result.From = value.From
		result.To = value.To
		if value.SourcePath != "" {
			result.SourcePath = value.SourcePath
		}
		for source, path := range value.SourcePaths {
			result.SourcePaths[source] = path
		}
		sources := value.Sources
		if len(sources) == 0 && value.Source.Valid() {
			sources = []usage.SourceKind{value.Source}
		}
		if value.SourcePath != "" && len(sources) == 1 {
			result.SourcePaths[sources[0]] = value.SourcePath
		}
		for _, source := range sources {
			seenSources[source] = struct{}{}
		}
	}
	for _, source := range usage.AllSourceKinds() {
		if _, ok := seenSources[source]; ok {
			result.Sources = append(result.Sources, source)
		}
	}
	if len(result.Sources) == 1 {
		result.Source = result.Sources[0]
	}
	return result
}

func run(args []string, stdout, stderr io.Writer) int {
	return runWithCtxLoader(args, stdout, stderr, ctxsource.Load)
}

func runWithCtxLoader(args []string, stdout, stderr io.Writer, loadCtx ctxHistoryLoader) int {
	args = defaultExplorerArgs(args)
	_, noColor := os.LookupEnv("NO_COLOR")
	diagnostics := newDiagnosticWriter(stderr, output.ColorAuto, noColor)
	kind := strings.ToLower(args[0])
	if kind == "config" {
		if len(args) == 2 && strings.EqualFold(args[1], "init") {
			configDir, err := os.UserConfigDir()
			if err != nil {
				diagnostics.errorf("resolve user config directory: %v", err)
				return 1
			}
			path := filepath.Join(configDir, "catsift", "config.yaml")
			if err := purpose.InitializeConfig(path); err != nil {
				diagnostics.errorf("initialize config: %v", err)
				return 1
			}
			_, _ = fmt.Fprintf(stdout, "Created %s\nSet session_purpose.enabled: true to enable session purpose classification.\n", path)
			return 0
		}
		if (len(args) == 2 && (args[1] == "--help" || args[1] == "-h")) ||
			(len(args) == 3 && strings.EqualFold(args[1], "init") && (args[2] == "--help" || args[2] == "-h")) {
			writeHelp(stdout, "Usage: catsift config init\n\nCreate a YAML configuration file without overwriting an existing one.\n")
			return 0
		}
		diagnostics.errorf("usage: catsift config init")
		return 2
	}
	if kind == "help" || kind == "--help" || kind == "-h" {
		if kind == "help" && len(args) > 1 {
			requested := strings.ToLower(args[1])
			if requested == "stats" || requested == "activity" || requested == "models" || requested == "tools" || requested == "skills" || requested == "sessions" {
				if len(args) > 2 && strings.EqualFold(args[2], "detail") && isDetailKind(requested) {
					writeHelp(stdout, detailUsageText(requested))
					return 0
				}
				writeHelp(stdout, commandUsage(requested))
				return 0
			}
		}
		writeHelp(stdout, usageText)
		return 0
	}
	if kind == "--version" {
		_, _ = fmt.Fprintf(stdout, "catsift %s\n", appversion.String())
		return 0
	}
	if kind != "stats" && kind != "activity" && kind != "models" && kind != "tools" && kind != "skills" && kind != "sessions" && kind != usageExplorerKind {
		diagnostics.errorf("unknown command %q", args[0])
		writeHelp(stderr, "\n"+usageText)
		return 2
	}
	if len(args) > 1 && strings.EqualFold(args[1], "detail") && !isDetailKind(kind) {
		diagnostics.errorf("detail is only valid for models, skills, or sessions")
		return 2
	}
	detailMode, detailSelector, commandArgs, err := splitDetailCommand(kind, args[1:])
	if err != nil {
		diagnostics.errorf("%v", err)
		return 2
	}
	if hasOption(commandArgs, "--help") || hasOption(commandArgs, "-h") {
		if detailMode {
			writeHelp(stdout, detailUsageText(kind))
		} else {
			writeHelp(stdout, commandUsage(kind))
		}
		return 0
	}
	if hasOption(commandArgs, "--version") {
		diagnostics.errorf("--version is a top-level option; use catsift --version")
		return 2
	}
	if kind != "skills" && hasOption(commandArgs, "--group-by") {
		diagnostics.errorf("--group-by is only valid for skills")
		return 2
	}

	flags := flag.NewFlagSet(kind, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		if detailMode {
			writeHelp(stderr, detailUsageText(kind))
			return
		}
		writeHelp(stderr, commandUsage(kind))
	}
	var sourceValues sourceList
	flags.Var(&sourceValues, "source", "history sources (repeatable or comma-separated)")
	days := flags.Int("days", 0, "include the last N days")
	from := flags.String("from", "", "include records on or after date")
	to := flags.String("to", "", "include records through date")
	color := flags.String("color", string(output.ColorAuto), "compatibility color mode (JSON is unchanged)")
	layer := flags.String("layer", string(usage.LayerEffective), "tool layer")
	var groupBy *string
	if kind == "skills" {
		groupBy = flags.String("group-by", string(aggregate.SkillGroupByTurn), "skill aggregation unit")
	}
	strict := flags.Bool("strict", false, "count confirmed skills only")
	view := flags.String("view", string(output.SkillUsageViewAuto), "compatibility skill view (JSON is unchanged)")
	unused := flags.Bool("unused", false, "show installed skills with no recorded usage")
	var roots stringList
	flags.Var(&roots, "root", "scan a scope root for supported skill layouts (repeatable; interactive view or skills --unused)")
	verbose := flags.Bool("verbose", false, "show input and cache diagnostic details")
	strictInput := flags.Bool("strict-input", false, "exit non-zero when input records are skipped")
	flags.Bool("json", false, "deprecated: JSON is default; removed in v0.7.0")
	if err := flags.Parse(commandArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if extra := flags.Args(); len(extra) > 0 {
		diagnostics.errorf("unexpected argument %q", extra[0])
		return 2
	}
	jsonFlagSet := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "json" {
			jsonFlagSet = true
		}
	})
	if kind == usageExplorerKind && jsonFlagSet {
		diagnostics.errorf("--json is not supported for the interactive view")
		return 2
	}
	if kind == usageExplorerKind {
		for _, option := range []struct {
			name string
			used bool
		}{
			{name: "--color", used: hasOption(commandArgs, "--color")},
			{name: "--layer", used: hasOption(commandArgs, "--layer")},
			{name: "--view", used: hasOption(commandArgs, "--view")},
		} {
			if option.used {
				diagnostics.errorf("%s is a report option; use a CLI subcommand", option.name)
				return 2
			}
		}
	}
	selectedSources := orderedSourceKinds([]usage.SourceKind(sourceValues))
	autoDetectSources := len(selectedSources) == 0
	selectedSource := usage.SourceKind("")
	if len(selectedSources) == 1 {
		selectedSource = selectedSources[0]
	}
	mode := output.ColorMode(*color)
	diagnostics = newDiagnosticWriter(stderr, mode, noColor)
	if kind != usageExplorerKind {
		diagnostics = newDiagnosticWriter(stderr, output.ColorNever, noColor)
	}
	if jsonFlagSet {
		diagnostics.write("warning", "--json is deprecated: --json is unnecessary because JSON is now default and will be removed in v0.7.0")
	}
	if !mode.Valid() {
		diagnostics.errorf("invalid --color %q (want auto, always, or never)", *color)
		return 2
	}
	selectedLayer := usage.ToolLayer(*layer)
	if selectedLayer != usage.LayerEffective && selectedLayer != usage.LayerRuntime && selectedLayer != usage.LayerModel {
		diagnostics.errorf("invalid --layer %q (want effective, runtime, or model)", *layer)
		return 2
	}
	if kind != "tools" && *layer != string(usage.LayerEffective) {
		diagnostics.errorf("--layer is only valid for tools")
		return 2
	}
	selectedGroupBy := aggregate.SkillGroupByTurn
	if groupBy != nil {
		selectedGroupBy = aggregate.SkillGroupBy(*groupBy)
		if !selectedGroupBy.Valid() {
			diagnostics.errorf("invalid --group-by %q (want turn or session)", *groupBy)
			return 2
		}
	}
	if kind != "skills" && kind != usageExplorerKind && *strict {
		diagnostics.errorf("--strict is only valid for skills")
		return 2
	}
	selectedSkillUsageView := output.SkillUsageView(*view)
	if !selectedSkillUsageView.Valid() {
		diagnostics.errorf("invalid --view %q (want auto, compact, mode, state, or all)", *view)
		return 2
	}
	if kind != "skills" && selectedSkillUsageView != output.SkillUsageViewAuto {
		diagnostics.errorf("--view is only valid for skills")
		return 2
	}
	if *unused && selectedSkillUsageView != output.SkillUsageViewAuto {
		diagnostics.errorf("--view cannot be combined with --unused")
		return 2
	}
	if kind == "skills" && detailMode {
		if *unused {
			diagnostics.errorf("--unused cannot be combined with skills detail")
			return 2
		}
		if hasOption(commandArgs, "--group-by") {
			diagnostics.errorf("--group-by cannot be combined with skills detail")
			return 2
		}
		if hasOption(commandArgs, "--view") {
			diagnostics.errorf("--view cannot be combined with skills detail")
			return 2
		}
	}
	if kind != "skills" && *unused {
		_, _ = fmt.Fprintln(stderr, "error: --unused is only valid for skills")
		return 2
	}
	if len(roots) > 0 && kind != usageExplorerKind && (kind != "skills" || !*unused) {
		_, _ = fmt.Fprintln(stderr, "error: --root is only valid with --unused for skills")
		return 2
	}
	if *days < 0 {
		diagnostics.errorf("--days must be at least 1")
		return 2
	}
	daysSet := false
	fromSet := false
	toSet := false
	flags.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "days":
			daysSet = true
		case "from":
			fromSet = true
		case "to":
			toSet = true
		}
	})
	if kind == usageExplorerKind && (daysSet || fromSet || toSet) {
		diagnostics.errorf("--days, --from, and --to are not supported for the interactive view; use d in the TUI")
		return 2
	}
	if daysSet && *days == 0 {
		diagnostics.errorf("--days must be at least 1")
		return 2
	}
	if daysSet && (fromSet || toSet) {
		diagnostics.errorf("--days cannot be combined with --from or --to")
		return 2
	}
	var fromDate, toDate time.Time
	if fromSet {
		var err error
		fromDate, err = parseDateOption("--from", *from)
		if err != nil {
			diagnostics.errorf("%v", err)
			return 2
		}
	}
	if toSet {
		date, err := parseDateOption("--to", *to)
		if err != nil {
			diagnostics.errorf("%v", err)
			return 2
		}
		toDate = date.AddDate(0, 0, 1)
	}
	if !fromDate.IsZero() && !toDate.IsZero() && !fromDate.Before(toDate) {
		diagnostics.errorf("--from must not be after --to")
		return 2
	}
	var selectedModel usage.ModelRef
	if kind == "models" && detailMode {
		var err error
		selectedModel, err = parseModelSelector(detailSelector)
		if err != nil {
			diagnostics.errorf("%v", err)
			return 2
		}
	}

	now := time.Now().UTC()
	cacheDir, _ := cache.DefaultDir()
	classifyPurposes := false
	var purposeConfig purpose.Config
	var purposeBase, purposeAPIKey string
	if kind == "sessions" || kind == usageExplorerKind {
		configDir, err := os.UserConfigDir()
		if err != nil {
			diagnostics.write("warning", "session purpose classification skipped: cannot locate user config directory")
		} else {
			purposeBase = filepath.Join(configDir, "catsift")
			purposeConfig, err = loadPurposeSettings(configDir)
			if err != nil {
				diagnostics.write("warning", "session purpose classification skipped: "+err.Error())
			} else if purposeConfig.SessionPurpose.Enabled {
				purposeAPIKey = strings.TrimSpace(os.Getenv("TYPESAFE_API_KEY"))
				if purposeAPIKey == "" {
					diagnostics.write("warning", "session purpose is enabled but TYPESAFE_API_KEY is not set; classification skipped")
				} else {
					classifyPurposes = true
				}
			}
		}
	}
	if *verbose {
		diagnostics.write("debug", fmt.Sprintf("catsift version: %s", appversion.String()))
		diagnostics.write("debug", fmt.Sprintf("cache version: %s schema=%d dir=%q", cache.Version, cache.SchemaVersion, cacheDir))
	}
	if kind == usageExplorerKind && !tui.IsInteractive(os.Stdin, stdout) {
		diagnostics.errorf("%v; use catsift stats, catsift activity, catsift tools, or catsift skills for non-interactive reports", tui.ErrNotInteractive)
		return 1
	}
	progress := newSpinner(stderr, kind == usageExplorerKind && diagnostics.capabilities.IsTTY, diagnostics.capabilities.ColorsEnabled())
	diagnostics.progress = progress.writeDiagnostic
	var (
		history      loadedHistory
		stopProgress func()
	)
	label := sourceLoadLabel(selectedSources)
	stopProgress = progress.Start(label)
	loadOptions := historyLoadOptions{
		Source: selectedSource, Sources: selectedSources, AutoDetect: autoDetectSources,
		Days: *days, DaysSet: daysSet, From: fromDate, To: toDate, Now: now, CacheDir: cacheDir, Verbose: *verbose, Diagnostics: diagnostics, LoadCtx: loadCtx,
	}
	var loadErr error
	history, loadErr = loadSelectedHistory(loadOptions)
	if loadErr != nil {
		stopProgress()
		diagnostics.errorf("%v", loadErr)
		return 1
	}
	selectedSources = append([]usage.SourceKind(nil), history.Sources...)
	purposeOptions := func(confirm func(int) bool) purposeClassificationOptions {
		return purposeClassificationOptions{
			Config:  purposeConfig,
			Base:    purposeBase,
			APIKey:  purposeAPIKey,
			Confirm: confirm,
			LoadBodies: func(sessions []usage.Session) (map[string][]string, error) {
				return loadPurposeMessages(loadOptions, sessions)
			},
		}
	}
	if classifyPurposes {
		confirm := func(count int) bool {
			interactive := tui.IsInteractive(os.Stdin, stdout) && tui.IsInteractive(os.Stdin, stderr)
			if !interactive {
				return false
			}
			stopProgress()
			confirmed := confirmPurposeClassification(count, true, os.Stdin, stderr)
			if confirmed {
				stopProgress = progress.Start("Classifying session purposes")
			}
			return confirmed
		}
		if err := applyPurposeClassification(&history.Input, purposeOptions(confirm)); err != nil {
			diagnostics.write("warning", "session purpose classification incomplete: "+err.Error())
		}
	}
	if kind == usageExplorerKind {
		stopProgress()
		inventory, err := discoverSkillInventory([]string(roots))
		if err != nil {
			diagnostics.errorf("scan skill roots: %v", err)
			return 1
		}
		history.SkillInventory = &inventory
		var inventoryWarnings atomic.Bool
		inventoryWarnings.Store(len(inventory.Warnings) > 0)
		filter := query.Filter{Strict: *strict, From: fromDate, To: toDate, Sources: append([]usage.SourceKind(nil), selectedSources...)}
		if len(selectedSources) == 1 {
			filter.Source = selectedSources[0]
		}
		if daysSet {
			filter.From = now.Add(-time.Duration(*days) * 24 * time.Hour)
			filter.To = now
		}
		reload := func() (query.Input, error) {
			reloadNow := time.Now().UTC()
			if daysSet {
				// Keep the originally selected relative window stable while the
				// source snapshot is refreshed.
				reloadNow = filter.To
			}
			loadOptions.Now = reloadNow
			var refreshed loadedHistory
			var err error
			refreshed, err = loadSelectedHistory(loadOptions)
			if err != nil {
				return query.Input{}, err
			}
			if classifyPurposes {
				if err := applyPurposeClassification(&refreshed.Input, purposeOptions(nil)); err != nil {
					diagnostics.write("warning", "session purpose classification incomplete: "+err.Error())
				}
			}
			inventory, err := discoverSkillInventory([]string(roots))
			if err != nil {
				return query.Input{}, fmt.Errorf("scan skill roots: %w", err)
			}
			refreshed.SkillInventory = &inventory
			inventoryWarnings.Store(len(inventory.Warnings) > 0)
			return refreshed.Input, nil
		}
		if err := tui.Run(tui.RunOptions{Input: history.Input, Filter: filter, SourcePath: history.SourcePath, SourcePaths: history.SourcePaths, Reload: reload, Stdin: os.Stdin, Stdout: stdout}); err != nil {
			diagnostics.errorf("run interactive view: %v", err)
			return 1
		}
		if *strictInput && (len(history.Warnings) > 0 || inventoryWarnings.Load()) {
			diagnostics.errorf("input diagnostics encountered (--strict-input)")
			return 1
		}
		return 0
	}
	turns := history.Turns
	warnings := history.Warnings
	agents := history.Agents
	sourcePath := history.SourcePath
	period := formatPeriod(turns)
	periodInfo := formatPeriodInfo(turns, daysSet, *days, fromDate, toDate, now)
	context := output.ReportContext{Source: history.Source, Sources: history.Sources, SourcePath: sourcePath, SourcePaths: history.SourcePaths, Agents: agents, Agent: legacyAgentValue(agents), Period: period, PeriodInfo: periodInfo, Layer: selectedLayer, SkillGroupBy: selectedGroupBy, SkillUsageView: selectedSkillUsageView, Strict: *strict, ReferenceTime: now, Location: time.Local}
	if *unused {
		context.SkillView = output.SkillViewUnused
	}

	activityReport := kind == "activity"
	queryReport := kind == "models" || kind == "sessions" || (kind == "skills" && detailMode)
	var readModel query.ReadModel
	if queryReport || activityReport {
		filter := queryFilterForHistory(history, selectedSources, daysSet, *days, fromDate, toDate, now)
		if kind == "models" && detailMode {
			filter.Model = selectedModel
		}
		if kind == "skills" && detailMode {
			filter.Skill = detailSelector
			filter.Strict = *strict
		}
		readModel = query.Build(query.SanitizeInput(history.Input), filter)
	}
	if activityReport {
		context.Trend = readModel.Overview.Trend
	}
	if activityReport {
		stopProgress()
		if err := output.WriteJSON(stdout, kind, context, aggregate.Report{}); err != nil {
			diagnostics.errorf("render json: %v", err)
			return 1
		}

		writeWarnings(stderr, warnings, *verbose, diagnostics.capabilities)
		if *strictInput && len(warnings) > 0 {
			diagnostics.errorf("input diagnostics encountered (--strict-input)")
			return 1
		}
		return 0
	}
	if queryReport {
		stopProgress()
		detailKey := ""
		switch kind {
		case "models":
			if detailMode {
				detailKey = selectedModel.Key()
				if _, ok := readModel.ModelDetail(detailKey); !ok {
					diagnostics.errorf("model %q was not found in the selected scope", detailSelector)
					return 2
				}
			}
		case "skills":
			if detailMode {
				detailKey = detailSelector
			}
			if _, ok := readModel.SkillDetail(detailKey); !ok {
				diagnostics.errorf("skill %q was not found in the selected scope", detailKey)
				return 2
			}
		case "sessions":
			if detailMode {
				selected, err := selectSession(readModel, detailSelector)
				if err != nil {
					diagnostics.errorf("%v", err)
					return 2
				}
				detailKey = selected.Key
			}
		}
		if err := output.WriteQueryJSON(stdout, kind, context, readModel, detailKey); err != nil {
			diagnostics.errorf("render json: %v", err)
			return 1
		}

		writeWarnings(stderr, warnings, *verbose, diagnostics.capabilities)
		if *strictInput && len(warnings) > 0 {
			diagnostics.errorf("input diagnostics encountered (--strict-input)")
			return 1
		}
		return 0
	}
	aggregateInput := aggregate.Input{Turns: turns, SessionCount: len(history.Sessions), Warnings: warnings, Source: history.Source, Agents: agents}
	report := aggregate.Report{}
	var inventorySnapshot skillinventory.InventorySnapshot
	if *unused {
		stopProgress = startProgressPhase(stopProgress, progress.Start, "Scanning installed skills")
		var err error
		inventorySnapshot, err = discoverSkillInventory([]string(roots))
		if err != nil {
			stopProgress()
			diagnostics.errorf("scan skill roots: %v", err)
			return 1
		}
		report = aggregate.BuildUnusedReport(aggregateInput, inventorySnapshot, *strict, selectedGroupBy)
		warnings = report.Warnings
		stopProgress()
	} else {
		report = aggregate.BuildOverview(aggregateInput)
		if kind == "tools" {
			report.Tools = aggregate.Tools(aggregateInput, selectedLayer)
		}
		if kind == "skills" {
			report.Skills = aggregate.SkillsBy(aggregateInput, *strict, selectedGroupBy)
		}
		stopProgress()
	}
	if *unused {
		context.SkillRoots = append([]string{}, inventorySnapshot.Roots...)
	}
	if err := output.WriteJSON(stdout, kind, context, report); err != nil {
		diagnostics.errorf("render json: %v", err)
		return 1
	}
	writeWarnings(stderr, warnings, *verbose, diagnostics.capabilities)
	if *strictInput && len(warnings) > 0 {
		diagnostics.errorf("input diagnostics encountered (--strict-input)")
		return 1
	}
	return 0
}

type diagnosticWriter struct {
	w            io.Writer
	capabilities output.TerminalCapabilities
	mutex        *sync.Mutex
	progress     func(func())
}

func newDiagnosticWriter(w io.Writer, mode output.ColorMode, noColor bool) diagnosticWriter {
	capabilities := output.TerminalCapabilities{ColorMode: mode, NoColor: noColor}
	if file, ok := w.(*os.File); ok {
		capabilities = output.DetectCapabilities(file, mode, noColor)
	}
	return diagnosticWriter{w: w, capabilities: capabilities, mutex: &sync.Mutex{}}
}

func (d diagnosticWriter) errorf(format string, args ...any) {
	d.write("error", fmt.Sprintf(format, args...))
}

func (d diagnosticWriter) write(level, message string) {
	if d.mutex != nil {
		d.mutex.Lock()
		defer d.mutex.Unlock()
	}
	write := func() {
		_, _ = fmt.Fprintf(d.w, "%s %s\n", output.DiagnosticPrefix(level, d.capabilities), output.DiagnosticMessage(level, message, d.capabilities))
	}
	if d.progress != nil {
		d.progress(write)
		return
	}
	write()
}

func writeWarnings(w io.Writer, warnings []usage.Warning, verbose bool, capabilities ...output.TerminalCapabilities) {
	if len(warnings) == 0 {
		return
	}
	diagnostics := diagnosticWriter{w: w, capabilities: output.TerminalCapabilities{ColorMode: output.ColorNever}}
	if len(capabilities) > 0 {
		diagnostics.capabilities = capabilities[0]
	}
	if !verbose {
		writeWarningSummary(diagnostics, warnings)
		return
	}
	for _, warning := range warnings {
		location := cleanWarningValue(warning.Path)
		if warning.Line > 0 {
			location = fmt.Sprintf("%s:%d", location, warning.Line)
		}
		typeSuffix := ""
		if warning.Type != "" {
			typeSuffix = " type=" + cleanWarningValue(warning.Type)
		}
		count := warning.Count
		if count <= 0 {
			count = 1
		}
		level := warningDiagnosticLevel(warning)
		description := warningDescription(warning.Reason)
		message := ""
		if location == "" {
			message = fmt.Sprintf("%s%s (%s)", description, typeSuffix, formatWarningCount(count))
		} else {
			message = fmt.Sprintf("%s%s at %s (%s)", description, typeSuffix, location, formatWarningCount(count))
		}
		if source := warningSourceLabel(warning.Source); source != "" {
			message = source + " " + message
		}
		if advice := usage.WarningAdvice(warning.Reason); advice != "" {
			message += "; " + advice
		} else {
			message += "; check the affected source or report this warning"
		}
		diagnostics.write(level, message)
	}
}

type warningSummary struct {
	records   int
	readFiles int
	databases int
	files     map[string]struct{}
	sources   map[usage.SourceKind]struct{}
}

func writeWarningSummary(diagnostics diagnosticWriter, warnings []usage.Warning) {
	for _, level := range []string{"warning", "info"} {
		filtered := warningsForDiagnosticLevel(warnings, level)
		if len(filtered) == 0 {
			continue
		}
		writeWarningSummaryForLevel(diagnostics, filtered, level)
	}
}

func writeWarningSummaryForLevel(diagnostics diagnosticWriter, warnings []usage.Warning, level string) {
	summary := summarizeWarnings(warnings)

	fileCount := len(summary.files)
	fileLabel := warningFileLabel(fileCount)
	parts := make([]string, 0, 2)
	switch {
	case summary.records > 0 && summary.readFiles > 0:
		parts = append(parts, fmt.Sprintf("skipped %s %s across %s %s; could not read %s %s", formatWarningCount(summary.records), warningRecordLabel(summary.records), formatWarningCount(fileCount), fileLabel, formatWarningCount(summary.readFiles), warningFileLabel(summary.readFiles)))
	case summary.records > 0:
		parts = append(parts, fmt.Sprintf("skipped %s %s across %s %s", formatWarningCount(summary.records), warningRecordLabel(summary.records), formatWarningCount(fileCount), fileLabel))
	case summary.readFiles > 0:
		parts = append(parts, fmt.Sprintf("could not read %s %s", formatWarningCount(summary.readFiles), warningFileLabel(summary.readFiles)))
	}
	if summary.databases > 0 {
		parts = append(parts, fmt.Sprintf("ignored %s %s", formatWarningCount(summary.databases), warningDatabaseLabel(summary.databases)))
	}
	message := strings.Join(parts, "; ")
	if sources := warningSourceNames(summary.sources); sources != "" {
		message += " from " + sources
	}
	if advice := warningSummaryAdvice(warnings); advice != "" {
		message += "; " + advice
	}
	diagnostics.write(level, message+"; use --verbose to show details")
}

func summarizeWarnings(warnings []usage.Warning) warningSummary {
	summary := warningSummary{files: make(map[string]struct{}), sources: make(map[usage.SourceKind]struct{})}
	for _, warning := range warnings {
		count := warning.Count
		if count <= 0 {
			count = 1
		}
		switch warning.Reason {
		case "read_file":
			summary.readFiles += count
		case opencode.MultipleDatabasesWarningReason:
			summary.databases += count
		default:
			summary.records += count
		}
		if warning.Path != "" && warning.Reason != opencode.MultipleDatabasesWarningReason {
			summary.files[warning.Path] = struct{}{}
		}
		if warning.Source.Valid() {
			summary.sources[warning.Source] = struct{}{}
		}
	}
	return summary
}

func warningSourceLabel(source usage.SourceKind) string {
	if !source.Valid() {
		return ""
	}
	return "[" + usage.AgentDisplayName(string(source)) + "]"
}

func warningSourceNames(sources map[usage.SourceKind]struct{}) string {
	if len(sources) == 0 {
		return ""
	}
	result := make([]string, 0, len(sources))
	for _, source := range usage.AllSourceKinds() {
		if _, ok := sources[source]; ok {
			result = append(result, usage.AgentDisplayName(string(source)))
		}
	}
	return strings.Join(result, ", ")
}

func warningSummaryAdvice(warnings []usage.Warning) string {
	advice := make(map[string]struct{})
	for _, warning := range warnings {
		if value := usage.WarningAdvice(warning.Reason); value != "" {
			advice[value] = struct{}{}
		}
	}
	if len(advice) == 1 {
		for value := range advice {
			return value
		}
	}
	if len(advice) > 1 {
		return "some statistics may be incomplete; review the affected source records"
	}
	return ""
}

func warningsForDiagnosticLevel(warnings []usage.Warning, level string) []usage.Warning {
	filtered := make([]usage.Warning, 0, len(warnings))
	for _, warning := range warnings {
		if warningDiagnosticLevel(warning) == level {
			filtered = append(filtered, warning)
		}
	}
	return filtered
}

func warningDiagnosticLevel(warning usage.Warning) string {
	return usage.WarningDiagnosticLevel(warning)
}

func warningDescription(reason string) string {
	if description := usage.WarningDescription(reason); description != "" {
		return description
	}
	return cleanWarningValue(reason)
}

func warningRecordLabel(count int) string {
	if count == 1 {
		return "record"
	}
	return "records"
}

func warningFileLabel(count int) string {
	if count == 1 {
		return "file"
	}
	return "files"
}

func warningDatabaseLabel(count int) string {
	if count == 1 {
		return "OpenCode database"
	}
	return "OpenCode databases"
}

func formatWarningCount(value int) string {
	if value < 0 {
		return "-" + formatWarningCount(-value)
	}
	text := strconv.Itoa(value)
	for i := len(text) - 3; i > 0; i -= 3 {
		text = text[:i] + "," + text[i:]
	}
	return text
}

func cleanWarningValue(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
}

func discoverSkillInventory(roots []string) (skillinventory.InventorySnapshot, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return skillinventory.InventorySnapshot{}, fmt.Errorf("resolve user home for skill inventory: %w", err)
	}
	resolved, err := skillinventory.ResolveRoots(roots, home)
	if err != nil {
		return skillinventory.InventorySnapshot{}, fmt.Errorf("resolve skill roots: %w", err)
	}
	return skillinventory.Discover(skillinventory.DiscoverOptions{Roots: resolved, AllowMissingRoots: len(roots) == 0})
}
