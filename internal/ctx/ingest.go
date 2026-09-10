// Package ctx reads ctx's public, read-only event stream.
package ctx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xkumiyu/catsift/internal/cache"
	"github.com/xkumiyu/catsift/internal/usage"
)

const (
	pageLimit        = 100_000
	probePageLimit   = 1
	ctxParserVersion = "ctx-normalizer-v1"
)

// CommandResult is the result of one ctx invocation. The runner is injectable
// so tests can exercise the public stream contract without a real ctx store.
type CommandResult struct {
	Stdout     []byte
	StdoutPath string
	Stderr     []byte
	ExitCode   int
}

type CommandRunner func(args []string) (CommandResult, error)

type IngestOptions struct {
	DataRoot string
	Days     int
	DaysSet  bool
	From     time.Time
	To       time.Time
	Now      time.Time
	Runner   CommandRunner
	CacheDir string
	// Diagnostic receives optional verbose cache and source status messages.
	Diagnostic func(string)
}

type IngestResult struct {
	Turns    []usage.Turn
	Sessions []SessionMetadata
	Agents   []string
	Warnings []usage.Warning
}

type SessionMetadata struct {
	ID                string          `json:"id"`
	Agent             string          `json:"agent"`
	Provider          string          `json:"provider,omitempty"`
	ProviderSessionID string          `json:"provider_session_id,omitempty"`
	CtxSessionID      string          `json:"ctx_session_id,omitempty"`
	Source            usage.SourceRef `json:"source"`
}

type event struct {
	PayloadValues     []any
	Text              string
	SkillTexts        []string
	SelectedSkill     bool
	TurnIDValue       string
	ID                string
	CtxSessionID      string
	Provider          string
	ProviderSessionID string
	EventType         string
	Role              string
	Timestamp         time.Time
	TimestampInvalid  bool
	Sequence          int64
	Ordinal           int64
	Line              int
}

type page struct {
	NextCursor   string
	Terminal     bool
	Truncated    bool
	GenerationID string
	Complete     bool
	Warnings     []usage.Warning
}

// Load enumerates all ctx pages and converts the selected events into the
// common usage model. It never opens ctx storage itself.
func Load(dataRoot string, options IngestOptions) (IngestResult, error) {
	if strings.TrimSpace(options.DataRoot) == "" {
		options.DataRoot = dataRoot
	}
	runner := options.Runner
	if runner == nil {
		runner = runCommand
	}
	if strings.TrimSpace(options.CacheDir) != "" {
		return loadCached(options, runner)
	}
	options.diagnostic("ctx cache: disabled; reading source")
	return load(options, runner)
}

func (options IngestOptions) diagnostic(message string) {
	if options.Diagnostic != nil {
		options.Diagnostic(message)
	}
}

func load(options IngestOptions, runner CommandRunner) (IngestResult, error) {
	result, _, err := loadStream(options, runner, true)
	return result, err
}

func loadStream(options IngestOptions, runner CommandRunner, includeRange bool) (IngestResult, string, error) {
	now := options.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cutoff, until, err := timeBounds(options, now)
	if err != nil {
		return IngestResult{}, "", err
	}
	assembler := newAssembler(nil)
	seenEvents := make(map[string]struct{})
	previousCursor := ""
	seenCursors := make(map[string]struct{})
	generation := ""
	for {
		args := eventQueryArgs(options, now, pageLimit, includeRange, previousCursor, "full")
		result, err := runner(args)
		if err != nil {
			return IngestResult{}, "", commandError(result, err)
		}
		if result.ExitCode != 0 {
			return IngestResult{}, "", commandError(result, fmt.Errorf("exit code %d", result.ExitCode))
		}
		current, err := parseCommandPage(result, func(item event) {
			if includeRange && !accept(item.Timestamp, cutoff, until) {
				return
			}
			if item.ID != "" {
				if _, ok := seenEvents[item.ID]; ok {
					return
				}
				seenEvents[item.ID] = struct{}{}
			}
			assembler.consume(item)
		})
		if err != nil {
			return IngestResult{}, "", err
		}
		assembler.warnings = append(assembler.warnings, current.Warnings...)
		if current.GenerationID != "" {
			if generation != "" && generation != current.GenerationID {
				return IngestResult{}, "", fmt.Errorf("ctx event stream changed generation from %q to %q", generation, current.GenerationID)
			}
			generation = current.GenerationID
		}
		if current.NextCursor == "" {
			if !current.Terminal || current.Truncated {
				return IngestResult{}, "", errors.New("ctx event stream ended without terminal completion")
			}
			break
		}
		if current.NextCursor == previousCursor {
			return IngestResult{}, "", errors.New("ctx event stream returned an unchanged cursor")
		}
		if _, ok := seenCursors[current.NextCursor]; ok {
			return IngestResult{}, "", errors.New("ctx event stream returned a repeated cursor")
		}
		seenCursors[current.NextCursor] = struct{}{}
		previousCursor = current.NextCursor
	}
	assembler.flush()
	return assembler.result(), generation, nil
}

func eventQueryArgs(options IngestOptions, now time.Time, limit int, includeRange bool, cursor, content string) []string {
	args := []string{"list", "events", "--content", content, "--format", "jsonl", "--quiet", "--limit", strconv.Itoa(limit)}
	if strings.TrimSpace(options.DataRoot) != "" {
		args = append(args, "--data-root", options.DataRoot)
	}
	if includeRange && hasTimeRange(options) {
		cutoff, until, _ := timeBounds(options, now)
		// ctx requires --since and --until as a pair. An open-ended --from
		// ends at now; an open-ended --to remains a local filter.
		if until.IsZero() && !cutoff.IsZero() {
			until = now
		}
		if !cutoff.IsZero() && !until.IsZero() {
			args = append(args, "--since", cutoff.UTC().Truncate(time.Millisecond).Format(time.RFC3339Nano))
			args = append(args, "--until", until.UTC().Truncate(time.Millisecond).Format(time.RFC3339Nano))
		}
	}
	if cursor != "" {
		args = append(args, "--cursor", cursor)
	}
	return args
}

