package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xkumiyu/catsift/internal/cache"
	"github.com/xkumiyu/catsift/internal/usage"
)

const DefaultMaxLineBytes = 4 << 20

const codexParserVersion = "codex-normalizer-v2"

// ResolveHome applies the Codex home precedence rule.
func ResolveHome(explicit string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return ResolveHomeFrom(explicit, os.Getenv("CODEX_HOME"), home)
}

// ResolveHomeFrom is the injectable form of ResolveHome used by tests and callers
// that already resolved the process environment.
func ResolveHomeFrom(explicit, envHome, userHome string) (string, error) {
	for _, candidate := range []string{explicit, envHome} {
		if strings.TrimSpace(candidate) != "" {
			return filepath.Clean(candidate), nil
		}
	}
	if strings.TrimSpace(userHome) == "" {
		return "", errors.New("cannot resolve user home")
	}
	return filepath.Join(userHome, ".codex"), nil
}

// Discover finds history files in both Codex history roots. Missing roots are
// normal; errors reading an existing root are returned to the caller.
func Discover(home string) ([]string, error) {
	if strings.TrimSpace(home) == "" {
		return nil, errors.New("codex home is empty")
	}
	info, err := os.Stat(home)
	if err != nil {
		return nil, fmt.Errorf("codex home %q: %w", home, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("codex home %q is not a directory", home)
	}
	seen := make(map[string]struct{})
	var files []string
	for _, rootName := range []string{"sessions", "archived_sessions"} {
		root := filepath.Join(home, rootName)
		rootInfo, statErr := os.Stat(root)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil {
			return nil, fmt.Errorf("discover %q: %w", root, statErr)
		}
		if !rootInfo.IsDir() {
			continue
		}
		err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".jsonl") {
				return nil
			}
			clean := filepath.Clean(path)
			if absolute, absErr := filepath.Abs(clean); absErr == nil {
				clean = absolute
			}
			if _, ok := seen[clean]; !ok {
				seen[clean] = struct{}{}
				files = append(files, clean)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("discover %q: %w", root, err)
		}
	}
	sort.Strings(files)
	return files, nil
}

