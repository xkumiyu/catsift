package opencode

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/xkumiyu/catsift/internal/cache"
	"github.com/xkumiyu/catsift/internal/usage"
)

const ParserVersion = "opencode-normalizer-v3"

// MultipleDatabasesWarningReason reports that a data root contained more
// than one OpenCode database while only the selected one was read.
const MultipleDatabasesWarningReason = "opencode_multiple_databases"

type IngestOptions struct {
	Days       int
	DaysSet    bool
	From       time.Time
	To         time.Time
	Now        time.Time
	CacheDir   string
	Diagnostic func(string)
}

type IngestResult struct {
	Turns    []usage.Turn
	Sessions []SessionMetadata
	Agents   []string
	Warnings []usage.Warning
}

type SessionMetadata = usage.Session

// Load discovers the OpenCode database below dataRoot. A root without a
// database is a normal empty source; root errors are source errors.
func Load(dataRoot string, options IngestOptions) (IngestResult, error) {
	filter, err := filterForOptions(options)
	if err != nil {
		return IngestResult{}, err
	}
	root, err := validateDataRoot(dataRoot)
	if err != nil {
		return IngestResult{}, err
	}
	result := IngestResult{Agents: []string{"opencode"}}
	database, candidates, err := discoverDatabase(root)
	if err != nil {
		return IngestResult{}, err
	}
	if database == "" {
		diagnose(options, fmt.Sprintf("opencode source: root=%q database=none parser=%s", root, ParserVersion))
		return result, nil
	}
	discoveryWarning, hasDiscoveryWarning := multipleDatabasesWarning(root, database, candidates)
	if hasDiscoveryWarning {
		diagnose(options, fmt.Sprintf("opencode source: multiple databases found count=%d selected=%q", len(candidates), database))
	}
	scope := root
	revision, err := sourceRevision(database)
	if err != nil {
		return IngestResult{}, err
	}
	cachePath := cache.New(options.CacheDir).Path(string(usage.SourceOpenCode), scope)
	diagnose(options, fmt.Sprintf("opencode source: root=%q database=%q revision=%q parser=%s", root, database, revision, ParserVersion))
	store := cache.New(options.CacheDir)
	if store.Dir != "" {
		diagnose(options, fmt.Sprintf("opencode cache: lookup path=%q revision=%q parser=%s", cachePath, revision, ParserVersion))
		data, hit, readErr := store.Read(string(usage.SourceOpenCode), scope, revision, ParserVersion)
		if readErr != nil {
			diagnose(options, fmt.Sprintf("opencode cache read failed path=%q: %v", cachePath, readErr))
		}
		if hit {
			var snapshot cache.Snapshot
			if err := json.Unmarshal(data, &snapshot); err == nil {
				diagnose(options, fmt.Sprintf("opencode cache: hit path=%q", cachePath))
				diagnose(options, "opencode cache hit; applying selected period locally")
				result := resultFromSnapshot(snapshot, database)
				if filter.active() {
					result = filterResult(result, filter)
				}
				if hasDiscoveryWarning {
					result.Warnings = append(result.Warnings, discoveryWarning)
				}
				return result, nil
			}
			diagnose(options, fmt.Sprintf("opencode cache miss; invalid snapshot path=%q", cachePath))
		} else {
			diagnose(options, fmt.Sprintf("opencode cache miss; reading source %q", database))
		}
	}
	diagnose(options, fmt.Sprintf("opencode source: reading database %q", database))
	normalizer := newNormalizer(database)
	if err := Read(database, normalizer.consume); err != nil {
		return IngestResult{}, err
	}
	result = normalizer.result()
	if store.Dir != "" {
		after, afterErr := sourceRevision(database)
		switch {
		case afterErr != nil:
			diagnose(options, fmt.Sprintf("opencode cache skipped; source revision check failed: %v", afterErr))
		case after != revision:
			diagnose(options, fmt.Sprintf("opencode cache skipped; source revision changed: before=%q after=%q", revision, after))
		default:
			if err := store.Write(string(usage.SourceOpenCode), scope, revision, ParserVersion, snapshotFromResult(result)); err != nil {
				diagnose(options, fmt.Sprintf("opencode cache write failed path=%q: %v", cachePath, err))
			} else {
				diagnose(options, fmt.Sprintf("opencode cache stored complete snapshot path=%q", cachePath))
			}
		}
	}
	if filter.active() {
		result = filterResult(result, filter)
	}
	if hasDiscoveryWarning {
		result.Warnings = append(result.Warnings, discoveryWarning)
	}
	return result, nil
}