func loadCached(options IngestOptions, runner CommandRunner) (IngestResult, error) {
	now := options.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if _, _, err := timeBounds(options, now); err != nil {
		return IngestResult{}, err
	}
	scope := canonicalDataRoot(options.DataRoot)
	store := cache.New(options.CacheDir)
	options.diagnostic("ctx cache: checking generation")
	generation, probeErr := probeGeneration(options, runner, now)
	if generation != "" {
		data, hit, _ := store.Read("ctx", scope, generation, ctxParserVersion)
		if hit {
			var snapshot cache.Snapshot
			if err := json.Unmarshal(data, &snapshot); err == nil {
				if hasTimeRange(options) {
					options.diagnostic("ctx cache: hit; applying selected period locally")
				} else {
					options.diagnostic("ctx cache: hit; using complete cached history")
				}
				return filterCachedResult(resultFromSnapshot(snapshot), options, now), nil
			}
		}
	}
	if probeErr != nil {
		options.diagnostic("ctx cache: generation check unavailable; reading source")
	} else {
		options.diagnostic("ctx cache: miss; reading source")
	}
	fullOptions := options
	fullOptions.Days = 0
	fullOptions.DaysSet = false
	if hasTimeRange(options) {
		options.diagnostic("ctx source: reading full event stream to populate complete cache")
	} else {
		options.diagnostic("ctx source: reading full event stream")
	}
	result, fullGeneration, err := loadStream(fullOptions, runner, false)
	if err != nil {
		return IngestResult{}, err
	}
	if fullGeneration != "" {
		if err := store.Write("ctx", scope, fullGeneration, ctxParserVersion, snapshotFromResult(result)); err != nil {
			options.diagnostic("ctx cache: could not store complete generation; result is still available")
		} else {
			options.diagnostic("ctx cache: stored complete generation")
		}
	} else {
		options.diagnostic("ctx cache: source returned no generation; result is still available")
	}
	return filterCachedResult(result, options, now), nil
}

func probeGeneration(options IngestOptions, runner CommandRunner, now time.Time) (string, error) {
	result, err := runner(eventQueryArgs(options, now, probePageLimit, false, "", "none"))
	if err != nil {
		return "", commandError(result, err)
	}
	if result.ExitCode != 0 {
		return "", commandError(result, fmt.Errorf("exit code %d", result.ExitCode))
	}
	page, err := parseCommandPage(result, func(event) {})
	if err != nil {
		return "", err
	}
	return page.GenerationID, nil
}

func snapshotFromResult(result IngestResult) cache.Snapshot {
	snapshot := cache.Snapshot{
		Agents:   append([]string(nil), result.Agents...),
		Warnings: append([]usage.Warning(nil), result.Warnings...),
	}
	for _, turn := range result.Turns {
		snapshot.Turns = append(snapshot.Turns, cache.TurnFromUsage(turn))
	}
	for _, session := range result.Sessions {
		snapshot.Sessions = append(snapshot.Sessions, cache.Session{
			ID:                session.ID,
			Agent:             session.Agent,
			Provider:          session.Provider,
			ProviderSessionID: session.ProviderSessionID,
			CtxSessionID:      session.CtxSessionID,
			Source:            cache.SourceRefFromUsage(session.Source),
		})
	}
	return snapshot
}

func resultFromSnapshot(snapshot cache.Snapshot) IngestResult {
	result := IngestResult{
		Agents:   append([]string(nil), snapshot.Agents...),
		Warnings: append([]usage.Warning(nil), snapshot.Warnings...),
	}
	for _, turn := range snapshot.Turns {
		result.Turns = append(result.Turns, turn.Usage())
	}
	for _, session := range snapshot.Sessions {
		result.Sessions = append(result.Sessions, SessionMetadata{
			ID:                session.ID,
			Agent:             session.Agent,
			Provider:          session.Provider,
			ProviderSessionID: session.ProviderSessionID,
			CtxSessionID:      session.CtxSessionID,
			Source:            session.Source.Usage(),
		})
	}
	return result
}

func filterCachedResult(result IngestResult, options IngestOptions, now time.Time) IngestResult {
	if !hasTimeRange(options) {
		return result
	}
	cutoff, until, _ := timeBounds(options, now)
	filtered := result
	filtered.Turns = nil
	selectedSessions := make(map[string]struct{})
	selectedAgents := make(map[string]struct{})
	for _, turn := range result.Turns {
		filteredTurn, ok := filterCtxTurn(turn, cutoff, until)
		if !ok {
			continue
		}
		filtered.Turns = append(filtered.Turns, filteredTurn)
		selectedSessions[turn.SessionID] = struct{}{}
		if turn.Source.Agent != "" {
			selectedAgents[turn.Source.Agent] = struct{}{}
		}
	}
	filtered.Sessions = nil
	for _, session := range result.Sessions {
		if _, ok := selectedSessions[session.ID]; ok {
			filtered.Sessions = append(filtered.Sessions, session)
		}
	}
	filtered.Agents = nil
	for _, agent := range result.Agents {
		if _, ok := selectedAgents[agent]; ok {
			filtered.Agents = append(filtered.Agents, agent)
		}
	}
	return filtered
}

func filterCtxTurn(turn usage.Turn, cutoff, until time.Time) (usage.Turn, bool) {
	when := turn.EndedAt
	if when.IsZero() {
		when = turn.StartedAt
	}
	if !accept(when, cutoff, until) {
		return usage.Turn{}, false
	}
	filtered := turn
	if len(turn.UserPromptTimes) > 0 {
		filtered.UserPromptTimes = filtered.UserPromptTimes[:0]
		for _, timestamp := range turn.UserPromptTimes {
			if accept(timestamp, cutoff, until) {
				filtered.UserPromptTimes = append(filtered.UserPromptTimes, timestamp)
			}
		}
		filtered.UserPrompts = len(filtered.UserPromptTimes)
	}
	filtered.ModelTools = filterCtxTools(turn.ModelTools, cutoff, until)
	filtered.RuntimeTools = filterCtxTools(turn.RuntimeTools, cutoff, until)
	filtered.SkillEvidence = filterCtxSkills(turn.SkillEvidence, cutoff, until)
	return filtered, true
}

