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

const ParserVersion = "opencode-normalizer-v2"

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

type SessionMetadata struct {
	ID          string          `json:"id"`
	ProjectPath string          `json:"project_path,omitempty"`
	CLIVersion  string          `json:"cli_version,omitempty"`
	CreatedAt   time.Time       `json:"created_at,omitempty"`
	UpdatedAt   time.Time       `json:"updated_at,omitempty"`
	Source      usage.SourceRef `json:"source"`
}

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
	database, err := discoverDatabase(root)
	if err != nil {
		return IngestResult{}, err
	}
	if database == "" {
		return result, nil
	}
	scope := root
	revision, err := sourceRevision(database)
	if err != nil {
		return IngestResult{}, err
	}
	store := cache.New(options.CacheDir)
	if store.Dir != "" {
		data, hit, readErr := store.Read(string(usage.SourceOpenCode), scope, revision, ParserVersion)
		if readErr != nil {
			diagnose(options, fmt.Sprintf("opencode cache read failed: %v", readErr))
		}
		if hit {
			var snapshot cache.Snapshot
			if err := json.Unmarshal(data, &snapshot); err == nil {
				diagnose(options, "opencode cache hit; applying selected period locally")
				result := resultFromSnapshot(snapshot)
				if filter.active() {
					result = filterResult(result, filter)
				}
				return result, nil
			}
			diagnose(options, "opencode cache miss; invalid snapshot")
		} else {
			diagnose(options, "opencode cache miss; reading source")
		}
	}
	normalizer := newNormalizer(database)
	if err := Read(database, normalizer.consume); err != nil {
		return IngestResult{}, err
	}
	result = normalizer.result()
	if store.Dir != "" {
		if after, afterErr := sourceRevision(database); afterErr == nil && after == revision {
			if err := store.Write(string(usage.SourceOpenCode), scope, revision, ParserVersion, snapshotFromResult(result)); err != nil {
				diagnose(options, fmt.Sprintf("opencode cache write failed: %v", err))
			} else {
				diagnose(options, "opencode cache stored complete snapshot")
			}
		} else {
			diagnose(options, "opencode cache skipped; source revision changed")
		}
	}
	if filter.active() {
		result = filterResult(result, filter)
	}
	return result, nil
}

func discoverDatabase(root string) (string, error) {
	primary := filepath.Join(root, "opencode.db")
	if _, err := os.Stat(primary); err == nil {
		return primary, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("OpenCode database %q: %w", primary, err)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return "", fmt.Errorf("read OpenCode data root %q: %w", root, err)
	}
	candidates := make([]string, 0)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "opencode-") || !strings.HasSuffix(name, ".db") || name == "opencode-.db" {
			continue
		}
		candidates = append(candidates, name)
	}
	if len(candidates) == 0 {
		return "", nil
	}
	sort.Strings(candidates)
	return filepath.Join(root, candidates[0]), nil
}

func diagnose(options IngestOptions, message string) {
	if options.Diagnostic != nil {
		options.Diagnostic(message)
	}
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
			ID:          session.ID,
			ProjectPath: session.ProjectPath,
			CLIVersion:  session.CLIVersion,
			Source:      cache.SourceRefFromUsage(session.Source),
		})
	}
	return snapshot
}

func resultFromSnapshot(snapshot cache.Snapshot) IngestResult {
	result := IngestResult{
		Agents:   append([]string(nil), snapshot.Agents...),
		Warnings: append([]usage.Warning(nil), snapshot.Warnings...),
	}
	if len(result.Agents) == 0 {
		result.Agents = []string{"opencode"}
	}
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
		selectedSessions[turn.SessionID] = struct{}{}
	}
	filtered.Sessions = nil
	for _, session := range result.Sessions {
		if _, ok := selectedSessions[session.ID]; ok {
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
	filtered.SkillEvidence = filterSkills(turn.SkillEvidence, filter)
	if len(turn.TokenUsageEvents) > 0 {
		filtered.TokenUsage = nil
		filtered.TokenUsageEvents = nil
		for _, event := range turn.TokenUsageEvents {
			if filter.accept(event.Timestamp) {
				filtered.AddTokenUsageAt(event.Timestamp, event.Usage)
			}
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
	for _, event := range turn.TokenUsageEvents {
		if filter.accept(event.Timestamp) {
			return true
		}
	}
	return false
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