func multipleDatabasesWarning(root, selected string, candidates []string) (usage.Warning, bool) {
	if selected == "" || len(candidates) <= 1 {
		return usage.Warning{}, false
	}
	return usage.Warning{
		Reason: MultipleDatabasesWarningReason,
		Type:   "database",
		Source: usage.SourceOpenCode,
		Path:   root,
		Count:  len(candidates) - 1,
	}, true
}

func discoverDatabase(root string) (string, []string, error) {
	primary := filepath.Join(root, "opencode.db")
	primaryExists := false
	if _, err := os.Stat(primary); err == nil {
		primaryExists = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", nil, fmt.Errorf("OpenCode database %q: %w", primary, err)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return "", nil, fmt.Errorf("read OpenCode data root %q: %w", root, err)
	}
	channelNames := make([]string, 0)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "opencode-") || !strings.HasSuffix(name, ".db") || name == "opencode-.db" {
			continue
		}
		channelNames = append(channelNames, name)
	}
	// OpenCode uses opencode.db for its latest/beta/prod channels and
	// opencode-<channel>.db otherwise, so channel switches can leave several
	// databases behind. Keep the default database authoritative when it
	// exists; otherwise read the most recently written channel database.
	// Every ambiguous layout is reported with MultipleDatabasesWarningReason.
	if primaryExists {
		sort.Strings(channelNames)
		candidates := make([]string, 0, len(channelNames)+1)
		candidates = append(candidates, primary)
		for _, name := range channelNames {
			candidates = append(candidates, filepath.Join(root, name))
		}
		return primary, candidates, nil
	}
	if len(channelNames) == 0 {
		return "", nil, nil
	}
	selected, candidates, err := newestChannelDatabase(root, channelNames)
	if err != nil {
		return "", nil, err
	}
	if selected == "" {
		return "", nil, nil
	}
	return selected, candidates, nil
}

// newestChannelDatabase selects the most recently written channel database.
// Ties fall back to lexicographic order so the same filesystem state always
// yields the same database. WAL sidecars advance history without changing
// the main file, so their modification times participate in the comparison.
func newestChannelDatabase(root string, names []string) (string, []string, error) {
	type candidate struct {
		name    string
		path    string
		modTime time.Time
	}
	considered := make([]candidate, 0, len(names))
	for _, name := range names {
		path := filepath.Join(root, name)
		modTime, err := databaseModTime(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return "", nil, err
		}
		considered = append(considered, candidate{name: name, path: path, modTime: modTime})
	}
	if len(considered) == 0 {
		return "", nil, nil
	}
	sort.Slice(considered, func(i, j int) bool {
		if !considered[i].modTime.Equal(considered[j].modTime) {
			return considered[i].modTime.After(considered[j].modTime)
		}
		return considered[i].name < considered[j].name
	})
	candidates := make([]string, 0, len(considered))
	for _, item := range considered {
		candidates = append(candidates, item.path)
	}
	return considered[0].path, candidates, nil
}

// databaseModTime reports the latest modification time across a database and
// its SQLite WAL sidecars. Missing sidecars are normal and ignored.
func databaseModTime(database string) (time.Time, error) {
	var latest time.Time
	seen := false
	for _, suffix := range []string{"", "-wal", "-shm"} {
		path := database + suffix
		info, err := os.Stat(path)
		if suffix != "" && errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return time.Time{}, fmt.Errorf("stat OpenCode source file %q: %w", path, err)
		}
		if !seen || info.ModTime().After(latest) {
			latest = info.ModTime()
			seen = true
		}
	}
	if !seen {
		return time.Time{}, os.ErrNotExist
	}
	return latest, nil
}