func filterCtxTools(values []usage.ToolObservation, cutoff, until time.Time) []usage.ToolObservation {
	filtered := values[:0]
	for _, value := range values {
		if accept(value.Timestamp, cutoff, until) {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func filterCtxSkills(values []usage.SkillEvidence, cutoff, until time.Time) []usage.SkillEvidence {
	filtered := values[:0]
	for _, value := range values {
		if accept(value.Timestamp, cutoff, until) {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func canonicalDataRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return "default"
	}
	clean := filepath.Clean(root)
	if absolute, err := filepath.Abs(clean); err == nil {
		return absolute
	}
	return clean
}

func commandError(result CommandResult, err error) error {
	if result.StdoutPath != "" {
		_ = os.Remove(result.StdoutPath)
	}
	message := strings.TrimSpace(string(result.Stderr))
	if message != "" {
		return fmt.Errorf("ctx event enumeration: %w: %s", err, message)
	}
	return fmt.Errorf("ctx event enumeration: %w", err)
}

func runCommand(args []string) (CommandResult, error) {
	command := exec.Command("ctx", args...)
	stdout, err := os.CreateTemp("", ".catsift-ctx-output-*")
	if err != nil {
		return CommandResult{}, fmt.Errorf("create ctx output temporary file: %w", err)
	}
	stdoutPath := stdout.Name()
	if err := stdout.Chmod(0o600); err != nil {
		_ = os.Remove(stdoutPath)
		_ = stdout.Close()
		return CommandResult{}, fmt.Errorf("secure ctx output temporary file: %w", err)
	}
	var stderr bytes.Buffer
	command.Stdout = stdout
	command.Stderr = &stderr
	runErr := command.Run()
	closeErr := stdout.Close()
	code := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			code = exitErr.ExitCode()
		}
	}
	if closeErr != nil && runErr == nil {
		runErr = closeErr
	}
	if runErr != nil {
		_ = os.Remove(stdoutPath)
		return CommandResult{Stderr: stderr.Bytes(), ExitCode: code}, runErr
	}
	return CommandResult{StdoutPath: stdoutPath, Stderr: stderr.Bytes(), ExitCode: code}, nil
}

func parseCommandPage(result CommandResult, consume func(event)) (page, error) {
	if result.StdoutPath == "" {
		return parsePageReader(bytes.NewReader(result.Stdout), consume)
	}
	file, err := os.Open(result.StdoutPath)
	if err != nil {
		_ = os.Remove(result.StdoutPath)
		return page{}, fmt.Errorf("open ctx event page: %w", err)
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(result.StdoutPath)
	}()
	return parsePageReader(file, consume)
}

func parsePageReader(input io.Reader, consume func(event)) (page, error) {
	result := page{}
	reader := bufio.NewReaderSize(input, 64<<10)
	lineNo := 0
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			lineNo++
			var raw map[string]any
			if err := json.Unmarshal(bytes.TrimSpace(line), &raw); err != nil {
				result.Warnings = append(result.Warnings, warning("ctx_malformed_json", "", lineNo))
			} else {
				switch strings.ToLower(strings.TrimSpace(stringValue(raw, "record_type"))) {
				case "event_range_event":
					itemRaw := raw["event"]
					item, ok := itemRaw.(map[string]any)
					if !ok {
						result.Warnings = append(result.Warnings, warning("ctx_invalid_event", "", lineNo))
					} else {
						event := eventFromMap(item, lineNo)
						if event.TimestampInvalid {
							result.Warnings = append(result.Warnings, warning("ctx_invalid_timestamp", event.EventType, lineNo))
						}
						if consume != nil {
							consume(event)
						}
					}
				case "event_range_completion":
					if result.Complete {
						return page{}, errors.New("ctx event stream returned multiple completion records")
					}
					result.Complete = true
					result.NextCursor = stringValue(raw, "next_cursor")
					result.Terminal = boolValue(raw, "terminal")
					result.Truncated = boolValue(raw, "truncated")
					result.GenerationID = stringValue(raw, "generation_id")
				default:
					result.Warnings = append(result.Warnings, warning("ctx_unknown_record", stringValue(raw, "record_type"), lineNo))
				}
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return page{}, fmt.Errorf("read ctx event page: %w", readErr)
		}
	}
	if !result.Complete {
		return page{}, errors.New("ctx event stream has no completion record")
	}
	return result, nil
}

func eventFromMap(raw map[string]any, line int) event {
	source := mapValue(raw, "source")
	provider := firstString(raw, "provider", "provider_name", "agent", "agent_name", "agent_id")
	if provider == "" {
		provider = firstString(source, "provider")
	}
	timestamp, timestampInvalid := eventTimestamp(raw)
	item := event{
		ID:                firstString(raw, "ctx_event_id", "event_id"),
		CtxSessionID:      firstString(raw, "ctx_session_id", "session_uuid"),
		Provider:          provider,
		ProviderSessionID: firstString(raw, "provider_session_id", "provider_session", "session_id"),
		EventType:         firstString(raw, "event_type", "type"),
		Role:              strings.ToLower(strings.TrimSpace(firstString(raw, "role", "author"))),
		Timestamp:         timestamp,
		TimestampInvalid:  timestampInvalid,
		Sequence:          integerValue(raw, "sequence", "event_seq"),
		Ordinal:           integerValue(raw, "ordinal"),
		Line:              line,
	}
	item.PayloadValues = eventPayloadValues(raw)
	item.Text = eventTextValues(item.PayloadValues)
	item.SkillTexts = eventSkillTextsValues(item.PayloadValues)
	item.SelectedSkill = hasSelectedSkillInstructionsValues(item.PayloadValues)
	item.TurnIDValue = turnIDFromValues(item.PayloadValues)
	return item
}

func eventTimestamp(raw map[string]any) (time.Time, bool) {
	if value := firstString(raw, "occurred_at", "timestamp", "time"); value != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return parsed, false
		}
		if value, ok := numberValue(raw["occurred_at_ms"]); ok && value != 0 {
			return time.UnixMilli(int64(value)), true
		}
		return time.Time{}, true
	}
	if value, ok := numberValue(raw["occurred_at_ms"]); ok && value != 0 {
		return time.UnixMilli(int64(value)), false
	}
	return time.Time{}, false
}

func hasTimeRange(options IngestOptions) bool {
	return options.DaysSet || options.Days != 0 || !options.From.IsZero() || !options.To.IsZero()
}

