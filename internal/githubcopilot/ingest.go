package githubcopilot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xkumiyu/catsift/internal/cache"
	"github.com/xkumiyu/catsift/internal/usage"
)

type IngestOptions struct {
	MaxLineBytes int
	Days         int
	DaysSet      bool
	From         time.Time
	To           time.Time
	Now          time.Time
	CacheDir     string
	Diagnostic   func(string)
}

// Load reads the complete Copilot event snapshot and applies period filters
// after cache lookup so a cached full history remains reusable.
func Load(dataRoot string, options IngestOptions) (IngestResult, error) {
	filter, err := filterForOptions(options)
	if err != nil {
		return IngestResult{}, err
	}
	root, err := canonicalRoot(dataRoot)
	if err != nil {
		return IngestResult{}, err
	}
	files, err := Discover(root)
	if err != nil {
		return IngestResult{}, err
	}
	revision, err := sourceRevision(root, files)
	if err != nil {
		return IngestResult{}, err
	}
	scope := root
	store := cache.New(options.CacheDir)
	cachePath := store.Path(string(usage.SourceCopilot), scope)
	diagnose(options, fmt.Sprintf("copilot source: root=%q files=%d revision=%q parser=%s", root, len(files), revision, ParserVersion))
	if store.Dir != "" {
		data, hit, readErr := store.Read(string(usage.SourceCopilot), scope, revision, ParserVersion)
		if readErr != nil {
			diagnose(options, fmt.Sprintf("copilot cache read failed path=%q: %v", cachePath, readErr))
		}
		if hit {
			var snapshot cache.Snapshot
			if err := json.Unmarshal(data, &snapshot); err == nil {
				diagnose(options, fmt.Sprintf("copilot cache hit path=%q", cachePath))
				return filterResult(resultFromSnapshot(snapshot), filter), nil
			}
			diagnose(options, fmt.Sprintf("copilot cache miss; invalid snapshot path=%q", cachePath))
		} else {
			diagnose(options, fmt.Sprintf("copilot cache miss; reading source root=%q", root))
		}
	}

	n := newNormalizer()
	warnings := &WarningCollector{}
	complete := true
	for _, path := range files {
		diagnose(options, fmt.Sprintf("copilot source: reading path=%q", path))
		metadataPath := workspaceMetadataPath(path)
		metadata, metadataErr := readWorkspaceMetadata(metadataPath)
		if metadataErr != nil && !errors.Is(metadataErr, os.ErrNotExist) {
			warnings.AddFile("read_workspace", metadataPath)
		}
		if err := DecodeFile(path, DecodeOptions{MaxLineBytes: options.MaxLineBytes, Warnings: warnings, SessionName: metadata.Name}, n.consume); err != nil {
			complete = false
			warnings.AddFile("read_file", path)
		} else if metadataErr == nil {
			n.observeWorkspaceMetadata(sessionIDFromPath(path), metadata, usage.SourceRef{Path: metadataPath, Source: usage.SourceCopilot, Agent: "copilot", Provider: "copilot", ProviderSessionID: sessionIDFromPath(path)})
		}
	}
	result := n.result()
	result.Warnings = warnings.Warnings()
	if store.Dir != "" && complete {
		afterFiles, discoverErr := Discover(root)
		if discoverErr != nil {
			diagnose(options, fmt.Sprintf("copilot cache skipped; source discovery after read failed: %v", discoverErr))
		} else if afterRevision, revisionErr := sourceRevision(root, afterFiles); revisionErr != nil {
			diagnose(options, fmt.Sprintf("copilot cache skipped; source revision check failed: %v", revisionErr))
		} else if !sameSourceFiles(files, afterFiles) || afterRevision != revision {
			diagnose(options, fmt.Sprintf("copilot cache skipped; source revision changed from %q to %q", revision, afterRevision))
		} else if err := store.Write(string(usage.SourceCopilot), scope, revision, ParserVersion, snapshotFromResult(result)); err != nil {
			diagnose(options, fmt.Sprintf("copilot cache write failed path=%q: %v", cachePath, err))
		} else {
			diagnose(options, fmt.Sprintf("copilot cache stored complete snapshot path=%q", cachePath))
		}
	}
	return filterResult(result, filter), nil
}

func canonicalRoot(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("GitHub Copilot data root is empty")
	}
	root, err := filepath.Abs(filepath.Clean(value))
	if err != nil {
		return "", fmt.Errorf("resolve GitHub Copilot data root %q: %w", value, err)
	}
	return root, nil
}

func diagnose(options IngestOptions, message string) {
	if options.Diagnostic != nil {
		options.Diagnostic(message)
	}
}