// HasHistory reports whether dataRoot contains an OpenCode database.
func HasHistory(dataRoot string) (bool, error) {
	root, err := validateDataRoot(dataRoot)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	database, _, err := discoverDatabase(root)
	if err != nil {
		return false, err
	}
	return database != "", nil
}

func diagnose(options IngestOptions, message string) {
	if options.Diagnostic != nil {
		options.Diagnostic(message)
	}
}

func snapshotFromResult(result IngestResult) cache.Snapshot {
	snapshot := cache.Snapshot{
		Agents:   append([]string(nil), result.Agents...),
		Warnings: cache.WarningsFromUsage(result.Warnings),
	}
	for _, turn := range result.Turns {
		snapshot.Turns = append(snapshot.Turns, cache.TurnFromUsage(turn))
	}
	for _, session := range result.Sessions {
		snapshot.Sessions = append(snapshot.Sessions, cache.SessionFromUsage(session))
	}
	return snapshot
}

func resultFromSnapshot(snapshot cache.Snapshot, database string) IngestResult {
	result := IngestResult{
		Agents:   append([]string(nil), snapshot.Agents...),
		Warnings: cache.WarningsToUsageForSource(snapshot.Warnings, usage.SourceOpenCode, database),
	}
	if len(result.Agents) == 0 {
		result.Agents = []string{"opencode"}
	}
	for _, turn := range snapshot.Turns {
		result.Turns = append(result.Turns, turn.Usage())
	}
	for _, session := range snapshot.Sessions {
		result.Sessions = append(result.Sessions, session.Usage())
	}
	return result
}

type timestampFilter struct {
	cutoff time.Time
	until  time.Time
}

func filterForOptions(options IngestOptions) (timestampFilter, error) {
	hasDays := options.DaysSet || options.Days != 0
	hasRange := !options.From.IsZero() || !options.To.IsZero()
	if hasDays && hasRange {
		return timestampFilter{}, errors.New("days cannot be combined with from or to")
	}
	if hasDays {
		if options.Days <= 0 {
			return timestampFilter{}, errors.New("days must be at least 1")
		}
		now := options.Now
		if now.IsZero() {
			now = time.Now().UTC()
		}
		return timestampFilter{cutoff: now.Add(-time.Duration(options.Days) * 24 * time.Hour)}, nil
	}
	if hasRange && !options.From.IsZero() && !options.To.IsZero() && !options.From.Before(options.To) {
		return timestampFilter{}, errors.New("from must be before to")
	}
	return timestampFilter{cutoff: options.From, until: options.To}, nil
}

func (f timestampFilter) active() bool { return !f.cutoff.IsZero() || !f.until.IsZero() }

func (f timestampFilter) accept(timestamp time.Time) bool {
	if !f.active() {
		return true
	}
	if timestamp.IsZero() || (!f.cutoff.IsZero() && timestamp.Before(f.cutoff)) {
		return false
	}
	return f.until.IsZero() || timestamp.Before(f.until)
}

func filterResult(result IngestResult, filter timestampFilter) IngestResult {
	filtered := result
	filtered.Turns = nil
	selectedSessions := make(map[string]struct{})
	for _, turn := range result.Turns {
		value, ok := filterTurn(turn, filter)
		if !ok {
			continue
		}
		filtered.Turns = append(filtered.Turns, value)
		selectedSessions[usage.NewSessionKey(turn.Source, turn.SessionID)] = struct{}{}
	}
	filtered.Sessions = nil
	for _, session := range result.Sessions {
		if _, ok := selectedSessions[session.QualifiedKey()]; ok {
			filtered.Sessions = append(filtered.Sessions, session)
		}
	}
	return filtered
}