func timeBounds(options IngestOptions, now time.Time) (time.Time, time.Time, error) {
	hasDays := options.DaysSet || options.Days != 0
	hasRange := !options.From.IsZero() || !options.To.IsZero()
	if hasDays && hasRange {
		return time.Time{}, time.Time{}, errors.New("days cannot be combined with from or to")
	}
	if hasDays {
		if options.Days <= 0 {
			return time.Time{}, time.Time{}, errors.New("days must be at least 1")
		}
		return now.Add(-time.Duration(options.Days) * 24 * time.Hour), now, nil
	}
	if !options.From.IsZero() && !options.To.IsZero() && !options.From.Before(options.To) {
		return time.Time{}, time.Time{}, errors.New("from must be before to")
	}
	return options.From, options.To, nil
}

func accept(timestamp, cutoff, until time.Time) bool {
	if !cutoff.IsZero() && (timestamp.IsZero() || timestamp.Before(cutoff)) {
		return false
	}
	return until.IsZero() || timestamp.IsZero() || timestamp.Before(until)
}

func warning(reason, typ string, line int) usage.Warning {
	return usage.Warning{Reason: reason, Type: strings.TrimSpace(typ), Path: "ctx", Line: line, Count: 1}
}

type assembler struct {
	warnings []usage.Warning
	turns    []usage.Turn
	current  map[string]*turnState
	ordinals map[string]int
	sessions map[string]SessionMetadata
	agents   map[string]struct{}
}

type turnState struct {
	turn       usage.Turn
	lastTime   time.Time
	explicitID bool
}

func newAssembler(warnings []usage.Warning) *assembler {
	return &assembler{
		warnings: append([]usage.Warning(nil), warnings...),
		current:  make(map[string]*turnState),
		ordinals: make(map[string]int),
		sessions: make(map[string]SessionMetadata),
		agents:   make(map[string]struct{}),
	}
}

func (a *assembler) consume(item event) {
	if isNonUsageEvent(item) {
		return
	}
	sessionID := item.sessionID()
	source := item.source()
	agent := source.Agent
	if !recognized(item) {
		if agent == "unknown" {
			a.warnings = append(a.warnings, warning("ctx_missing_agent", item.EventType, item.Line))
		}
		a.warnings = append(a.warnings, warning("ctx_unknown_event", item.EventType, item.Line))
		return
	}
	a.agents[agent] = struct{}{}
	if agent == "unknown" {
		a.warnings = append(a.warnings, warning("ctx_missing_agent", item.EventType, item.Line))
	}
	if _, ok := a.sessions[sessionID]; !ok {
		a.sessions[sessionID] = SessionMetadata{
			ID:                sessionID,
			Agent:             agent,
			Provider:          item.Provider,
			ProviderSessionID: item.ProviderSessionID,
			CtxSessionID:      item.CtxSessionID,
			Source:            source,
		}
	}
	turnID := item.turnID()
	if isUserMessage(item) {
		if current := a.current[sessionID]; current != nil && current.turn.UserPrompts > 0 && (turnID == "" || current.turn.ID != turnID) {
			a.finish(sessionID)
		}
	}
	current := a.ensure(sessionID, turnID, item)
	if !item.Timestamp.IsZero() {
		if current.turn.StartedAt.IsZero() || item.Timestamp.Before(current.turn.StartedAt) {
			current.turn.StartedAt = item.Timestamp
		}
		current.lastTime = item.Timestamp
	}
	a.observe(current, item)
	if isTurnComplete(item) {
		current.turn.EndedAt = item.Timestamp
		a.finish(sessionID)
	}
}

func (a *assembler) ensure(sessionID, explicitID string, item event) *turnState {
	if current := a.current[sessionID]; current != nil {
		if explicitID == "" || explicitID == current.turn.ID {
			return current
		}
		if !current.explicitID {
			current.adoptID(explicitID)
			return current
		}
		a.finish(sessionID)
	}
	a.ordinals[sessionID]++
	id := explicitID
	if id == "" {
		id = strconv.Itoa(a.ordinals[sessionID])
	}
	turn := usage.NewTurn(sessionID, id, a.ordinals[sessionID], item.source())
	a.current[sessionID] = &turnState{turn: turn, explicitID: explicitID != ""}
	return a.current[sessionID]
}

func (s *turnState) adoptID(id string) {
	if id == "" || s.turn.ID == id {
		return
	}
	s.turn.ID = id
	s.explicitID = true
	for i := range s.turn.ModelTools {
		s.turn.ModelTools[i].TurnID = id
	}
	for i := range s.turn.RuntimeTools {
		s.turn.RuntimeTools[i].TurnID = id
	}
	for i := range s.turn.SkillEvidence {
		s.turn.SkillEvidence[i].TurnID = id
	}
}

func (a *assembler) finish(sessionID string) {
	current := a.current[sessionID]
	if current == nil {
		return
	}
	if current.turn.EndedAt.IsZero() {
		current.turn.EndedAt = current.lastTime
	}
	a.turns = append(a.turns, current.turn)
	delete(a.current, sessionID)
}

func (a *assembler) flush() {
	ids := make([]string, 0, len(a.current))
	for id := range a.current {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		a.finish(id)
	}
	sort.SliceStable(a.turns, func(i, j int) bool {
		if a.turns[i].StartedAt.Equal(a.turns[j].StartedAt) {
			if a.turns[i].SessionID == a.turns[j].SessionID {
				return a.turns[i].Ordinal < a.turns[j].Ordinal
			}
			return a.turns[i].SessionID < a.turns[j].SessionID
		}
		return a.turns[i].StartedAt.Before(a.turns[j].StartedAt)
	})
}

func (a *assembler) result() IngestResult {
	agents := make([]string, 0, len(a.agents))
	for agent := range a.agents {
		agents = append(agents, agent)
	}
	sort.Strings(agents)
	sessionIDs := make([]string, 0, len(a.sessions))
	for id := range a.sessions {
		sessionIDs = append(sessionIDs, id)
	}
	sort.Strings(sessionIDs)
	sessions := make([]SessionMetadata, 0, len(sessionIDs))
	for _, id := range sessionIDs {
		sessions = append(sessions, a.sessions[id])
	}
	return IngestResult{Turns: a.turns, Sessions: sessions, Agents: agents, Warnings: a.warnings}
}

