package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/xkumiyu/agentstats/internal/aggregate"
	"github.com/xkumiyu/agentstats/internal/cache"
	"github.com/xkumiyu/agentstats/internal/codex"
	ctxsource "github.com/xkumiyu/agentstats/internal/ctx"
	"github.com/xkumiyu/agentstats/internal/opencode"
	"github.com/xkumiyu/agentstats/internal/output"
	"github.com/xkumiyu/agentstats/internal/skillinventory"
	"github.com/xkumiyu/agentstats/internal/usage"
	appversion "github.com/xkumiyu/agentstats/internal/version"
)

const usageText = `Usage:
  agentstats <command> [options]
  agentstats --help
  agentstats --version

Commands:
  stats     Show an overview of agent usage
  tools     Show tool usage by canonical name
  skills    Show skill usage and evidence state

Options:
  --help       Show this help
  --version    Show the agentstats version

Run "agentstats <command> --help" for command-specific options.
`

const statsUsageText = `Usage: agentstats stats [options]

Show an overview of agent usage.

Options:
  --source SOURCE   codex, ctx, or opencode (default: codex)
  --days N          Include the last N days (N >= 1; default: all time)
  --from DATE       Include records on or after DATE (YYYY-MM-DD)
  --to DATE         Include records before the day after DATE (YYYY-MM-DD)
  --codex-home PATH Override CODEX_HOME for this invocation (default: CODEX_HOME or ~/.codex)
  --ctx-data-root PATH Read a specific ctx data root (default: ctx default)
  --opencode-home PATH Override OpenCode data root for this invocation (default: XDG_DATA_HOME/opencode or ~/.local/share/opencode)
  --color MODE      auto, always, or never (default: auto; human report only)
  --verbose         Show input and cache diagnostic details
  --strict-input    Exit non-zero when input records are skipped
  --json            Emit JSON
  --help            Show this help
`

const toolsUsageText = `Usage: agentstats tools [options]

Show tool usage by canonical name.

Options:
  --source SOURCE   codex, ctx, or opencode (default: codex)
  --days N          Include the last N days (N >= 1; default: all time)
  --from DATE       Include records on or after DATE (YYYY-MM-DD)
  --to DATE         Include records before the day after DATE (YYYY-MM-DD)
  --codex-home PATH Override CODEX_HOME for this invocation (default: CODEX_HOME or ~/.codex)
  --ctx-data-root PATH Read a specific ctx data root (default: ctx default)
  --opencode-home PATH Override OpenCode data root for this invocation (default: XDG_DATA_HOME/opencode or ~/.local/share/opencode)
  --color MODE      auto, always, or never (default: auto; human report only)
  --layer LAYER     effective, runtime, or model (default: effective)
  --verbose         Show input and cache diagnostic details
  --strict-input    Exit non-zero when input records are skipped
  --json            Emit JSON
  --help            Show this help
`

const skillsUsageText = `Usage: agentstats skills [options]

Show skill usage and evidence state.

Options:
  --source SOURCE   codex, ctx, or opencode (default: codex)
  --days N          Include the last N days (N >= 1; default: all time)
  --from DATE       Include records on or after DATE (YYYY-MM-DD)
  --to DATE         Include records before the day after DATE (YYYY-MM-DD)
  --codex-home PATH Override CODEX_HOME for this invocation (default: CODEX_HOME or ~/.codex)
  --ctx-data-root PATH Read a specific ctx data root (default: ctx default)
  --opencode-home PATH Override OpenCode data root for this invocation (default: XDG_DATA_HOME/opencode or ~/.local/share/opencode)
  --color MODE      auto, always, or never (default: auto; human report only)
  --group-by UNIT   turn or session (default: turn; no effect on --unused)
  --strict          Count confirmed skill evidence only
  --view VIEW       auto, compact, mode, state, or all (default: auto; human report only)
  --unused          Show installed skills with no recorded usage
  --root PATH       Scan a skill root (repeatable; only with --unused; default if omitted: ~/.agents/skills)
  --verbose         Show input and cache diagnostic details
  --strict-input    Exit non-zero when input records are skipped
  --json            Emit JSON
  --help            Show this help
`

const dateLayout = "2006-01-02"