func snapshotFromResult(result IngestResult) cache.Snapshot {
	snapshot := cache.Snapshot{Agents: append([]string(nil), result.Agents...), Warnings: cache.WarningsFromUsage(result.Warnings)}
	for _, turn := range result.Turns {
		snapshot.Turns = append(snapshot.Turns, cache.TurnFromUsage(turn))
	}
	for _, session := range result.Sessions {
		snapshot.Sessions = append(snapshot.Sessions, cache.SessionFromUsage(session))
	}
	return snapshot
}

func resultFromSnapshot(snapshot cache.Snapshot) IngestResult {
	result := IngestResult{Agents: append([]string(nil), snapshot.Agents...), Warnings: cache.WarningsToUsageForSource(snapshot.Warnings, usage.SourceCopilot)}
	if len(result.Agents) == 0 {
		result.Agents = []string{"copilot"}
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
		return timestampFilter{cutoff: now.Add(-time.Duration(options.Days) * 24 * time.Hour), until: now}, nil
	}
	if hasRange && !options.From.IsZero() && !options.To.IsZero() && !options.From.Before(options.To) {
		return timestampFilter{}, errors.New("from must be before to")
	}
	return timestampFilter{cutoff: options.From, until: options.To}, nil
}

func (filter timestampFilter) active() bool { return !filter.cutoff.IsZero() || !filter.until.IsZero() }

func (filter timestampFilter) accept(timestamp time.Time) bool {
	if !filter.active() {
		return true
	}
	if timestamp.IsZero() || (!filter.cutoff.IsZero() && timestamp.Before(filter.cutoff)) {
		return false
	}
	return filter.until.IsZero() || timestamp.Before(filter.until)
}

func filterResult(result IngestResult, filter timestampFilter) IngestResult {
	if !filter.active() {
		return result
	}
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
	filtered.UserPromptTimes = filterTimes(turn.UserPromptTimes, filter)
	filtered.UserPrompts = len(filtered.UserPromptTimes)
	filtered.ModelTools = filterTools(turn.ModelTools, filter)
	filtered.RuntimeTools = filterTools(turn.RuntimeTools, filter)
	filtered.ModelObservations = filterModels(turn.ModelObservations, filter)
	filtered.SkillEvidence = filterSkills(turn.SkillEvidence, filter)
	if !filter.accept(turn.StartedAt) {
		filtered.StartedAt = time.Time{}
	}
	if !filter.accept(turn.EndedAt) {
		filtered.EndedAt = time.Time{}
	}
	if len(turn.TokenUsageEvents) > 0 {
		filtered.TokenUsage = nil
		filtered.TokenUsageEvents = nil
		var total usage.TokenUsage
		for _, event := range turn.TokenUsageEvents {
			if filter.accept(event.Timestamp) {
				filtered.TokenUsageEvents = append(filtered.TokenUsageEvents, event)
				total.Add(event.Usage)
			}
		}
		if len(filtered.TokenUsageEvents) > 0 {
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
	for _, item := range append(append([]usage.ToolObservation{}, turn.ModelTools...), turn.RuntimeTools...) {
		if filter.accept(item.Timestamp) {
			return true
		}
	}
	for _, item := range turn.ModelObservations {
		if filter.accept(item.Timestamp) {
			return true
		}
	}
	for _, item := range turn.SkillEvidence {
		if filter.accept(item.Timestamp) {
			return true
		}
	}
	for _, item := range turn.TokenUsageEvents {
		if filter.accept(item.Timestamp) {
			return true
		}
	}
	return false
}

func filterTimes(values []time.Time, filter timestampFilter) []time.Time {
	result := make([]time.Time, 0, len(values))
	for _, value := range values {
		if filter.accept(value) {
			result = append(result, value)
		}
	}
	return result
}

func filterModels(values []usage.ModelObservation, filter timestampFilter) []usage.ModelObservation {
	result := make([]usage.ModelObservation, 0, len(values))
	for _, value := range values {
		if filter.accept(value.Timestamp) {
			result = append(result, value)
		}
	}
	return result
}

func filterTools(values []usage.ToolObservation, filter timestampFilter) []usage.ToolObservation {
	result := make([]usage.ToolObservation, 0, len(values))
	for _, value := range values {
		if filter.accept(value.Timestamp) {
			result = append(result, value)
		}
	}
	return result
}

func filterSkills(values []usage.SkillEvidence, filter timestampFilter) []usage.SkillEvidence {
	result := make([]usage.SkillEvidence, 0, len(values))
	for _, value := range values {
		if filter.accept(value.Timestamp) {
			result = append(result, value)
		}
	}
	return result
}