func (a *assembler) observe(current *turnState, item event) {
	text := item.Text
	if isUserMessage(item) {
		injected := onlyInjectedSkill(text)
		if !injected {
			current.turn.UserPrompts++
			current.turn.UserPromptTimes = append(current.turn.UserPromptTimes, item.Timestamp)
		}
		current.turn.SkillEvidence = append(current.turn.SkillEvidence, usage.DetectExplicitRequest(text, current.turn.SessionID, current.turn.ID, item.Timestamp, item.source())...)
		if item.SelectedSkill {
			for _, skillText := range item.SkillTexts {
				current.turn.SkillEvidence = append(current.turn.SkillEvidence, usage.DetectSelectedSkillInstructions(skillText, current.turn.SessionID, current.turn.ID, item.Timestamp, item.source())...)
			}
		} else {
			for _, skillText := range item.SkillTexts {
				current.turn.SkillEvidence = append(current.turn.SkillEvidence, usage.DetectInjectedSkillsWithMode(skillText, current.turn.SessionID, current.turn.ID, usage.ModeUnknown, item.Timestamp, item.source())...)
			}
		}
		for _, value := range item.PayloadValues {
			current.turn.SkillEvidence = append(current.turn.SkillEvidence, usage.DetectRuntimeSkillItems(value, current.turn.SessionID, current.turn.ID, item.Timestamp, item.source())...)
		}
	}

	tools := eventTools(item, current.turn.SessionID, current.turn.ID)
	seen := make(map[string]struct{}, len(tools))
	for _, observation := range tools {
		key := string(observation.Layer) + "\x00" + observation.CallID + "\x00" + observation.ItemID + "\x00" + observation.CanonicalName
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if observation.Layer == usage.LayerRuntime {
			current.turn.RuntimeTools = append(current.turn.RuntimeTools, observation)
		} else {
			current.turn.ModelTools = append(current.turn.ModelTools, observation)
		}
		current.turn.SkillEvidence = append(current.turn.SkillEvidence, usage.DetectStructuredSkillTool(observation)...)
		if observation.Layer == usage.LayerRuntime && observation.Status != usage.StatusFailure {
			current.turn.SkillEvidence = append(current.turn.SkillEvidence, usage.DetectImplicitAccess(observation.Arguments, current.turn.SessionID, current.turn.ID, item.Timestamp, item.source())...)
		} else if observation.Layer == usage.LayerModel && observation.Status == usage.StatusSuccess && isExecTool(observation) {
			for _, command := range execCommandsFromModelArguments(observation.Arguments) {
				current.turn.SkillEvidence = append(current.turn.SkillEvidence, usage.DetectImplicitAccess(command, current.turn.SessionID, current.turn.ID, item.Timestamp, item.source())...)
			}
		}
	}
	if strings.Contains(compact(item.EventType), "skill") {
		for _, name := range skillActivityNamesValues(item.PayloadValues) {
			current.turn.SkillEvidence = append(current.turn.SkillEvidence, usage.NewSkillEvidence(current.turn.SessionID, current.turn.ID, name, usage.ModeExplicit, usage.MethodStructuredTool, usage.StateConfirmed, item.Timestamp, item.source()))
		}
	}
}

func eventTools(item event, sessionID, turnID string) []usage.ToolObservation {
	result := make([]usage.ToolObservation, 0)
	seen := make(map[string]struct{})
	for _, value := range item.PayloadValues {
		collectActivityTools(value, item, sessionID, turnID, &result, seen)
	}
	if isToolEvent(item) {
		if topLevel := eventTopLevel(item); topLevel != nil {
			if observation, ok := normalizeToolMap(topLevel, item, nil, sessionID, turnID); ok {
				result = append(result, observation)
			}
		}
	}
	return result
}

func eventTopLevel(item event) map[string]any {
	if len(item.PayloadValues) == 0 {
		return nil
	}
	topLevel, _ := item.PayloadValues[0].(map[string]any)
	return topLevel
}

func isExecTool(observation usage.ToolObservation) bool {
	return strings.EqualFold(observation.RawName, "exec") || strings.EqualFold(observation.CanonicalName, "exec")
}

// execCommandsFromModelArguments extracts structured commands from a
// completed Codex exec payload. ctx may preserve the provider-native input as
// a JSON object or as JavaScript that calls tools.exec_command; arbitrary text
// is deliberately not parsed as a command.
func execCommandsFromModelArguments(arguments string) []string {
	arguments = strings.TrimSpace(arguments)
	commands := make([]string, 0, 1)
	seen := make(map[string]struct{})
	appendCommand := func(command string) {
		command = strings.TrimSpace(command)
		if command == "" {
			return
		}
		if _, ok := seen[command]; ok {
			return
		}
		seen[command] = struct{}{}
		commands = append(commands, command)
	}

	var payload map[string]any
	if err := json.NewDecoder(strings.NewReader(arguments)).Decode(&payload); err == nil {
		appendCommand(stringValue(payload, "cmd"))
		appendCommand(stringValue(payload, "command"))
	}
	for _, marker := range []string{"exec_command(", "execCommand("} {
		for offset := 0; offset < len(arguments); {
			relative := strings.Index(arguments[offset:], marker)
			if relative < 0 {
				break
			}
			markerStart := offset + relative
			index := markerStart + len(marker)
			candidate := arguments[index:]
			object, ok := javascriptObjectArgument(candidate)
			if ok {
				if javascriptObjectHasShorthand(object) {
					for _, literal := range javascriptMappedCommandLiterals(arguments, markerStart) {
						appendCommand(literal)
					}
				}
				appendCommand(javascriptObjectCommand(object))
			}
			offset = index
		}
	}
	if len(commands) == 0 {
		appendCommand(javascriptObjectCommand(arguments))
	}
	return commands
}

func javascriptObjectArgument(source string) (string, bool) {
	start := 0
	for start < len(source) && (source[start] == ' ' || source[start] == '\t' || source[start] == '\r' || source[start] == '\n') {
		start++
	}
	if start >= len(source) || source[start] != '{' {
		return "", false
	}
	depth := 0
	for index := start; index < len(source); index++ {
		switch source[index] {
		case '\'', '"', '`':
			_, next, ok := javascriptString(source, index)
			if !ok {
				return "", false
			}
			index = next - 1
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return source[start : index+1], true
			}
		}
	}
	return "", false
}

func javascriptObjectCommand(source string) string {
	for index := 0; index < len(source); {
		switch source[index] {
		case '\'', '"', '`':
			key, next, ok := javascriptString(source, index)
			if !ok {
				return ""
			}
			if key == "cmd" || key == "command" {
				if command, ok := javascriptPropertyString(source, next); ok {
					return command
				}
			}
			index = next
			continue
		}
		if isJavaScriptIdentifierStart(source[index]) {
			start := index
			index++
			for index < len(source) && isJavaScriptIdentifierPart(source[index]) {
				index++
			}
			key := source[start:index]
			if key == "cmd" || key == "command" {
				if command, ok := javascriptPropertyString(source, index); ok {
					return command
				}
			}
			continue
		}
		index++
	}
	return ""
}