func filterTurn(turn usage.Turn, filter timestampFilter) (usage.Turn, bool) {
	if !turnHasAcceptedTimestamp(turn, filter) {
		return usage.Turn{}, false
	}
	filtered := turn
	if !filter.accept(turn.StartedAt) {
		filtered.StartedAt = time.Time{}
	}
	if !filter.accept(turn.EndedAt) {
		filtered.EndedAt = time.Time{}
	}
	filtered.UserPromptTimes = filterTimes(turn.UserPromptTimes, filter)
	if len(turn.UserPromptTimes) > 0 {
		filtered.UserPrompts = len(filtered.UserPromptTimes)
	} else if turn.UserPrompts > 0 {
		filtered.UserPrompts = 0
	}
	filtered.ModelTools = filterTools(turn.ModelTools, filter)
	filtered.RuntimeTools = filterTools(turn.RuntimeTools, filter)
	filtered.ModelObservations = filterModels(turn.ModelObservations, filter)
	filtered.SkillEvidence = filterSkills(turn.SkillEvidence, filter)
	if len(turn.TokenUsageEvents) > 0 {
		filtered.TokenUsage = nil
		filtered.TokenUsageEvents = nil
		var total usage.TokenUsage
		included := false
		for _, event := range turn.TokenUsageEvents {
			if !filter.accept(event.Timestamp) {
				continue
			}
			filtered.TokenUsageEvents = append(filtered.TokenUsageEvents, event)
			total.Add(event.Usage)
			included = true
		}
		if included {
			filtered.TokenUsage = &total
		}
	}
	return filtered, true
}

func turnHasAcceptedTimestamp(turn usage.Turn, filter timestampFilter) bool {
	if filter.accept(turn.StartedAt) || filter.accept(turn.EndedAt) {
		return true
	}
	for _, timestamp := range turn.UserPromptTimes {
		if filter.accept(timestamp) {
			return true
		}
	}
	for _, tool := range append(append([]usage.ToolObservation{}, turn.ModelTools...), turn.RuntimeTools...) {
		if filter.accept(tool.Timestamp) {
			return true
		}
	}
	for _, skill := range turn.SkillEvidence {
		if filter.accept(skill.Timestamp) {
			return true
		}
	}
	for _, model := range turn.ModelObservations {
		if filter.accept(model.Timestamp) {
			return true
		}
	}
	for _, event := range turn.TokenUsageEvents {
		if filter.accept(event.Timestamp) {
			return true
		}
	}
	return false
}

func filterModels(values []usage.ModelObservation, filter timestampFilter) []usage.ModelObservation {
	filtered := make([]usage.ModelObservation, 0, len(values))
	for _, value := range values {
		if filter.accept(value.Timestamp) {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func filterTimes(values []time.Time, filter timestampFilter) []time.Time {
	filtered := make([]time.Time, 0, len(values))
	for _, value := range values {
		if filter.accept(value) {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func filterTools(values []usage.ToolObservation, filter timestampFilter) []usage.ToolObservation {
	filtered := make([]usage.ToolObservation, 0, len(values))
	for _, value := range values {
		if filter.accept(value.Timestamp) {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func filterSkills(values []usage.SkillEvidence, filter timestampFilter) []usage.SkillEvidence {
	filtered := make([]usage.SkillEvidence, 0, len(values))
	for _, value := range values {
		if filter.accept(value.Timestamp) {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func validateDataRoot(dataRoot string) (string, error) {
	value := strings.TrimSpace(dataRoot)
	if value == "" {
		return "", errors.New("OpenCode data root is empty")
	}
	root := filepath.Clean(value)
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("OpenCode data root %q: %w", root, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("OpenCode data root %q is not a directory", root)
	}
	if info.Mode().Perm()&0444 == 0 || info.Mode().Perm()&0111 == 0 {
		return "", fmt.Errorf("OpenCode data root %q: permission denied", root)
	}
	if absolute, err := filepath.Abs(root); err == nil {
		return absolute, nil
	}
	return root, nil
}