func commandUsage(kind string) string {
	switch kind {
	case "stats":
		return statsUsageText
	case "tools":
		return toolsUsageText
	case "skills":
		return skillsUsageText
	default:
		return usageText
	}
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

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func legacyAgentValue(agents []string) string {
	if len(agents) == 0 {
		return ""
	}
	return strings.Join(agents, ",")
}

type ctxHistoryLoader func(string, ctxsource.IngestOptions) (ctxsource.IngestResult, error)

func run(args []string, stdout, stderr io.Writer) int {
	return runWithCtxLoader(args, stdout, stderr, ctxsource.Load)
}

func runWithCtxLoader(args []string, stdout, stderr io.Writer, loadCtx ctxHistoryLoader) int {
	_, noColor := os.LookupEnv("NO_COLOR")
	diagnostics := newDiagnosticWriter(stderr, output.ColorAuto, noColor)
	if len(args) == 0 {
		_, _ = io.WriteString(stderr, usageText)
		return 2
	}
	kind := strings.ToLower(args[0])
	if kind == "help" || kind == "--help" || kind == "-h" {
		if kind == "help" && len(args) > 1 {
			requested := strings.ToLower(args[1])
			if requested == "stats" || requested == "tools" || requested == "skills" {
				_, _ = io.WriteString(stdout, commandUsage(requested))
				return 0
			}
		}
		_, _ = io.WriteString(stdout, usageText)
		return 0
	}
	if kind == "--version" {
		_, _ = fmt.Fprintf(stdout, "agentstats %s\n", appversion.String())
		return 0
	}
	if kind != "stats" && kind != "tools" && kind != "skills" {
		diagnostics.errorf("unknown command %q", args[0])
		_, _ = io.WriteString(stderr, "\n"+usageText)
		return 2
	}
	if hasOption(args[1:], "--help") || hasOption(args[1:], "-h") {
		_, _ = io.WriteString(stdout, commandUsage(kind))
		return 0
	}
	if hasOption(args[1:], "--version") {
		diagnostics.errorf("--version is a top-level option; use agentstats --version")
		return 2
	}
	if kind != "skills" && hasOption(args[1:], "--group-by") {
		diagnostics.errorf("--group-by is only valid for skills")
		return 2
	}

	flags := flag.NewFlagSet(kind, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { _, _ = fmt.Fprint(stderr, commandUsage(kind)) }
	source := flags.String("source", string(usage.SourceCodex), "history source")
	days := flags.Int("days", 0, "include the last N days")
	from := flags.String("from", "", "include records on or after date")
	to := flags.String("to", "", "include records through date")
	codexHome := flags.String("codex-home", "", "override CODEX_HOME for this invocation")
	ctxDataRoot := flags.String("ctx-data-root", "", "ctx data root path")
	opencodeHome := flags.String("opencode-home", "", "override OpenCode data root for this invocation")
	color := flags.String("color", string(output.ColorAuto), "human report color mode")
	layer := flags.String("layer", string(usage.LayerEffective), "tool layer")
	var groupBy *string
	if kind == "skills" {
		groupBy = flags.String("group-by", string(aggregate.SkillGroupByTurn), "skill aggregation unit")
	}
	strict := flags.Bool("strict", false, "count confirmed skills only")
	view := flags.String("view", string(output.SkillUsageViewAuto), "skill report view")
	unused := flags.Bool("unused", false, "show installed skills with no recorded usage")
	var roots stringList
	flags.Var(&roots, "root", "scan a skill root (repeatable; only with --unused)")
	verbose := flags.Bool("verbose", false, "show input and cache diagnostic details")
	strictInput := flags.Bool("strict-input", false, "exit non-zero when input records are skipped")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if extra := flags.Args(); len(extra) > 0 {
		diagnostics.errorf("unexpected argument %q", extra[0])
		return 2
	}
	selectedSource := usage.SourceKind(strings.ToLower(strings.TrimSpace(*source)))
	if !selectedSource.Valid() {
		diagnostics.errorf("invalid --source %q (want codex, ctx, or opencode)", *source)
		return 2
	}
	codexHomeSet := false
	ctxDataRootSet := false
	opencodeHomeSet := false
	flags.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "codex-home":
			codexHomeSet = true
		case "ctx-data-root":
			ctxDataRootSet = true
		case "opencode-home":
			opencodeHomeSet = true
		}
	})
	if selectedSource != usage.SourceCodex && codexHomeSet {
		diagnostics.errorf("--codex-home is only valid for codex source")
		return 2
	}
	if selectedSource != usage.SourceCtx && ctxDataRootSet {
		diagnostics.errorf("--ctx-data-root is only valid for ctx source")
		return 2
	}
	if selectedSource != usage.SourceOpenCode && opencodeHomeSet {
		diagnostics.errorf("--opencode-home is only valid for opencode source")
		return 2
	}
	mode := output.ColorMode(*color)
	diagnostics = newDiagnosticWriter(stderr, mode, noColor)
	if *jsonOutput {
		diagnostics = newDiagnosticWriter(stderr, output.ColorNever, noColor)
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
	if kind != "skills" && *strict {
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
	if kind != "skills" && *unused {
		_, _ = fmt.Fprintln(stderr, "error: --unused is only valid for skills")
		return 2
	}
	if len(roots) > 0 && (kind != "skills" || !*unused) {
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

	now := time.Now().UTC()
	cacheDir, _ := cache.DefaultDir()
	progress := newSpinner(stderr, !*jsonOutput && !*verbose && diagnostics.capabilities.IsTTY, diagnostics.capabilities.ColorsEnabled())
	var (
		turns        []usage.Turn
		sessionCount int
		warnings     []usage.Warning
		agents       []string
		sourcePath   string
		stopProgress func()
	)
	switch selectedSource {
	case usage.SourceCtx:
		sourcePath = strings.TrimSpace(*ctxDataRoot)
		stopProgress = progress.Start("Reading ctx history")
		ctxOptions := ctxsource.IngestOptions{DataRoot: *ctxDataRoot, Days: *days, DaysSet: daysSet, From: fromDate, To: toDate, Now: now, CacheDir: cacheDir}
		if *verbose {
			ctxOptions.Diagnostic = func(message string) {
				diagnostics.write("debug", message)
			}
		}
		input, loadErr := loadCtx(*ctxDataRoot, ctxOptions)
		if loadErr != nil {
			stopProgress()
			diagnostics.errorf("read ctx history: %v", loadErr)
			return 1
		}
		turns, sessionCount, warnings, agents = input.Turns, len(input.Sessions), input.Warnings, input.Agents
	case usage.SourceOpenCode:
		home, resolveErr := opencode.ResolveHome(*opencodeHome)
		if resolveErr != nil {
			diagnostics.errorf("resolve OpenCode data root: %v", resolveErr)
			return 1
		}
		sourcePath = home
		stopProgress = progress.Start("Reading OpenCode history")
		opencodeOptions := opencode.IngestOptions{Days: *days, DaysSet: daysSet, From: fromDate, To: toDate, Now: now, CacheDir: cacheDir}
		if *verbose {
			opencodeOptions.Diagnostic = func(message string) {
				diagnostics.write("debug", message)
			}
		}
		input, loadErr := opencode.Load(home, opencodeOptions)
		if loadErr != nil {
			stopProgress()
			diagnostics.errorf("read OpenCode history %q: %v", home, loadErr)
			return 1
		}
		turns, sessionCount, warnings, agents = input.Turns, len(input.Sessions), input.Warnings, input.Agents
	case usage.SourceCodex:
		home, resolveErr := codex.ResolveHome(*codexHome)
		if resolveErr != nil {
			diagnostics.errorf("resolve Codex home: %v", resolveErr)
			return 1
		}
		sourcePath = home
		stopProgress = progress.Start("Reading Codex history")
		input, loadErr := codex.Load(home, codex.IngestOptions{Days: *days, DaysSet: daysSet, From: fromDate, To: toDate, Now: now, CacheDir: cacheDir})
		if loadErr != nil {
			stopProgress()
			diagnostics.errorf("read Codex history %q: %v", home, loadErr)
			return 1
		}
		turns, sessionCount, warnings = input.Turns, len(input.Sessions), input.Warnings
		agents = []string{"codex"}
	}
	aggregateInput := aggregate.Input{Turns: turns, SessionCount: sessionCount, Warnings: warnings, Source: selectedSource, Agents: agents}
	report := aggregate.Report{}
	var inventorySnapshot skillinventory.InventorySnapshot
	if *unused {
		stopProgress = startProgressPhase(stopProgress, progress.Start, "Scanning installed skills")
		userHome, err := os.UserHomeDir()
		if err != nil {
			stopProgress()
			_, _ = fmt.Fprintf(stderr, "error: resolve user home for skill inventory: %v\n", err)
			return 1
		}
		resolvedRoots, err := skillinventory.ResolveRoots([]string(roots), userHome)
		if err != nil {
			stopProgress()
			_, _ = fmt.Fprintf(stderr, "error: resolve skill roots: %v\n", err)
			return 1
		}
		inventorySnapshot, err = skillinventory.Discover(skillinventory.DiscoverOptions{
			Roots:             resolvedRoots,
			AllowMissingRoots: len(roots) == 0,
		})
		if err != nil {
			stopProgress()
			_, _ = fmt.Fprintf(stderr, "error: scan skill roots: %v\n", err)
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
	period := formatPeriod(turns)
	periodInfo := formatPeriodInfo(turns, daysSet, *days, fromDate, toDate, now)
	context := output.ReportContext{Source: selectedSource, SourcePath: sourcePath, Agents: agents, Agent: legacyAgentValue(agents), Period: period, PeriodInfo: periodInfo, Layer: selectedLayer, SkillGroupBy: selectedGroupBy, SkillUsageView: selectedSkillUsageView, Strict: *strict, ReferenceTime: now, Location: time.Local}
	if *unused {
		context.SkillView = output.SkillViewUnused
		context.SkillRoots = append([]string{}, inventorySnapshot.Roots...)
	}
	if *jsonOutput {
		if err := output.WriteJSON(stdout, kind, context, report); err != nil {
			diagnostics.errorf("render json: %v", err)
			return 1
		}
	} else {
		capabilities := output.TerminalCapabilities{ColorMode: mode, NoColor: noColor}
		if file, ok := stdout.(*os.File); ok {
			capabilities = output.DetectCapabilities(file, mode, capabilities.NoColor)
		}
		text := output.RenderHuman(kind, context, report, capabilities)
		if _, err := io.WriteString(stdout, text); err != nil {
			diagnostics.errorf("write report: %v", err)
			return 1
		}
	}
	if !*jsonOutput && len(warnings) > 0 {
		_, _ = io.WriteString(stderr, "\n")
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
}

func newDiagnosticWriter(w io.Writer, mode output.ColorMode, noColor bool) diagnosticWriter {
	capabilities := output.TerminalCapabilities{ColorMode: mode, NoColor: noColor}
	if file, ok := w.(*os.File); ok {
		capabilities = output.DetectCapabilities(file, mode, noColor)
	}
	return diagnosticWriter{w: w, capabilities: capabilities}
}

func (d diagnosticWriter) errorf(format string, args ...any) {
	d.write("error", fmt.Sprintf(format, args...))
}

func (d diagnosticWriter) write(level, message string) {
	_, _ = fmt.Fprintf(d.w, "%s %s\n", output.DiagnosticPrefix(level, d.capabilities), output.DiagnosticMessage(level, message, d.capabilities))
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
		if location == "" {
			diagnostics.write(level, fmt.Sprintf("%s%s (%s)", description, typeSuffix, formatWarningCount(count)))
		} else {
			diagnostics.write(level, fmt.Sprintf("%s%s at %s (%s)", description, typeSuffix, location, formatWarningCount(count)))
		}
	}
}

type warningSummary struct {
	records   int
	readFiles int
	files     map[string]struct{}
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
	message := ""
	switch {
	case summary.records > 0 && summary.readFiles > 0:
		message = fmt.Sprintf("skipped %s %s across %s %s; could not read %s %s", formatWarningCount(summary.records), warningRecordLabel(summary.records), formatWarningCount(fileCount), fileLabel, formatWarningCount(summary.readFiles), warningFileLabel(summary.readFiles))
	case summary.readFiles > 0:
		message = fmt.Sprintf("could not read %s %s", formatWarningCount(summary.readFiles), warningFileLabel(summary.readFiles))
	default:
		message = fmt.Sprintf("skipped %s %s across %s %s", formatWarningCount(summary.records), warningRecordLabel(summary.records), formatWarningCount(fileCount), fileLabel)
	}
	diagnostics.write(level, message+"; use --verbose to show details")
}

func summarizeWarnings(warnings []usage.Warning) warningSummary {
	summary := warningSummary{files: make(map[string]struct{})}
	for _, warning := range warnings {
		count := warning.Count
		if count <= 0 {
			count = 1
		}
		if warning.Reason == "read_file" {
			summary.readFiles += count
		} else {
			summary.records += count
		}
		if warning.Path != "" {
			summary.files[warning.Path] = struct{}{}
		}
	}
	return summary
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
	switch strings.TrimSpace(warning.Reason) {
	case "large_line", "empty_line":
		return "info"
	default:
		return "warning"
	}
}

func warningDescription(reason string) string {
	switch strings.TrimSpace(reason) {
	case "large_line":
		return "skipped oversized history record"
	case "empty_line":
		return "skipped empty history line"
	case "malformed_json":
		return "skipped malformed JSON record"
	case "ctx_malformed_json":
		return "skipped malformed ctx event record"
	case "ctx_invalid_event":
		return "skipped invalid ctx event"
	case "ctx_unknown_record":
		return "skipped unknown ctx stream record"
	case "ctx_unknown_event":
		return "skipped unknown ctx event"
	case "ctx_invalid_timestamp":
		return "ctx event has an invalid timestamp"
	case "ctx_missing_agent":
		return "ctx event has no agent identity"
	case "unknown_type":
		return "skipped unknown record type"
	case "invalid_timestamp":
		return "record has an invalid timestamp"
	case "read_file":
		return "could not read file"
	default:
		return cleanWarningValue(reason)
	}
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