func javascriptObjectHasShorthand(source string) bool {
	for index := 0; index < len(source); {
		switch source[index] {
		case '\'', '"', '`':
			_, next, ok := javascriptString(source, index)
			if !ok {
				return false
			}
			index = next
			continue
		}
		if !isJavaScriptIdentifierStart(source[index]) {
			index++
			continue
		}
		start := index
		index++
		for index < len(source) && isJavaScriptIdentifierPart(source[index]) {
			index++
		}
		key := source[start:index]
		if key != "cmd" && key != "command" {
			continue
		}
		for index < len(source) && (source[index] == ' ' || source[index] == '\t' || source[index] == '\r' || source[index] == '\n') {
			index++
		}
		if index < len(source) && (source[index] == ',' || source[index] == '}') {
			return true
		}
	}
	return false
}

func javascriptMappedCommandLiterals(source string, callIndex int) []string {
	if callIndex < 0 || callIndex > len(source) {
		return nil
	}
	before := source[:callIndex]
	mapIndex := strings.LastIndex(before, ".map")
	if mapIndex < 0 {
		return nil
	}
	variable := javascriptIdentifierBefore(before, mapIndex)
	if variable == "" {
		return nil
	}
	declaration := -1
	for _, prefix := range []string{"const ", "let ", "var "} {
		if index := strings.LastIndex(before, prefix+variable); index > declaration {
			declaration = index + len(prefix+variable)
		}
	}
	if declaration < 0 {
		return nil
	}
	index := declaration
	for index < len(before) && (before[index] == ' ' || before[index] == '\t' || before[index] == '\r' || before[index] == '\n') {
		index++
	}
	if index >= len(before) || before[index] != '=' {
		return nil
	}
	index++
	for index < len(before) && (before[index] == ' ' || before[index] == '\t' || before[index] == '\r' || before[index] == '\n') {
		index++
	}
	if index >= len(before) || before[index] != '[' {
		return nil
	}
	array, ok := javascriptArrayArgument(before[index:])
	if !ok {
		return nil
	}
	return javascriptStringLiterals(array)
}

func javascriptIdentifierBefore(source string, end int) string {
	for end > 0 && (source[end-1] == ' ' || source[end-1] == '\t' || source[end-1] == '\r' || source[end-1] == '\n') {
		end--
	}
	start := end
	for start > 0 && isJavaScriptIdentifierPart(source[start-1]) {
		start--
	}
	return source[start:end]
}

func javascriptArrayArgument(source string) (string, bool) {
	start := 0
	for start < len(source) && (source[start] == ' ' || source[start] == '\t' || source[start] == '\r' || source[start] == '\n') {
		start++
	}
	if start >= len(source) || source[start] != '[' {
		return "", false
	}
	depth := 0
	for index := start; index < len(source); index++ {
		switch source[index] {
		case '\'', '"', '`':
			_, next, ok := javascriptString(source, index)
			if !ok {
				return "", false
			}
			index = next - 1
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return source[start : index+1], true
			}
		}
	}
	return "", false
}

func javascriptStringLiterals(source string) []string {
	result := make([]string, 0)
	for index := 0; index < len(source); {
		if source[index] != '\'' && source[index] != '"' && source[index] != '`' {
			index++
			continue
		}
		value, next, ok := javascriptString(source, index)
		if !ok {
			break
		}
		result = append(result, value)
		index = next
	}
	return result
}

func javascriptPropertyString(source string, index int) (string, bool) {
	for index < len(source) && (source[index] == ' ' || source[index] == '\t' || source[index] == '\r' || source[index] == '\n') {
		index++
	}
	if index >= len(source) || source[index] != ':' {
		return "", false
	}
	index++
	for index < len(source) && (source[index] == ' ' || source[index] == '\t' || source[index] == '\r' || source[index] == '\n') {
		index++
	}
	if index >= len(source) || (source[index] != '\'' && source[index] != '"' && source[index] != '`') {
		return "", false
	}
	value, _, ok := javascriptString(source, index)
	return value, ok
}

func javascriptString(source string, start int) (string, int, bool) {
	quote := source[start]
	var value strings.Builder
	for index := start + 1; index < len(source); index++ {
		switch source[index] {
		case quote:
			return value.String(), index + 1, true
		case '\\':
			if index+1 >= len(source) {
				return "", 0, false
			}
			index++
			switch source[index] {
			case 'n':
				value.WriteByte('\n')
			case 'r':
				value.WriteByte('\r')
			case 't':
				value.WriteByte('\t')
			case 'b':
				value.WriteByte('\b')
			case 'f':
				value.WriteByte('\f')
			case 'v':
				value.WriteByte('\v')
			case '0':
				value.WriteByte('\x00')
			case 'x':
				if index+2 >= len(source) {
					return "", 0, false
				}
				parsed, err := strconv.ParseUint(source[index+1:index+3], 16, 8)
				if err != nil {
					return "", 0, false
				}
				value.WriteByte(byte(parsed))
				index += 2
			case 'u':
				if index+4 >= len(source) {
					return "", 0, false
				}
				parsed, err := strconv.ParseUint(source[index+1:index+5], 16, 16)
				if err != nil {
					return "", 0, false
				}
				value.WriteRune(rune(parsed))
				index += 4
			default:
				value.WriteByte(source[index])
			}
		default:
			value.WriteByte(source[index])
		}
	}
	return "", 0, false
}