// Envelope is the tolerant, line-level representation of a Codex record.
type Envelope struct {
	Timestamp time.Time       `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	SessionID string          `json:"session_id,omitempty"`
	Source    usage.SourceRef `json:"source"`
}

type DecodeOptions struct {
	MaxLineBytes int
	Warnings     *WarningCollector
}

func (o DecodeOptions) maxLineBytes() int {
	if o.MaxLineBytes <= 0 {
		return DefaultMaxLineBytes
	}
	return o.MaxLineBytes
}

// DecodeFile streams valid, known envelopes to fn. A malformed or unknown line
// is recorded and does not stop later lines from being decoded.
func DecodeFile(path string, opts DecodeOptions, fn func(Envelope)) (err error) {
	if fn == nil {
		fn = func(Envelope) {}
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close %q: %w", path, closeErr)
		}
	}()
	warnings := opts.Warnings
	if warnings == nil {
		warnings = &WarningCollector{}
	}
	reader := bufio.NewReaderSize(file, 64<<10)
	lineNo := 0
	for {
		line, tooLarge, readErr := readLine(reader, opts.maxLineBytes())
		if len(line) > 0 || readErr == nil || tooLarge {
			lineNo++
			if tooLarge {
				warnings.Add("large_line", path, lineNo)
			} else if len(bytes.TrimSpace(line)) == 0 {
				warnings.Add("empty_line", path, lineNo)
			} else {
				var raw struct {
					Timestamp json.RawMessage `json:"timestamp"`
					Type      string          `json:"type"`
					Payload   json.RawMessage `json:"payload"`
					SessionID string          `json:"session_id"`
				}
				if err := json.Unmarshal(line, &raw); err != nil {
					warnings.Add("malformed_json", path, lineNo)
				} else if ignoredType(raw.Type) {
					// These valid Codex metadata records are not needed for the
					// current aggregate and should not create noisy warnings.
				} else if !knownType(raw.Type) {
					warnings.AddType("unknown_type", raw.Type, path, lineNo)
				} else {
					ts, tsErr := parseTimestamp(raw.Timestamp)
					if tsErr != nil && len(raw.Timestamp) > 0 && string(raw.Timestamp) != "null" {
						warnings.Add("invalid_timestamp", path, lineNo)
					}
					fn(Envelope{Timestamp: ts, Type: raw.Type, Payload: raw.Payload, SessionID: raw.SessionID, Source: usage.NewCodexSourceRef(path, lineNo, "")})
				}
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return readErr
			}
			break
		}
	}
	return nil
}

func knownType(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "session_meta", "task_started", "task_complete", "turn_aborted", "event_msg", "response_item", "response", "user_message", "turn_started", "turn_complete":
		return true
	default:
		return false
	}
}

func ignoredType(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "turn_context", "world_state", "compacted", "inter_agent_communication_metadata":
		return true
	default:
		return false
	}
}

func readLine(reader *bufio.Reader, max int) ([]byte, bool, error) {
	var line []byte
	tooLarge := false
	for {
		part, err := reader.ReadSlice('\n')
		if !tooLarge {
			if len(line)+len(part) > max {
				tooLarge = true
				line = nil
			} else {
				line = append(line, part...)
			}
		}
		if err == nil {
			return bytes.TrimSuffix(line, []byte{'\n'}), tooLarge, nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return bytes.TrimSuffix(line, []byte{'\n'}), tooLarge, err
	}
}

func parseTimestamp(raw json.RawMessage) (time.Time, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return time.Time{}, nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return time.Parse(time.RFC3339Nano, text)
	}
	var number float64
	if json.Unmarshal(raw, &number) == nil {
		if number > 1e12 {
			return time.UnixMilli(int64(number)), nil
		}
		return time.Unix(int64(number), int64((number-float64(int64(number)))*1e9)), nil
	}
	return time.Time{}, errors.New("invalid timestamp")
}

// WarningCollector groups recoverable problems for machine and human renderers.
type WarningCollector struct {
	warnings []usage.Warning
}

func (c *WarningCollector) Add(reason, path string, line int) {
	c.add(usage.Warning{Reason: reason, Path: path, Line: line, Count: 1})
}

func (c *WarningCollector) AddType(reason, typ, path string, line int) {
	c.add(usage.Warning{Reason: reason, Type: strings.TrimSpace(typ), Path: path, Line: line, Count: 1})
}

func (c *WarningCollector) add(incoming usage.Warning) {
	for i := range c.warnings {
		w := &c.warnings[i]
		if w.Reason == incoming.Reason && w.Type == incoming.Type && w.Path == incoming.Path {
			w.Count += incoming.Count
			if w.Line != incoming.Line {
				w.Line = 0
			}
			return
		}
	}
	c.warnings = append(c.warnings, incoming)
}

func (c *WarningCollector) AddFile(reason, path string) { c.Add(reason, path, 0) }

func (c *WarningCollector) Warnings() []usage.Warning {
	result := append([]usage.Warning(nil), c.warnings...)
	return result
}

type TimestampFilter struct {
	cutoff time.Time
	until  time.Time
}

func NewTimestampFilter(days int, now time.Time) (TimestampFilter, error) {
	if days <= 0 {
		return TimestampFilter{}, errors.New("days must be at least 1")
	}
	if now.IsZero() {
		now = time.Now()
	}
	return TimestampFilter{cutoff: now.Add(-time.Duration(days) * 24 * time.Hour)}, nil
}

func NewDateRangeFilter(from, to time.Time) (TimestampFilter, error) {
	if !from.IsZero() && !to.IsZero() && !from.Before(to) {
		return TimestampFilter{}, errors.New("from must be before to")
	}
	return TimestampFilter{cutoff: from, until: to}, nil
}

func filterForOptions(options IngestOptions) (TimestampFilter, error) {
	hasDays := options.DaysSet || options.Days != 0
	hasRange := !options.From.IsZero() || !options.To.IsZero()
	if hasDays && hasRange {
		return TimestampFilter{}, errors.New("days cannot be combined with from or to")
	}
	if hasDays {
		return NewTimestampFilter(options.Days, options.Now)
	}
	if hasRange {
		return NewDateRangeFilter(options.From, options.To)
	}
	return TimestampFilter{}, nil
}

func (f TimestampFilter) Accept(timestamp time.Time) bool {
	if f.cutoff.IsZero() && f.until.IsZero() {
		return true
	}
	if timestamp.IsZero() || (!f.cutoff.IsZero() && timestamp.Before(f.cutoff)) {
		return false
	}
	return f.until.IsZero() || timestamp.Before(f.until)
}

func (f TimestampFilter) Cutoff() time.Time { return f.cutoff }

func (f TimestampFilter) Active() bool { return !f.cutoff.IsZero() || !f.until.IsZero() }

type IngestOptions struct {
	MaxLineBytes int
	Days         int
	DaysSet      bool
	From         time.Time
	To           time.Time
	Now          time.Time
	CacheDir     string
}

type IngestResult struct {
	Turns    []usage.Turn
	Sessions []SessionMetadata
	Warnings []usage.Warning
}

type SessionMetadata struct {
	ID          string          `json:"id"`
	ProjectPath string          `json:"project_path,omitempty"`
	CLIVersion  string          `json:"cli_version,omitempty"`
	Source      usage.SourceRef `json:"source"`
}

// Stream discovers history and emits each completed turn as soon as it is
// assembled. The callback is never called concurrently; turns are not retained
// by the ingestion pipeline.
func Stream(home string, opts IngestOptions, consume func(usage.Turn)) (sessions []SessionMetadata, warnings []usage.Warning, err error) {
	if consume == nil {
		consume = func(usage.Turn) {}
	}
	filter, err := filterForOptions(opts)
	if err != nil {
		return nil, nil, err
	}
	files, err := Discover(home)
	if err != nil {
		return nil, nil, err
	}
	collector := &WarningCollector{}
	var selectedSessionIDs map[string]struct{}
	if filter.Active() {
		selectedSessionIDs = make(map[string]struct{})
	}
	a := newAssembler(filter, collector, func(turn usage.Turn) {
		if selectedSessionIDs != nil {
			selectedSessionIDs[turn.SessionID] = struct{}{}
		}
		consume(turn)
	})
	for _, path := range files {
		if err := DecodeFile(path, DecodeOptions{MaxLineBytes: opts.MaxLineBytes, Warnings: collector}, a.consume); err != nil {
			collector.AddFile("read_file", path)
		}
	}
	a.flush()
	sessions = a.sessions()
	if selectedSessionIDs != nil {
		sessions = filterCodexSessions(sessions, selectedSessionIDs)
	}
	return sessions, collector.Warnings(), nil
}

// Load is the collecting convenience wrapper around Stream.
func Load(home string, opts IngestOptions) (IngestResult, error) {
	if strings.TrimSpace(opts.CacheDir) != "" {
		return loadCached(home, opts)
	}
	var result IngestResult
	sessions, warnings, err := Stream(home, opts, func(turn usage.Turn) { result.Turns = append(result.Turns, turn) })
	if err != nil {
		return IngestResult{}, err
	}
	result.Sessions, result.Warnings = sessions, warnings
	return result, nil
}

func loadCached(home string, opts IngestOptions) (IngestResult, error) {
	filter, err := filterForOptions(opts)
	if err != nil {
		return IngestResult{}, err
	}
	files, err := Discover(home)
	if err != nil {
		return IngestResult{}, err
	}
	store := cache.New(opts.CacheDir)
	var turns []usage.Turn
	var warnings []usage.Warning
	sessions := make(map[string]SessionMetadata)
	for _, path := range files {
		before, statErr := os.Stat(path)
		if statErr != nil {
			warnings = append(warnings, usage.Warning{Reason: "read_file", Path: path, Count: 1})
			continue
		}
		revision := fileRevision(before)
		data, hit, _ := store.Read("codex", path, revision, codexParserVersion)
		var snapshot cache.Snapshot
		var fileResult IngestResult
		if hit {
			if unmarshalErr := json.Unmarshal(data, &snapshot); unmarshalErr != nil {
				hit = false
			} else {
				fileResult = resultFromSnapshot(snapshot)
			}
		}
		if !hit {
			var complete bool
			snapshot, fileResult, complete = parseFileSnapshot(path, opts)
			if complete {
				if after, afterErr := os.Stat(path); afterErr == nil && fileRevision(after) == revision {
					_ = store.Write("codex", path, revision, codexParserVersion, snapshot)
				}
			}
		}
		turns = append(turns, fileResult.Turns...)
		for _, value := range fileResult.Sessions {
			sessions[value.ID] = value
		}
		warnings = append(warnings, fileResult.Warnings...)
	}
	if filter.Active() {
		filtered := turns[:0]
		selectedSessionIDs := make(map[string]struct{})
		for _, turn := range turns {
			if filteredTurn, ok := filterCodexTurn(turn, filter); ok {
				filtered = append(filtered, filteredTurn)
				selectedSessionIDs[turn.SessionID] = struct{}{}
			}
		}
		turns = filtered
		for id := range sessions {
			if _, ok := selectedSessionIDs[id]; !ok {
				delete(sessions, id)
			}
		}
	}
	sessionIDs := make([]string, 0, len(sessions))
	for id := range sessions {
		sessionIDs = append(sessionIDs, id)
	}
	sort.Strings(sessionIDs)
	resultSessions := make([]SessionMetadata, 0, len(sessionIDs))
	for _, id := range sessionIDs {
		resultSessions = append(resultSessions, sessions[id])
	}
	return IngestResult{Turns: turns, Sessions: resultSessions, Warnings: warnings}, nil
}

func filterCodexSessions(sessions []SessionMetadata, selected map[string]struct{}) []SessionMetadata {
	filtered := sessions[:0]
	for _, session := range sessions {
		if _, ok := selected[session.ID]; ok {
			filtered = append(filtered, session)
		}
	}
	return filtered
}

func filterCodexTurn(turn usage.Turn, filter TimestampFilter) (usage.Turn, bool) {
	when := turn.EndedAt
	if when.IsZero() {
		when = turn.StartedAt
	}
	if !filter.Accept(when) {
		return usage.Turn{}, false
	}
	filtered := turn
	if len(turn.UserPromptTimes) > 0 {
		filtered.UserPromptTimes = filtered.UserPromptTimes[:0]
		for _, timestamp := range turn.UserPromptTimes {
			if filter.Accept(timestamp) {
				filtered.UserPromptTimes = append(filtered.UserPromptTimes, timestamp)
			}
		}
		filtered.UserPrompts = len(filtered.UserPromptTimes)
	}
	filtered.ModelTools = filterCodexTools(turn.ModelTools, filter)
	filtered.RuntimeTools = filterCodexTools(turn.RuntimeTools, filter)
	filtered.SkillEvidence = filterCodexSkills(turn.SkillEvidence, filter)
	if len(turn.TokenUsageEvents) > 0 {
		filtered.TokenUsage = nil
		filtered.TokenUsageEvents = nil
		for _, event := range turn.TokenUsageEvents {
			if !filter.Accept(event.Timestamp) {
				continue
			}
			filtered.AddTokenUsageAt(event.Timestamp, event.Usage)
		}
	}
	return filtered, true
}

func filterCodexTools(values []usage.ToolObservation, filter TimestampFilter) []usage.ToolObservation {
	filtered := values[:0]
	for _, value := range values {
		if filter.Accept(value.Timestamp) {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func filterCodexSkills(values []usage.SkillEvidence, filter TimestampFilter) []usage.SkillEvidence {
	filtered := values[:0]
	for _, value := range values {
		if filter.Accept(value.Timestamp) {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func parseFileSnapshot(path string, opts IngestOptions) (cache.Snapshot, IngestResult, bool) {
	collector := &WarningCollector{}
	turns := make([]usage.Turn, 0)
	a := newAssembler(TimestampFilter{}, collector, func(turn usage.Turn) { turns = append(turns, turn) })
	err := DecodeFile(path, DecodeOptions{MaxLineBytes: opts.MaxLineBytes, Warnings: collector}, a.consume)
	if err != nil {
		collector.AddFile("read_file", path)
	}
	a.flush()
	result := IngestResult{Turns: turns, Sessions: a.sessions(), Warnings: collector.Warnings()}
	snapshot := cache.Snapshot{Warnings: result.Warnings}
	for _, turn := range turns {
		snapshot.Turns = append(snapshot.Turns, cache.TurnFromUsage(turn))
	}
	for _, session := range result.Sessions {
		snapshot.Sessions = append(snapshot.Sessions, cache.Session{
			ID:          session.ID,
			ProjectPath: session.ProjectPath,
			CLIVersion:  session.CLIVersion,
			Source:      cache.SourceRefFromUsage(session.Source),
		})
	}
	return snapshot, result, err == nil
}

func resultFromSnapshot(snapshot cache.Snapshot) IngestResult {
	result := IngestResult{Warnings: append([]usage.Warning(nil), snapshot.Warnings...)}
	for _, turn := range snapshot.Turns {
		result.Turns = append(result.Turns, turn.Usage())
	}
	for _, session := range snapshot.Sessions {
		result.Sessions = append(result.Sessions, SessionMetadata{
			ID:          session.ID,
			ProjectPath: session.ProjectPath,
			CLIVersion:  session.CLIVersion,
			Source:      session.Source.Usage(),
		})
	}
	return result
}

func fileRevision(info os.FileInfo) string {
	return fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
}

type assembler struct {
	filter               TimestampFilter
	warnings             *WarningCollector
	out                  func(usage.Turn)
	current              map[string]*turnState
	ordinals             map[string]int
	metadata             map[string]SessionMetadata
	pathToSID            map[string]string
	versions             map[string]string
	cumulativeTokenUsage map[string]usage.TokenUsage
}

type turnState struct {
	turn       usage.Turn
	hasContent bool
	lastTime   time.Time
}

func newAssembler(filter TimestampFilter, warnings *WarningCollector, out func(usage.Turn)) *assembler {
	return &assembler{filter: filter, warnings: warnings, out: out, current: make(map[string]*turnState), ordinals: make(map[string]int), metadata: make(map[string]SessionMetadata), pathToSID: make(map[string]string), versions: make(map[string]string), cumulativeTokenUsage: make(map[string]usage.TokenUsage)}
}

func (a *assembler) consume(env Envelope) {
	payload := object(env.Payload)
	sid := env.SessionID
	if sid == "" {
		sid = stringValue(payload, "session_id")
	}
	if sid == "" {
		sid = a.pathToSID[env.Source.Path]
	}
	if env.Type == "session_meta" {
		metaID := firstString(payload, "id", "session_id", "sessionId")
		if metaID != "" {
			sid = metaID
			a.pathToSID[env.Source.Path] = sid
		}
		if sid == "" {
			sid = env.Source.Path
		}
		version := firstString(payload, "cli_version", "version")
		if version != "" {
			a.versions[sid] = version
		}
		env.Source.CLIVersion = version
		meta := SessionMetadata{ID: sid, ProjectPath: firstString(payload, "project_path", "cwd", "projectPath"), CLIVersion: version, Source: env.Source}
		a.metadata[sid] = meta
		return
	}
	if sid == "" {
		sid = env.Source.Path
	}
	if sid != env.Source.Path {
		a.pathToSID[env.Source.Path] = sid
	}
	if version := a.versions[sid]; version != "" {
		env.Source.CLIVersion = version
	}
	if !a.filter.Accept(env.Timestamp) {
		return
	}
	id := recordTurnID(payload)
	kind := env.Type
	if kind == "event_msg" || kind == "response" || kind == "response_item" {
		kind = firstString(payload, "type", "event_type", "eventType")
	}
	if id == "" && isTurnBoundary(env.Type, kind) {
		id = firstString(payload, "id")
	}
	if strings.EqualFold(kind, "user_message") || strings.EqualFold(kind, "user_input") || (strings.EqualFold(kind, "message") && strings.EqualFold(firstString(payload, "role", "author"), "user")) || (env.Type == "user_message") {
		text := rawValueText(payload, "text", "message", "content")
		injected := onlyInjectedSkill(text)
		if cur := a.current[sid]; cur != nil && cur.turn.UserPrompts > 0 && !injected && (id == "" || cur.turn.ID != id) {
			a.finish(sid, false)
		}
		cur := a.ensure(sid, id, env)
		if !injected {
			cur.turn.UserPrompts++
			cur.turn.UserPromptTimes = append(cur.turn.UserPromptTimes, env.Timestamp)
		}
		cur.hasContent = true
		a.touch(cur, env.Timestamp, false)
		a.observe(cur, env, payload)
		return
	}
	if env.Type == "task_started" || env.Type == "turn_started" || kind == "task_started" {
		if cur := a.current[sid]; cur != nil && (id == "" || cur.turn.ID != id) {
			a.finish(sid, false)
		}
		cur := a.ensure(sid, id, env)
		cur.hasContent = true
		a.touch(cur, env.Timestamp, false)
		a.observe(cur, env, payload)
		return
	}
	if env.Type == "task_complete" || env.Type == "turn_complete" || kind == "task_complete" {
		cur := a.ensure(sid, id, env)
		cur.hasContent = true
		a.touch(cur, env.Timestamp, true)
		a.observe(cur, env, payload)
		a.finish(sid, false)
		return
	}
	if env.Type == "turn_aborted" || kind == "turn_aborted" {
		cur := a.ensure(sid, id, env)
		cur.turn.Aborted = true
		cur.hasContent = true
		a.touch(cur, env.Timestamp, true)
		a.observe(cur, env, payload)
		a.finish(sid, false)
		return
	}
	cur := a.ensure(sid, id, env)
	cur.hasContent = true
	a.touch(cur, env.Timestamp, false)
	a.observe(cur, env, payload)
}

func isTurnBoundary(envelopeType, payloadType string) bool {
	for _, value := range []string{envelopeType, payloadType} {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "task_started", "task_complete", "turn_started", "turn_complete", "turn_aborted":
			return true
		}
	}
	return false
}

func (a *assembler) ensure(sid, explicitID string, env Envelope) *turnState {
	if cur := a.current[sid]; cur != nil {
		if explicitID == "" || explicitID == cur.turn.ID {
			return cur
		}
		a.finish(sid, false)
	}
	a.ordinals[sid]++
	id := explicitID
	if id == "" {
		id = strconv.Itoa(a.ordinals[sid])
	}
	turn := usage.NewTurn(sid, id, a.ordinals[sid], env.Source)
	if version := a.versions[sid]; version != "" {
		turn.Source.CLIVersion = version
	}
	a.current[sid] = &turnState{turn: turn}
	return a.current[sid]
}

func (a *assembler) touch(cur *turnState, timestamp time.Time, end bool) {
	if timestamp.IsZero() {
		return
	}
	if cur.turn.StartedAt.IsZero() || timestamp.Before(cur.turn.StartedAt) {
		cur.turn.StartedAt = timestamp
	}
	cur.lastTime = timestamp
	if end {
		cur.turn.EndedAt = timestamp
	}
}

func (a *assembler) finish(sid string, _ bool) {
	cur := a.current[sid]
	if cur == nil {
		return
	}
	if cur.turn.EndedAt.IsZero() {
		cur.turn.EndedAt = cur.lastTime
	}
	a.out(cur.turn)
	delete(a.current, sid)
}

func (a *assembler) flush() {
	ids := make([]string, 0, len(a.current))
	for sid := range a.current {
		ids = append(ids, sid)
	}
	sort.Strings(ids)
	for _, sid := range ids {
		a.finish(sid, true)
	}
}

func (a *assembler) sessions() []SessionMetadata {
	ids := make([]string, 0, len(a.metadata))
	for id := range a.metadata {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]SessionMetadata, 0, len(ids))
	for _, id := range ids {
		result = append(result, a.metadata[id])
	}
	return result
}

func object(raw json.RawMessage) map[string]any {
	var value map[string]any
	_ = json.Unmarshal(raw, &value)
	return value
}

func stringValue(value map[string]any, key string) string {
	if text, ok := value[key].(string); ok {
		return text
	}
	return ""
}

func firstString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if text := stringValue(value, key); text != "" {
			return text
		}
	}
	return ""
}

func (a *assembler) observe(cur *turnState, env Envelope, payload map[string]any) {
	item := payload
	if nested, ok := payload["item"].(map[string]any); ok {
		item = nested
	}
	if env.Type == "event_msg" && sameRecordType(firstString(payload, "type", "event_type"), "token_count") {
		if tokenUsage, ok, cumulative := parseTokenUsage(payload); ok {
			if cumulative {
				current := tokenUsage
				if previous, exists := a.cumulativeTokenUsage[cur.turn.SessionID]; exists {
					tokenUsage = tokenUsageDelta(current, previous)
				}
				a.cumulativeTokenUsage[cur.turn.SessionID] = current
			}
			cur.turn.AddTokenUsageAt(env.Timestamp, tokenUsage)
		}
		return
	}
	if env.Type == "response_item" || env.Type == "response" {
		if obs, ok := usage.NormalizeModelCall(cur.turn.SessionID, cur.turn.ID, item, env.Timestamp, env.Source); ok {
			cur.turn.ModelTools = append(cur.turn.ModelTools, obs)
			cur.turn.SkillEvidence = append(cur.turn.SkillEvidence, usage.DetectStructuredSkillTool(obs)...)
		}
	}
	if env.Type == "event_msg" && sameRecordType(firstString(payload, "type", "event_type"), "ItemCompleted") {
		if sameRecordType(stringValue(item, "type"), "UserMessage") {
			text := rawValueText(item, "text", "message", "content")
			cur.turn.SkillEvidence = append(cur.turn.SkillEvidence, usage.DetectExplicitRequest(text, cur.turn.SessionID, cur.turn.ID, env.Timestamp, env.Source)...)
			cur.turn.SkillEvidence = append(cur.turn.SkillEvidence, usage.DetectRuntimeSkillItems(item, cur.turn.SessionID, cur.turn.ID, env.Timestamp, env.Source)...)
		}
		if obs, ok := usage.NormalizeRuntimeItem(cur.turn.SessionID, cur.turn.ID, item, env.Timestamp, env.Source); ok {
			cur.turn.RuntimeTools = append(cur.turn.RuntimeTools, obs)
			cur.turn.SkillEvidence = append(cur.turn.SkillEvidence, usage.DetectStructuredSkillTool(obs)...)
			// A failed command records an attempted path, not evidence that the
			// referenced skill was actually accessed. Keep the runtime observation
			// for diagnostics, but do not infer a skill use from it.
			if obs.Status != usage.StatusFailure {
				cur.turn.SkillEvidence = append(cur.turn.SkillEvidence, usage.DetectImplicitAccess(obs.Arguments, cur.turn.SessionID, cur.turn.ID, env.Timestamp, env.Source)...)
			}
		}
	}
	if strings.EqualFold(firstString(payload, "type", "event_type"), "user_message") || strings.EqualFold(firstString(payload, "type", "event_type"), "user_input") || (strings.EqualFold(firstString(payload, "type", "event_type"), "message") && strings.EqualFold(firstString(payload, "role", "author"), "user")) || env.Type == "user_message" {
		text := rawValueText(payload, "text", "message", "content")
		cur.turn.SkillEvidence = append(cur.turn.SkillEvidence, usage.DetectExplicitRequest(text, cur.turn.SessionID, cur.turn.ID, env.Timestamp, env.Source)...)
		if hasSelectedSkillInstructions(payload) {
			cur.turn.SkillEvidence = append(cur.turn.SkillEvidence, usage.DetectSelectedSkillInstructions(text, cur.turn.SessionID, cur.turn.ID, env.Timestamp, env.Source)...)
		} else {
			cur.turn.SkillEvidence = append(cur.turn.SkillEvidence, usage.DetectInjectedSkillsWithMode(text, cur.turn.SessionID, cur.turn.ID, usage.ModeUnknown, env.Timestamp, env.Source)...)
		}
	}
}

func parseTokenUsage(payload map[string]any) (usage.TokenUsage, bool, bool) {
	info, _ := payload["info"].(map[string]any)
	last, _ := info["last_token_usage"].(map[string]any)
	if len(last) == 0 {
		last, _ = payload["last_token_usage"].(map[string]any)
	}
	if len(last) == 0 {
		last, _ = info["usage"].(map[string]any)
	}
	if len(last) == 0 {
		last, _ = info["total_token_usage"].(map[string]any)
		if len(last) == 0 {
			last, _ = payload["total_token_usage"].(map[string]any)
		}
		if len(last) == 0 {
			return usage.TokenUsage{}, false, false
		}
		return tokenUsageFromMap(last), true, true
	}
	return tokenUsageFromMap(last), true, false
}

func tokenUsageFromMap(value map[string]any) usage.TokenUsage {
	return usage.TokenUsage{
		InputTokens:           integerValue(value, "input_tokens"),
		CachedInputTokens:     integerValue(value, "cached_input_tokens"),
		CacheWriteInputTokens: integerValue(value, "cache_write_input_tokens"),
		OutputTokens:          integerValue(value, "output_tokens"),
		ReasoningOutputTokens: integerValue(value, "reasoning_output_tokens"),
		TotalTokens:           integerValue(value, "total_tokens"),
	}
}

func tokenUsageDelta(current, previous usage.TokenUsage) usage.TokenUsage {
	return usage.TokenUsage{
		InputTokens:           nonNegativeDelta(current.InputTokens, previous.InputTokens),
		CachedInputTokens:     nonNegativeDelta(current.CachedInputTokens, previous.CachedInputTokens),
		CacheWriteInputTokens: nonNegativeDelta(current.CacheWriteInputTokens, previous.CacheWriteInputTokens),
		OutputTokens:          nonNegativeDelta(current.OutputTokens, previous.OutputTokens),
		ReasoningOutputTokens: nonNegativeDelta(current.ReasoningOutputTokens, previous.ReasoningOutputTokens),
		TotalTokens:           nonNegativeDelta(current.TotalTokens, previous.TotalTokens),
	}
}

func nonNegativeDelta(current, previous int64) int64 {
	if current < previous {
		return current
	}
	return current - previous
}

func integerValue(value map[string]any, key string) int64 {
	raw, ok := value[key]
	if !ok {
		return 0
	}
	switch typed := raw.(type) {
	case float64:
		return int64(typed)
	case float32:
		return int64(typed)
	case int:
		return int64(typed)
	case int64:
		return typed
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	default:
		return 0
	}
}

// sameRecordType accepts the spelling variants emitted by different Codex
// versions, such as ItemCompleted and item_completed.
func sameRecordType(value, expected string) bool {
	compact := func(input string) string {
		input = strings.ToLower(strings.TrimSpace(input))
		input = strings.ReplaceAll(input, "_", "")
		input = strings.ReplaceAll(input, "-", "")
		input = strings.ReplaceAll(input, " ", "")
		return input
	}
	return compact(value) == compact(expected)
}

func hasSelectedSkillInstructions(payload map[string]any) bool {
	metadata, ok := payload["internal_chat_message_metadata_passthrough"].(map[string]any)
	if !ok {
		return false
	}
	kinds, ok := metadata["content_item_kinds"].([]any)
	if !ok {
		return false
	}
	for _, value := range kinds {
		if kind, ok := value.(string); ok && strings.EqualFold(strings.TrimSpace(kind), "skills.selected_skill_instructions") {
			return true
		}
	}
	return false
}

func recordTurnID(payload map[string]any) string {
	if id := firstString(payload, "turn_id", "turnId", "task_id", "taskId"); id != "" {
		return id
	}
	for _, key := range []string{"internal_chat_message_metadata_passthrough", "metadata"} {
		if nested, ok := payload[key].(map[string]any); ok {
			if id := firstString(nested, "turn_id", "turnId", "task_id", "taskId"); id != "" {
				return id
			}
		}
	}
	if nested, ok := payload["item"].(map[string]any); ok {
		return firstString(nested, "turn_id", "turnId", "task_id", "taskId")
	}
	return ""
}

func rawValueText(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if raw, ok := value[key]; ok {
			if text := valueText(raw); text != "" {
				return text
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
		for _, item := range typed {
			if text := valueText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		for _, key := range []string{"text", "value", "content", "input", "message"} {
			if raw, ok := typed[key]; ok {
				if text := valueText(raw); text != "" {
					return text
				}
			}
		}
	}
	return ""
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