func isJavaScriptIdentifierStart(value byte) bool {
	return value == '_' || value == '$' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func isJavaScriptIdentifierPart(value byte) bool {
	return isJavaScriptIdentifierStart(value) || value >= '0' && value <= '9'
}

func collectActivityTools(value any, item event, sessionID, turnID string, result *[]usage.ToolObservation, seen map[string]struct{}) {
	switch typed := value.(type) {
	case []any:
		for _, child := range typed {
			collectActivityTools(child, item, sessionID, turnID, result, seen)
		}
	case map[string]any:
		if invocation, ok := typed["invocation"].(map[string]any); ok {
			merged := copyMap(invocation)
			if completion, ok := typed["result"].(map[string]any); ok {
				mergeMap(merged, completion)
			}
			if observation, ok := normalizeToolMap(merged, item, typed, sessionID, turnID); ok {
				appendUniqueTool(result, seen, observation)
			}
		}
		if isToolLikeMap(typed) {
			if observation, ok := normalizeToolMap(typed, item, nil, sessionID, turnID); ok {
				appendUniqueTool(result, seen, observation)
			}
		}
		for key, child := range typed {
			if key == "result" || key == "invocation" {
				continue
			}
			collectActivityTools(child, item, sessionID, turnID, result, seen)
		}
	}
}

func appendUniqueTool(result *[]usage.ToolObservation, seen map[string]struct{}, observation usage.ToolObservation) {
	key := string(observation.Layer) + "\x00" + observation.CallID + "\x00" + observation.ItemID + "\x00" + observation.CanonicalName
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	*result = append(*result, observation)
}

func normalizeToolMap(raw map[string]any, item event, parent map[string]any, sessionID, turnID string) (usage.ToolObservation, bool) {
	value := copyMap(raw)
	kind := compact(item.EventType)
	valueKind := compact(firstString(value, "type", "kind", "activity_type"))
	layer := usage.LayerModel
	if runtimeEventKind(kind) || runtimeEventKind(valueKind) {
		layer = usage.LayerRuntime
	}
	if requested := strings.ToLower(strings.TrimSpace(firstString(value, "layer"))); requested == string(usage.LayerModel) || requested == string(usage.LayerRuntime) {
		layer = usage.ToolLayer(requested)
	}
	protocol := strings.ToLower(strings.TrimSpace(firstString(value, "protocol", "transport")))
	if layer == usage.LayerRuntime {
		if stringValue(value, "type") == "" {
			switch protocol {
			case "mcp":
				value["type"] = "McpToolCall"
			case "command", "shell":
				value["type"] = "CommandExecution"
			}
		}
		if parent != nil {
			mergeMap(value, mapValue(parent, "result"))
		}
		if arguments := mapValue(value, "arguments"); arguments != nil {
			if command := firstString(arguments, "command", "cmd", "script"); command != "" {
				value["command"] = command
			}
		}
		return usage.NormalizeRuntimeItem(sessionID, turnID, value, item.Timestamp, item.source())
	}
	if stringValue(value, "type") == "" {
		value["type"] = "custom_tool_call"
	}
	return usage.NormalizeModelCall(sessionID, turnID, value, item.Timestamp, item.source())
}

func runtimeEventKind(kind string) bool {
	if strings.Contains(kind, "completion") || strings.Contains(kind, "result") || strings.Contains(kind, "runtime") || strings.Contains(kind, "execution") {
		return true
	}
	switch {
	case kind == "command", strings.Contains(kind, "shellcommand"):
		return true
	case strings.Contains(kind, "mcptoolcall"), strings.Contains(kind, "mcpcall"):
		return true
	case strings.Contains(kind, "filechange"), strings.Contains(kind, "websearch"):
		return true
	case strings.Contains(kind, "imageview"), strings.Contains(kind, "imagegeneration"):
		return true
	case strings.Contains(kind, "collabagenttoolcall"), strings.Contains(kind, "collaboration"):
		return true
	default:
		return false
	}
}

func isToolLikeMap(value map[string]any) bool {
	kind := compact(firstString(value, "type", "kind", "activity_type"))
	if strings.Contains(kind, "tool") || strings.Contains(kind, "command") || strings.Contains(kind, "execution") || strings.Contains(kind, "invocation") || runtimeEventKind(kind) {
		return true
	}
	return firstString(value, "raw_name", "tool_name") != ""
}

func isToolEvent(item event) bool {
	kind := compact(item.EventType)
	switch kind {
	case "toolcall", "tooluse", "toolresult", "tooloutput", "toolinvocation", "command", "commandcompletion", "commandexecution", "shellcommand", "mcpcall", "mcptoolcall", "customtoolcall", "functioncall", "invocation", "runtime", "runtimeactivity", "itemcompleted":
		return true
	default:
		return false
	}
}

func skillActivityNamesValues(values []any) []string {
	result := make(map[string]struct{})
	var visit func(any)
	visit = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			kind := compact(firstString(typed, "type", "kind", "activity_type"))
			if kind == "skill" || strings.Contains(kind, "skilltool") {
				for _, key := range []string{"name", "skill", "skill_name", "skillName", "path"} {
					if name := skillName(stringValue(typed, key)); name != "" {
						result[name] = struct{}{}
					}
				}
			}
			for _, child := range typed {
				visit(child)
			}
		case []any:
			for _, child := range typed {
				visit(child)
			}
		}
	}
	for _, value := range values {
		visit(value)
	}
	names := make([]string, 0, len(result))
	for name := range result {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func skillName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.TrimPrefix(value, "$")
	if strings.Contains(value, "/") {
		return usage.SkillNameFromPath(value)
	}
	for _, r := range value {
		if r == '-' || r == '_' || r == '.' || r == ':' ||
			r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			continue
		}
		return ""
	}
	return value
}

func recognized(item event) bool {
	kind := compact(item.EventType)
	if isUserMessage(item) || isToolEvent(item) {
		return true
	}
	switch kind {
	case "message", "usermessage", "userinput", "prompt", "assistantmessage", "activity", "skill", "skillactivity", "skilltool", "skillinvocation", "turnstarted", "turncomplete", "taskstarted", "taskcomplete", "turnaborted":
		return true
	}
	return false
}

func isUserMessage(item event) bool {
	kind := compact(item.EventType)
	return item.Role == "user" && (kind == "message" || kind == "usermessage" || kind == "userinput" || kind == "prompt" || kind == "")
}

func isTurnComplete(item event) bool {
	kind := compact(item.EventType)
	return kind == "turncomplete" || kind == "taskcomplete" || kind == "turnaborted"
}

func isNonUsageEvent(item event) bool {
	// ctx summaries are provider metadata, not a user prompt or tool
	// observation. They are expected in a complete event stream and should not
	// produce an unknown-event warning or an empty usage turn.
	return compact(item.EventType) == "summary"
}

func (item event) source() usage.SourceRef {
	path := "ctx"
	if item.ID != "" {
		path = "ctx://" + item.ID
	}
	source := usage.NewCtxSourceRef(path, item.Provider, item.ProviderSessionID, item.CtxSessionID, item.ID)
	source.Line = item.Line
	return source
}

func (item event) sessionID() string {
	source := item.source()
	parts := []string{"ctx", source.Agent, item.Provider, item.ProviderSessionID, item.CtxSessionID}
	if item.ProviderSessionID == "" && item.CtxSessionID == "" {
		parts = append(parts, item.ID)
	}
	return strings.Join(parts, "\x00")
}

func (item event) turnID() string {
	if item.TurnIDValue != "" {
		return item.TurnIDValue
	}
	return turnIDFromValues(item.PayloadValues)
}

func turnIDFromValues(values []any) string {
	for _, value := range values {
		if payload, ok := value.(map[string]any); ok {
			if id := findString(payload, "turn_id", "turnId", "provider_turn_id", "providerTurnId", "task_id", "taskId", "interaction_id", "interactionId"); id != "" {
				return id
			}
		}
	}
	return ""
}

func eventTextValues(values []any) string {
	for _, value := range values {
		payload, ok := value.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"text", "message", "content"} {
			if text := textValue(payload[key]); text != "" {
				return text
			}
		}
	}
	return ""
}

func eventSkillTextsValues(values []any) []string {
	texts := make([]string, 0)
	seen := make(map[string]struct{})
	for _, value := range values {
		payload, ok := value.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"text", "message", "content", "structured_content"} {
			text := textValue(payload[key])
			if text == "" {
				continue
			}
			if _, ok := seen[text]; ok {
				continue
			}
			seen[text] = struct{}{}
			texts = append(texts, text)
		}
	}
	return texts
}

func hasSelectedSkillInstructionsValues(values []any) bool {
	for _, value := range values {
		if containsSelectedSkillInstructions(value) {
			return true
		}
	}
	return false
}

func containsSelectedSkillInstructions(value any) bool {
	switch typed := value.(type) {
	case []any:
		for _, child := range typed {
			if containsSelectedSkillInstructions(child) {
				return true
			}
		}
	case map[string]any:
		if kinds, ok := typed["content_item_kinds"]; ok && selectedSkillKind(kinds) {
			return true
		}
		for _, child := range typed {
			if containsSelectedSkillInstructions(child) {
				return true
			}
		}
	}
	return false
}

func selectedSkillKind(value any) bool {
	switch typed := value.(type) {
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "skills.selected_skill_instructions")
	case []any:
		for _, child := range typed {
			if selectedSkillKind(child) {
				return true
			}
		}
	}
	return false
}

func textValue(value any) string {
	text := valueText(value)
	if text == "" {
		return ""
	}
	var decoded any
	if err := json.Unmarshal([]byte(text), &decoded); err == nil {
		if decodedText := valueText(decoded); decodedText != "" {
			return decodedText
		}
	}
	return text
}

func onlyInjectedSkill(text string) bool {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(strings.ToLower(text), "<skill") {
		return false
	}
	if len(usage.DetectInjectedSkills(text, "", "", time.Time{}, usage.SourceRef{})) == 0 {
		return false
	}
	lower := strings.ToLower(text)
	closeIndex := strings.LastIndex(lower, "</skill")
	if closeIndex < 0 {
		return false
	}
	end := strings.Index(lower[closeIndex:], ">")
	return end >= 0 && strings.TrimSpace(text[closeIndex+end+1:]) == ""
}

func findString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if text := stringValue(value, key); text != "" {
			return text
		}
	}
	for key, child := range value {
		if key == "content" || key == "activity" || key == "structured_content" || key == "invocation" || key == "metadata" || key == "source" || key == "item" || key == "internal_chat_message_metadata_passthrough" {
			if nested := mapValueFromAny(child); nested != nil {
				if text := findString(nested, keys...); text != "" {
					return text
				}
			}
		}
	}
	return ""
}

func valueText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, child := range typed {
			if text := valueText(child); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		for _, key := range []string{"text", "value", "message", "content", "input", "source"} {
			if text := valueText(typed[key]); text != "" {
				return text
			}
		}
	}
	return ""
}

// eventPayloadValues returns the event itself, the normalized content maps,
// and JSON payloads embedded in ctx's source projection. ctx keeps provider
// payloads under event.text or source.text when full content is requested;
// older synthetic projections may put them under content.source.text.
// Exposing all three shapes lets the normalizer consume provider payloads
// without reading provider-owned files.
func eventPayloadValues(raw map[string]any) []any {
	values := []any{raw}
	if text := stringValue(raw, "text"); text != "" {
		var decoded any
		if err := json.Unmarshal([]byte(text), &decoded); err == nil {
			values = append(values, decoded)
		}
	}
	if source := mapValue(raw, "source"); source != nil {
		values = append(values, source)
		if text := stringValue(source, "text"); text != "" {
			var decoded any
			if err := json.Unmarshal([]byte(text), &decoded); err == nil {
				values = append(values, decoded)
			}
		}
	}
	content := mapValue(raw, "content")
	if content == nil {
		return values
	}
	values = append(values, content)
	source := mapValue(content, "source")
	if source == nil {
		return values
	}
	values = append(values, source)
	text := stringValue(source, "text")
	if text == "" {
		return values
	}
	var decoded any
	if err := json.Unmarshal([]byte(text), &decoded); err == nil {
		values = append(values, decoded)
	}
	return values
}

func mapValue(raw map[string]any, key string) map[string]any {
	return mapValueFromAny(raw[key])
}

func mapValueFromAny(value any) map[string]any {
	valueMap, _ := value.(map[string]any)
	return valueMap
}

func copyMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, child := range value {
		result[key] = child
	}
	return result
}

func mergeMap(target, source map[string]any) {
	for key, value := range source {
		target[key] = value
	}
}

func firstString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if text := stringValue(value, key); text != "" {
			return text
		}
	}
	return ""
}

func stringValue(value map[string]any, key string) string {
	text, _ := value[key].(string)
	return strings.TrimSpace(text)
}

func boolValue(value map[string]any, key string) bool {
	result, _ := value[key].(bool)
	return result
}

func integerValue(value map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if number, ok := numberValue(value[key]); ok {
			return int64(number)
		}
	}
	return 0
}

func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func compact(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, "-", "")
	value = strings.ReplaceAll(value, " ", "")
	return value
}
