// Package query builds the bounded, source-neutral read model used by the
// explorer. It deliberately accepts normalized turns and sessions only; raw
// provider records never cross this package boundary.
package query

import (
	"sort"
	"strings"
	"time"

	"github.com/xkumiyu/catsift/internal/usage"
)

// Input is the normalized source result passed to Build.
type Input struct {
	Turns    []usage.Turn
	Sessions []usage.Session
	Agents   []string
	Warnings []usage.Warning
	Source   usage.SourceKind
	Sources  []usage.SourceKind
	From     time.Time
	To       time.Time
	Filter   Filter
}

// Filter is shared by the static input path and interactive views. From is
// inclusive and To is exclusive, matching the source adapters' range rules.
type Filter struct {
	Source        usage.SourceKind
	Sources       []usage.SourceKind
	Agent         string
	Project       string
	Model         usage.ModelRef
	ModelKey      string
	ModelProvider string
	ModelName     string
	Skill         string
	Strict        bool
	Search        string
	From          time.Time
	To            time.Time
}

// Period is the selected time context shown by the overview.
type Period struct {
	From time.Time `json:"from,omitempty"`
	To   time.Time `json:"to,omitempty"`
}

// OverviewView contains scope totals and a compact daily usage trend.
type OverviewView struct {
	Source              usage.SourceKind   `json:"source,omitempty"`
	Sources             []usage.SourceKind `json:"sources,omitempty"`
	Agent               string             `json:"agent,omitempty"`
	Agents              []string           `json:"agents,omitempty"`
	Project             string             `json:"project,omitempty"`
	Period              Period             `json:"period"`
	Sessions            int                `json:"sessions"`
	Turns               int                `json:"turns"`
	UserPrompts         int                `json:"user_prompts"`
	ToolCalls           int                `json:"tool_calls"`
	SkillUses           int                `json:"skill_uses"`
	SkillUsesSession    int                `json:"skill_uses_session"`
	TokenUsage          usage.TokenUsage   `json:"token_usage"`
	TokenUsageAvailable bool               `json:"token_usage_available"`
	Trend               []UsageTrend       `json:"trend,omitempty"`
	Warnings            []usage.Warning    `json:"warnings,omitempty"`
}

// UsageTrend is a daily bucket. Dates are normalized to UTC midnight.
type UsageTrend struct {
	Date                time.Time        `json:"date"`
	Sessions            int              `json:"sessions"`
	Turns               int              `json:"turns"`
	UserPrompts         int              `json:"user_prompts"`
	ToolCalls           int              `json:"tool_calls"`
	SkillUses           int              `json:"skill_uses"`
	TokenUsage          usage.TokenUsage `json:"token_usage"`
	TokenUsageAvailable bool             `json:"token_usage_available"`
}

// ModelSummary is the aggregate row for one provider/model identity.
type ModelSummary struct {
	Model               usage.ModelRef   `json:"model"`
	Sessions            int              `json:"sessions"`
	Turns               int              `json:"turns"`
	UserPrompts         int              `json:"user_prompts"`
	ToolCalls           int              `json:"tool_calls"`
	SkillUses           int              `json:"skill_uses"`
	TokenUsage          usage.TokenUsage `json:"token_usage"`
	TokenUsageAvailable bool             `json:"token_usage_available"`
	FirstUsed           time.Time        `json:"first_used,omitempty"`
	LastUsed            time.Time        `json:"last_used,omitempty"`
}

// ModelDetail adds the sessions in which a model was observed.
type ModelDetail struct {
	Summary  ModelSummary        `json:"summary"`
	Sessions []ModelSessionUsage `json:"sessions,omitempty"`
}

type ModelSessionUsage struct {
	Key        string           `json:"key"`
	ID         string           `json:"id"`
	Title      string           `json:"title,omitempty"`
	Source     usage.SourceKind `json:"source,omitempty"`
	Agent      string           `json:"agent,omitempty"`
	Project    string           `json:"project,omitempty"`
	Turns      int              `json:"turns"`
	TokenUsage usage.TokenUsage `json:"token_usage"`
	FirstUsed  time.Time        `json:"first_used,omitempty"`
	LastUsed   time.Time        `json:"last_used,omitempty"`
}

// SkillSummary is the deduplicated usage row for one skill.
type SkillSummary struct {
	Name         string                            `json:"name"`
	Uses         int                               `json:"uses"`
	Sessions     int                               `json:"sessions"`
	Turns        int                               `json:"turns"`
	Explicit     int                               `json:"explicit"`
	Implicit     int                               `json:"implicit"`
	Unknown      int                               `json:"unknown"`
	Confirmed    int                               `json:"confirmed"`
	Inferred     int                               `json:"inferred"`
	Unconfirmed  int                               `json:"unconfirmed"`
	ModeCounts   map[usage.SkillMode]int           `json:"mode_counts,omitempty"`
	StateCounts  map[usage.SkillState]int          `json:"state_counts,omitempty"`
	MethodCounts map[usage.SkillEvidenceMethod]int `json:"method_counts,omitempty"`
	FirstUsed    time.Time                         `json:"first_used,omitempty"`
	LastUsed     time.Time                         `json:"last_used,omitempty"`
}

// SkillDetail contains bounded per-session usage facts, never the source text
// that caused the evidence to be detected.
type SkillDetail struct {
	Summary  SkillSummary        `json:"summary"`
	Sessions []SkillSessionUsage `json:"sessions,omitempty"`
}

// SkillSessionUsage summarizes one skill's usage within one session.
type SkillSessionUsage struct {
	SessionKey   string                            `json:"session_key"`
	SessionID    string                            `json:"session_id"`
	Title        string                            `json:"title,omitempty"`
	Source       usage.SourceKind                  `json:"source,omitempty"`
	Agent        string                            `json:"agent,omitempty"`
	Project      string                            `json:"project,omitempty"`
	Uses         int                               `json:"uses"`
	Turns        int                               `json:"turns"`
	ModeCounts   map[usage.SkillMode]int           `json:"mode_counts,omitempty"`
	StateCounts  map[usage.SkillState]int          `json:"state_counts,omitempty"`
	MethodCounts map[usage.SkillEvidenceMethod]int `json:"method_counts,omitempty"`
	FirstUsed    time.Time                         `json:"first_used,omitempty"`
	LastUsed     time.Time                         `json:"last_used,omitempty"`
}

// SessionSummary is the safe metadata and aggregate row for one session.
type SessionSummary struct {
	Key                 string           `json:"key"`
	ID                  string           `json:"id"`
	Title               string           `json:"title,omitempty"`
	Source              usage.SourceKind `json:"source,omitempty"`
	Agent               string           `json:"agent,omitempty"`
	Provider            string           `json:"provider,omitempty"`
	ProviderSessionID   string           `json:"provider_session_id,omitempty"`
	CtxSessionID        string           `json:"ctx_session_id,omitempty"`
	ProjectPath         string           `json:"project_path,omitempty"`
	CLIVersion          string           `json:"cli_version,omitempty"`
	CreatedAt           time.Time        `json:"created_at,omitempty"`
	UpdatedAt           time.Time        `json:"updated_at,omitempty"`
	StartedAt           time.Time        `json:"started_at,omitempty"`
	EndedAt             time.Time        `json:"ended_at,omitempty"`
	Models              []usage.ModelRef `json:"models,omitempty"`
	Turns               int              `json:"turns"`
	UserPrompts         int              `json:"user_prompts"`
	ToolCalls           int              `json:"tool_calls"`
	SkillUses           int              `json:"skill_uses"`
	TokenUsage          usage.TokenUsage `json:"token_usage"`
	TokenUsageAvailable bool             `json:"token_usage_available"`
	Aborted             bool             `json:"aborted,omitempty"`
}

// SessionDetail contains chronological, bounded turn summaries.
type SessionDetail struct {
	Summary SessionSummary `json:"summary"`
	Turns   []TurnSummary  `json:"turns,omitempty"`
}

type TurnSummary struct {
	ID                  string           `json:"id"`
	Ordinal             int              `json:"ordinal"`
	StartedAt           time.Time        `json:"started_at,omitempty"`
	EndedAt             time.Time        `json:"ended_at,omitempty"`
	Aborted             bool             `json:"aborted,omitempty"`
	Models              []usage.ModelRef `json:"models,omitempty"`
	TokenUsage          usage.TokenUsage `json:"token_usage"`
	TokenUsageAvailable bool             `json:"token_usage_available"`
	Tools               []string         `json:"tools,omitempty"`
	Skills              []string         `json:"skills,omitempty"`
}

// ReadModel is the complete safe model consumed by renderers. Detail maps
// are intentionally private so marshaling the model cannot expose an
// implementation-only payload.
type ReadModel struct {
	Filter   Filter           `json:"filter"`
	Overview OverviewView     `json:"overview"`
	Models   []ModelSummary   `json:"models,omitempty"`
	Skills   []SkillSummary   `json:"skills,omitempty"`
	Sessions []SessionSummary `json:"sessions,omitempty"`
	Warnings []usage.Warning  `json:"warnings,omitempty"`

	modelDetails   map[string]ModelDetail
	skillDetails   map[string]SkillDetail
	sessionDetails map[string]SessionDetail
}

// SanitizeInput creates the bounded in-memory input suitable for a renderer's
// state. Raw source positions and tool arguments are useful during ingestion
// but are not needed to rebuild this read model.
func SanitizeInput(input Input) Input {
	result := input
	result.Agents = append([]string(nil), input.Agents...)
	result.Sources = append([]usage.SourceKind(nil), input.Sources...)
	result.Warnings = cloneWarnings(input.Warnings)
	result.Sessions = make([]usage.Session, 0, len(input.Sessions))
	for _, session := range input.Sessions {
		session.Source = sanitizedSource(session.Source)
		result.Sessions = append(result.Sessions, session)
	}
	result.Turns = make([]usage.Turn, 0, len(input.Turns))
	for _, turn := range input.Turns {
		turn.Source = sanitizedSource(turn.Source)
		turn.UserPromptTimes = append([]time.Time(nil), turn.UserPromptTimes...)
		modelObservations := turn.ModelObservations
		turn.ModelObservations = make([]usage.ModelObservation, 0, len(modelObservations))
		for _, observation := range modelObservations {
			observation.Source = sanitizedSource(observation.Source)
			turn.ModelObservations = append(turn.ModelObservations, observation)
		}
		turn.ModelTools = sanitizeTools(turn.ModelTools)
		turn.RuntimeTools = sanitizeTools(turn.RuntimeTools)
		skillEvidence := turn.SkillEvidence
		turn.SkillEvidence = make([]usage.SkillEvidence, 0, len(skillEvidence))
		for _, evidence := range skillEvidence {
			evidence.Source = sanitizedSource(evidence.Source)
			turn.SkillEvidence = append(turn.SkillEvidence, evidence)
		}
		if turn.TokenUsage != nil {
			value := *turn.TokenUsage
			turn.TokenUsage = &value
		}
		turn.TokenUsageEvents = append([]usage.TokenUsageEvent(nil), turn.TokenUsageEvents...)
		result.Turns = append(result.Turns, turn)
	}
	return result
}

func sanitizedSource(value usage.SourceRef) usage.SourceRef {
	value.Path = ""
	value.Line = 0
	return value
}

func sanitizeTools(values []usage.ToolObservation) []usage.ToolObservation {
	result := make([]usage.ToolObservation, 0, len(values))
	for _, value := range values {
		value.Arguments = ""
		value.Source = sanitizedSource(value.Source)
		result = append(result, value)
	}
	return result
}

// Build constructs a read model. The optional filter form keeps callers that
// already have an Input.Filter concise while allowing the TUI to replace the
// filter without copying all source data.
func Build(input Input, filters ...Filter) ReadModel {
	filter := input.Filter
	if len(filters) > 0 {
		filter = filters[0]
	}
	if filter.Source == "" && filter.Sources == nil {
		filter.Source = input.Source
	}
	if filter.Sources != nil {
		filter.Source = ""
		if len(filter.Sources) == 1 {
			filter.Source = filter.Sources[0]
		}
	}
	if filter.From.IsZero() && filter.To.IsZero() {
		filter.From, filter.To = input.From, input.To
	}

	turns := deduplicateTurns(input.Turns, filter)
	sessions := deduplicateSessions(input.Sessions, filter)
	index := newSessionIndex(sessions, turns)
	selected := make([]selectedTurn, 0, len(turns))
	for _, turn := range turns {
		key := index.keyForTurn(turn)
		meta := index.sessions[key]
		if !matchesBaseFilter(turn, meta, filter) {
			continue
		}
		filtered, ok := selectTurn(turn, filter)
		if !ok {
			continue
		}
		selected = append(selected, selectedTurn{key: key, turn: filtered})
		index.ensure(key, turn.SessionID, turn.Source)
	}

	selectedBySession := make(map[string][]usage.Turn)
	for _, item := range selected {
		selectedBySession[item.key] = append(selectedBySession[item.key], item.turn)
	}

	modelAccumulators := aggregateModels(selected)
	skillUses := mergeSkills(selected)
	skillAccumulators := aggregateSkills(skillUses)

	readModel := ReadModel{
		Filter:         filter,
		Warnings:       cloneWarnings(input.Warnings),
		modelDetails:   make(map[string]ModelDetail),
		skillDetails:   make(map[string]SkillDetail),
		sessionDetails: make(map[string]SessionDetail),
	}
	readModel.Models = modelRows(modelAccumulators)
	readModel.Skills = skillRows(skillAccumulators)

	includeEmpty := !filter.hasObservationNarrowing()
	sessionKeys := make(map[string]struct{}, len(selectedBySession))
	for key := range selectedBySession {
		sessionKeys[key] = struct{}{}
	}
	for key, meta := range index.sessions {
		if !sessionMatchesFilter(meta, filter) {
			continue
		}
		if includeEmpty || len(selectedBySession[key]) > 0 || (filter.hasPeriod() && sessionMetadataInPeriod(meta, filter)) {
			sessionKeys[key] = struct{}{}
		}
	}
	readModel.Overview = buildOverview(filter, input.Agents, input.Warnings, selected, len(sessionKeys), skillUses)
	readModel.Sessions = sessionRows(index, sessionKeys, selectedBySession, skillUses)
	readModel.buildDetails(index, sessionKeys, selectedBySession, modelAccumulators, skillUses, skillAccumulators)
	return readModel
}

// ModelDetail returns a selected model detail by canonical provider/name key.
func (model ReadModel) ModelDetail(key string) (ModelDetail, bool) {
	value, ok := model.modelDetails[key]
	return value, ok
}

// SkillDetail returns a selected skill detail using case-insensitive lookup.
func (model ReadModel) SkillDetail(name string) (SkillDetail, bool) {
	value, ok := model.skillDetails[strings.ToLower(strings.TrimSpace(name))]
	return value, ok
}

// SessionDetail returns a session detail by source-qualified key.
func (model ReadModel) SessionDetail(key string) (SessionDetail, bool) {
	value, ok := model.sessionDetails[key]
	return value, ok
}

type selectedTurn struct {
	key  string
	turn usage.Turn
}

type sessionIndex struct {
	sessions map[string]usage.Session
	turnKeys map[string]string
}

func newSessionIndex(values []usage.Session, turns []usage.Turn) *sessionIndex {
	index := &sessionIndex{sessions: make(map[string]usage.Session), turnKeys: make(map[string]string, len(turns))}
	for _, value := range values {
		key := value.QualifiedKey()
		if key == "" {
			key = usage.NewSessionKey(value.Source, value.ID)
			value.Key = key
		}
		if previous, ok := index.sessions[key]; ok {
			index.sessions[key] = mergeSession(previous, value)
		} else {
			index.sessions[key] = value
		}
	}
	for _, turn := range turns {
		index.keyForTurn(turn)
	}
	return index
}

func (index *sessionIndex) keyForTurn(turn usage.Turn) string {
	lookup := turnLookupKey(turn)
	if key, ok := index.turnKeys[lookup]; ok {
		return key
	}
	exact := usage.NewSessionKey(turn.Source, turn.SessionID)
	if _, ok := index.sessions[exact]; ok {
		index.turnKeys[lookup] = exact
		return exact
	}
	keys := make([]string, 0, len(index.sessions))
	for key := range index.sessions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if sessionMatchesTurn(index.sessions[key], turn) {
			index.turnKeys[lookup] = key
			return key
		}
	}
	index.ensure(exact, turn.SessionID, turn.Source)
	index.turnKeys[lookup] = exact
	return exact
}

func (index *sessionIndex) ensure(key, id string, source usage.SourceRef) {
	if _, ok := index.sessions[key]; ok {
		return
	}
	index.sessions[key] = usage.NewSession(id, source)
}

func mergeSession(left, right usage.Session) usage.Session {
	result := left
	if result.ID == "" {
		result.ID = right.ID
	}
	if result.Title == "" {
		result.Title = right.Title
	}
	if result.Key == "" {
		result.Key = right.Key
	}
	if result.ProjectPath == "" {
		result.ProjectPath = right.ProjectPath
	}
	if result.CLIVersion == "" {
		result.CLIVersion = right.CLIVersion
	}
	if result.Agent == "" || result.Agent == "unknown" {
		result.Agent = right.Agent
	}
	if result.Provider == "" || result.Provider == "unknown" {
		result.Provider = right.Provider
	}
	if result.ProviderSessionID == "" {
		result.ProviderSessionID = right.ProviderSessionID
	}
	if result.CtxSessionID == "" {
		result.CtxSessionID = right.CtxSessionID
	}
	if result.CreatedAt.IsZero() || (!right.CreatedAt.IsZero() && right.CreatedAt.Before(result.CreatedAt)) {
		result.CreatedAt = right.CreatedAt
	}
	if result.UpdatedAt.IsZero() || right.UpdatedAt.After(result.UpdatedAt) {
		result.UpdatedAt = right.UpdatedAt
	}
	if result.Source.Source == "" {
		result.Source = right.Source
	}
	return result
}

func turnLookupKey(turn usage.Turn) string {
	return usage.NewSessionKey(turn.Source, turn.SessionID) + "\x00" + turn.ID
}

func sessionMatchesTurn(session usage.Session, turn usage.Turn) bool {
	if session.ID == turn.SessionID && sameSource(session.Source.Source, turn.Source.Source) {
		return sameIdentityPart(session.Agent, turn.Source.Agent) && sameIdentityPart(session.Provider, turn.Source.Provider)
	}
	if session.ProviderSessionID != "" && session.ProviderSessionID == turn.Source.ProviderSessionID && sameSource(session.Source.Source, turn.Source.Source) {
		return true
	}
	if session.CtxSessionID != "" && session.CtxSessionID == turn.Source.CtxSessionID && sameSource(session.Source.Source, turn.Source.Source) {
		return true
	}
	return false
}

func sameSource(left, right usage.SourceKind) bool {
	return left == "" || right == "" || left == right
}

func sameIdentityPart(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	return left == "" || left == "unknown" || right == "" || right == "unknown" || strings.EqualFold(left, right)
}

func matchesBaseFilter(turn usage.Turn, session usage.Session, filter Filter) bool {
	source := session.Source.Source
	if source == "" {
		source = turn.Source.Source
	}
	if !sourceMatchesFilter(source, filter) {
		return false
	}
	agent := session.Agent
	if agent == "" {
		agent = turn.Source.Agent
	}
	if filter.Agent != "" && usage.CanonicalAgentID(agent) != usage.CanonicalAgentID(filter.Agent) {
		return false
	}
	if filter.Project != "" && session.ProjectPath != filter.Project {
		return false
	}
	if filter.hasPeriod() && !turnInPeriod(turn, filter) {
		return false
	}
	if filter.Search != "" && !searchMatchesTurn(turn, session, filter.Search) {
		return false
	}
	return true
}

func sessionMatchesFilter(session usage.Session, filter Filter) bool {
	if session.Source.Source != "" && !sourceMatchesFilter(session.Source.Source, filter) {
		return false
	}
	if filter.Agent != "" && usage.CanonicalAgentID(session.Agent) != usage.CanonicalAgentID(filter.Agent) {
		return false
	}
	return filter.Project == "" || session.ProjectPath == filter.Project
}

func sourceMatchesFilter(source usage.SourceKind, filter Filter) bool {
	if source == "" {
		return true
	}
	if filter.Sources != nil {
		for _, selected := range filter.Sources {
			if source == selected {
				return true
			}
		}
		return false
	}
	return filter.Source == "" || source == filter.Source
}

func deduplicateTurns(values []usage.Turn, filter Filter) []usage.Turn {
	result := make([]usage.Turn, 0, len(values))
	positions := make(map[string]int, len(values))
	for _, turn := range values {
		if !sourceMatchesFilter(turn.Source.Source, filter) {
			continue
		}
		key := sharedTurnIdentity(turn)
		if key == "" {
			result = append(result, turn)
			continue
		}
		position, ok := positions[key]
		if !ok {
			positions[key] = len(result)
			result = append(result, turn)
			continue
		}
		if result[position].Source.Source == turn.Source.Source {
			continue
		}
		if preferSource(turn.Source.Source, result[position].Source.Source) {
			result[position] = turn
		}
	}
	return result
}

func deduplicateSessions(values []usage.Session, filter Filter) []usage.Session {
	result := make([]usage.Session, 0, len(values))
	positions := make(map[string]int, len(values))
	for _, session := range values {
		if session.Source.Source != "" && !sourceMatchesFilter(session.Source.Source, filter) {
			continue
		}
		key := sharedSessionIdentity(session)
		if key == "" {
			result = append(result, session)
			continue
		}
		position, ok := positions[key]
		if !ok {
			positions[key] = len(result)
			result = append(result, session)
			continue
		}
		if result[position].Source.Source == session.Source.Source {
			continue
		}
		if preferSource(session.Source.Source, result[position].Source.Source) {
			result[position] = session
		}
	}
	return result
}

func sharedTurnIdentity(turn usage.Turn) string {
	// ponytail: use stable adapter-provided IDs only; add a fuzzy timestamp/content
	// identity when sources expose no shared turn ID without risking false merges.
	identity := strings.TrimSpace(turn.Source.ProviderSessionID)
	if identity == "" {
		identity = strings.TrimSpace(turn.SessionID)
	}
	turnID := strings.TrimSpace(turn.ID)
	if identity == "" || turnID == "" {
		return ""
	}
	return usage.CanonicalAgentID(turn.Source.Agent) + "\x00" + identity + "\x00" + turnID
}

func sharedSessionIdentity(session usage.Session) string {
	identity := strings.TrimSpace(session.ProviderSessionID)
	if identity == "" {
		identity = strings.TrimSpace(session.ID)
	}
	if identity == "" {
		return ""
	}
	return usage.CanonicalAgentID(session.Agent) + "\x00" + identity
}

func preferSource(left, right usage.SourceKind) bool {
	if left == usage.SourceCtx {
		return false
	}
	if right == usage.SourceCtx {
		return true
	}
	return string(left) < string(right)
}

func sessionMetadataInPeriod(session usage.Session, filter Filter) bool {
	if !filter.hasPeriod() {
		return true
	}
	for _, timestamp := range []time.Time{session.CreatedAt, session.UpdatedAt} {
		if accept(timestamp, filter.From, filter.To) {
			return true
		}
	}
	return false
}

func searchMatchesTurn(turn usage.Turn, session usage.Session, value string) bool {
	want := strings.ToLower(strings.TrimSpace(value))
	if want == "" {
		return true
	}
	parts := []string{session.ID, session.Title, session.Key, session.ProjectPath, session.Agent, session.Provider, string(session.Source.Source), turn.SessionID, turn.ID}
	for _, model := range modelsForTurn(turn) {
		parts = append(parts, model.Provider, model.Name)
	}
	for _, evidence := range turn.SkillEvidence {
		parts = append(parts, evidence.SkillName)
	}
	haystack := strings.ToLower(strings.Join(parts, "\x00"))
	return strings.Contains(haystack, want)
}

func (filter Filter) hasPeriod() bool { return !filter.From.IsZero() || !filter.To.IsZero() }

func (filter Filter) hasModel() bool {
	return strings.TrimSpace(filter.ModelKey) != "" || strings.TrimSpace(filter.Model.Provider) != "" || strings.TrimSpace(filter.Model.Name) != "" || strings.TrimSpace(filter.ModelProvider) != "" || strings.TrimSpace(filter.ModelName) != ""
}

func (filter Filter) hasSkill() bool { return strings.TrimSpace(filter.Skill) != "" }

func (filter Filter) hasObservationNarrowing() bool {
	return filter.hasPeriod() || filter.hasModel() || filter.hasSkill() || strings.TrimSpace(filter.Search) != ""
}

func selectTurn(turn usage.Turn, filter Filter) (usage.Turn, bool) {
	if filter.hasModel() && !turnHasModel(turn, filter) {
		return usage.Turn{}, false
	}
	if filter.hasSkill() && !turnHasSkill(turn, filter, filter.Skill) {
		return usage.Turn{}, false
	}
	filtered := turn
	filtered.UserPromptTimes = filterTimes(turn.UserPromptTimes, filter)
	if len(turn.UserPromptTimes) > 0 {
		filtered.UserPrompts = len(filtered.UserPromptTimes)
	} else if filter.hasPeriod() && !accept(turn.StartedAt, filter.From, filter.To) && !accept(turn.EndedAt, filter.From, filter.To) {
		filtered.UserPrompts = 0
	}
	filtered.ModelObservations = filterModels(turn.ModelObservations, filter)
	filtered.ModelTools = filterTools(turn.ModelTools, filter)
	filtered.RuntimeTools = filterTools(turn.RuntimeTools, filter)
	filtered.SkillEvidence = filterSkills(turn.SkillEvidence, filter)
	filtered.StartedAt = filterTimestamp(turn.StartedAt, filter)
	filtered.EndedAt = filterTimestamp(turn.EndedAt, filter)

	events := tokenEvents(turn)
	filtered.TokenUsageEvents = nil
	filtered.TokenUsage = nil
	for _, event := range events {
		model := normalizedModel(event.Model)
		if filter.hasModel() && !matchesModel(model, filter) {
			continue
		}
		if filter.hasPeriod() && !accept(event.Timestamp, filter.From, filter.To) {
			continue
		}
		if len(turn.TokenUsageEvents) > 0 {
			filtered.TokenUsageEvents = append(filtered.TokenUsageEvents, usage.TokenUsageEvent{Timestamp: event.Timestamp, Model: model, Usage: event.Usage})
		}
		if filtered.TokenUsage == nil {
			filtered.TokenUsage = &usage.TokenUsage{}
		}
		filtered.TokenUsage.Add(event.Usage)
	}
	if len(events) == 0 || filtered.TokenUsage == nil {
		if turn.TokenUsage == nil || (filter.hasModel() && !matchesModel(usage.UnknownModel(), filter)) {
			filtered.TokenUsage = nil
		}
	}
	return filtered, true
}

func turnHasModel(turn usage.Turn, filter Filter) bool {
	for _, observation := range turn.ModelObservations {
		if matchesModel(normalizedModel(observation.Model), filter) {
			return true
		}
	}
	for _, event := range tokenEvents(turn) {
		if matchesModel(normalizedModel(event.Model), filter) {
			return true
		}
	}
	return false
}

func turnHasSkill(turn usage.Turn, filter Filter, name string) bool {
	for _, use := range usage.MergeSkillEvidence(turn.SkillEvidence) {
		if strings.EqualFold(use.SkillName, strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

func matchesModel(model usage.ModelRef, filter Filter) bool {
	model = normalizedModel(model)
	if key := strings.TrimSpace(filter.ModelKey); key != "" {
		return model.Key() == strings.ToLower(key)
	}
	wantProvider := strings.ToLower(strings.TrimSpace(filter.ModelProvider))
	wantName := strings.ToLower(strings.TrimSpace(filter.ModelName))
	if filter.Model.Provider != "" {
		wantProvider = strings.ToLower(strings.TrimSpace(filter.Model.Provider))
	}
	if filter.Model.Name != "" {
		wantName = strings.ToLower(strings.TrimSpace(filter.Model.Name))
	}
	if wantProvider != "" && model.Provider != wantProvider {
		return false
	}
	return wantName == "" || model.Name == wantName
}

func normalizedModel(model usage.ModelRef) usage.ModelRef {
	if model == (usage.ModelRef{}) {
		return usage.UnknownModel()
	}
	return usage.NewModelRef(model.Provider, model.Name)
}

func tokenEvents(turn usage.Turn) []usage.TokenUsageEvent {
	if len(turn.TokenUsageEvents) > 0 {
		result := make([]usage.TokenUsageEvent, 0, len(turn.TokenUsageEvents))
		for _, event := range turn.TokenUsageEvents {
			event.Model = normalizedModel(event.Model)
			result = append(result, event)
		}
		return result
	}
	if turn.TokenUsage == nil {
		return nil
	}
	when := turn.EndedAt
	if when.IsZero() {
		when = turn.StartedAt
	}
	return []usage.TokenUsageEvent{{Timestamp: when, Model: usage.UnknownModel(), Usage: *turn.TokenUsage}}
}

func turnInPeriod(turn usage.Turn, filter Filter) bool {
	if !filter.hasPeriod() {
		return true
	}
	for _, timestamp := range turnTimestamps(turn) {
		if accept(timestamp, filter.From, filter.To) {
			return true
		}
	}
	return false
}

func turnTimestamps(turn usage.Turn) []time.Time {
	result := []time.Time{turn.StartedAt, turn.EndedAt}
	result = append(result, turn.UserPromptTimes...)
	for _, observation := range turn.ModelObservations {
		result = append(result, observation.Timestamp)
	}
	for _, tool := range append(append([]usage.ToolObservation{}, turn.ModelTools...), turn.RuntimeTools...) {
		result = append(result, tool.Timestamp)
	}
	for _, skill := range turn.SkillEvidence {
		result = append(result, skill.Timestamp)
	}
	for _, event := range tokenEvents(turn) {
		result = append(result, event.Timestamp)
	}
	return result
}

func accept(timestamp, from, to time.Time) bool {
	if timestamp.IsZero() {
		return false
	}
	if !from.IsZero() && timestamp.Before(from) {
		return false
	}
	return to.IsZero() || timestamp.Before(to)
}

func filterTimestamp(timestamp time.Time, filter Filter) time.Time {
	if !filter.hasPeriod() || accept(timestamp, filter.From, filter.To) {
		return timestamp
	}
	return time.Time{}
}

func filterTimes(values []time.Time, filter Filter) []time.Time {
	result := make([]time.Time, 0, len(values))
	for _, value := range values {
		if !filter.hasPeriod() || accept(value, filter.From, filter.To) {
			result = append(result, value)
		}
	}
	return result
}

func filterModels(values []usage.ModelObservation, filter Filter) []usage.ModelObservation {
	result := make([]usage.ModelObservation, 0, len(values))
	for _, value := range values {
		if filter.hasModel() && !matchesModel(value.Model, filter) {
			continue
		}
		if filter.hasPeriod() && !accept(value.Timestamp, filter.From, filter.To) {
			continue
		}
		value.Model = normalizedModel(value.Model)
		result = append(result, value)
	}
	return result
}

func filterTools(values []usage.ToolObservation, filter Filter) []usage.ToolObservation {
	result := make([]usage.ToolObservation, 0, len(values))
	for _, value := range values {
		if filter.hasPeriod() && !accept(value.Timestamp, filter.From, filter.To) {
			continue
		}
		value.Arguments = ""
		result = append(result, value)
	}
	return result
}

func filterSkills(values []usage.SkillEvidence, filter Filter) []usage.SkillEvidence {
	result := make([]usage.SkillEvidence, 0, len(values))
	for _, value := range values {
		if filter.hasSkill() && !strings.EqualFold(value.SkillName, strings.TrimSpace(filter.Skill)) {
			continue
		}
		if filter.Strict && value.State != usage.StateConfirmed {
			continue
		}
		if filter.hasPeriod() && !accept(value.Timestamp, filter.From, filter.To) {
			continue
		}
		result = append(result, value)
	}
	return result
}

func buildOverview(filter Filter, agents []string, warnings []usage.Warning, selected []selectedTurn, sessionCount int, skillUses []usage.SkillUse) OverviewView {
	view := OverviewView{Source: filter.Source, Sources: append([]usage.SourceKind(nil), filter.Sources...), Project: filter.Project, Period: selectedPeriod(selected), Warnings: cloneWarnings(warnings)}
	view.Agents = overviewAgents(filter, agents, selected)
	view.Agent = strings.Join(view.Agents, ",")
	if view.Source == "" && len(view.Sources) == 0 && len(selected) > 0 {
		view.Source = selected[0].turn.Source.Source
	}
	if view.Source == "" && len(view.Sources) == 1 {
		view.Source = view.Sources[0]
	}
	sessions := make(map[string]struct{})
	trend := make(map[time.Time]*trendAccumulator)
	for _, item := range selected {
		turn := item.turn
		view.Turns++
		sessions[item.key] = struct{}{}
		view.UserPrompts += turn.UserPrompts
		effective := usage.EffectiveTools(turn)
		view.ToolCalls += len(effective)
		for _, event := range tokenEvents(turn) {
			view.TokenUsageAvailable = true
			view.TokenUsage.Add(event.Usage)
		}
		bucket := turnBucket(turn)
		if bucket.IsZero() {
			continue
		}
		current := trend[bucket]
		if current == nil {
			current = &trendAccumulator{date: bucket, sessions: make(map[string]struct{})}
			trend[bucket] = current
		}
		current.sessions[item.key] = struct{}{}
		current.turns++
		current.prompts += turn.UserPrompts
		current.tools += len(effective)
		for _, event := range tokenEvents(turn) {
			current.tokens.Add(event.Usage)
			current.tokenAvailable = true
		}
	}
	view.Sessions = sessionCount
	if view.Sessions == 0 {
		view.Sessions = len(sessions)
	}
	view.SkillUses = len(skillUses)
	view.SkillUsesSession = skillUsesBySession(skillUses)
	trendFrom, trendTo := trendBounds(view.Period, trend)
	for date := trendFrom; !date.IsZero() && !date.After(trendTo); date = date.AddDate(0, 0, 1) {
		value := trend[date]
		if value == nil {
			view.Trend = append(view.Trend, UsageTrend{Date: date})
			continue
		}
		view.Trend = append(view.Trend, UsageTrend{Date: date, Sessions: len(value.sessions), Turns: value.turns, UserPrompts: value.prompts, ToolCalls: value.tools, TokenUsage: value.tokens, TokenUsageAvailable: value.tokenAvailable})
	}
	return view
}

func trendBounds(period Period, trend map[time.Time]*trendAccumulator) (from, to time.Time) {
	for date := range trend {
		if from.IsZero() || date.Before(from) {
			from = date
		}
		if to.IsZero() || date.After(to) {
			to = date
		}
	}
	if !period.From.IsZero() {
		from = dateBucket(period.From)
	}
	if !period.To.IsZero() {
		to = dateBucket(period.To)
	}
	return from, to
}

func skillUsesBySession(uses []usage.SkillUse) int {
	seen := make(map[string]struct{}, len(uses))
	for _, use := range uses {
		key := sessionKeyFromUse(use) + "\x00" + use.SkillName
		seen[key] = struct{}{}
	}
	return len(seen)
}

func overviewAgents(filter Filter, agents []string, selected []selectedTurn) []string {
	var values []string
	if filter.Agent != "" {
		values = []string{filter.Agent}
	} else if filter.hasObservationNarrowing() || strings.TrimSpace(filter.Project) != "" {
		for _, item := range selected {
			values = append(values, item.turn.Source.Agent)
		}
	} else {
		values = append(values, agents...)
	}
	if len(values) == 0 {
		for _, item := range selected {
			values = append(values, item.turn.Source.Agent)
		}
	}
	if len(values) == 0 && (filter.Source == usage.SourceCodex || sourceListContains(filter.Sources, usage.SourceCodex)) {
		values = []string{"codex"}
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		id := usage.CanonicalAgentID(value)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	sort.Strings(result)
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

func selectedPeriod(selected []selectedTurn) Period {
	var from, to time.Time
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
	for _, item := range selected {
		for _, timestamp := range turnTimestamps(item.turn) {
			add(timestamp)
		}
	}
	return Period{From: from, To: to}
}

type trendAccumulator struct {
	date           time.Time
	sessions       map[string]struct{}
	turns          int
	prompts        int
	tools          int
	tokens         usage.TokenUsage
	tokenAvailable bool
}

func turnBucket(turn usage.Turn) time.Time {
	when := turn.StartedAt
	if when.IsZero() {
		when = turn.EndedAt
	}
	if when.IsZero() {
		for _, timestamp := range turnTimestamps(turn) {
			if !timestamp.IsZero() && (when.IsZero() || timestamp.Before(when)) {
				when = timestamp
			}
		}
	}
	if when.IsZero() {
		return time.Time{}
	}
	return dateBucket(when)
}

func dateBucket(when time.Time) time.Time {
	when = when.UTC()
	return time.Date(when.Year(), when.Month(), when.Day(), 0, 0, 0, 0, time.UTC)
}

func aggregateModels(selected []selectedTurn) map[string]*modelAccumulator {
	result := make(map[string]*modelAccumulator)
	for _, item := range selected {
		turn := item.turn
		models := modelsForTurn(turn)
		if len(models) == 0 {
			continue
		}
		uses := usage.MergeSkillEvidence(turn.SkillEvidence)
		effectiveTools := usage.EffectiveTools(turn)
		for _, model := range models {
			key := model.Key()
			current := result[key]
			if current == nil {
				current = &modelAccumulator{model: model, sessions: make(map[string]struct{}), turns: make(map[string]struct{})}
				result[key] = current
			}
			current.sessions[item.key] = struct{}{}
			current.turns[item.key+"\x00"+turn.ID] = struct{}{}
			current.prompts += turn.UserPrompts
			current.tools += len(effectiveTools)
			current.skills += len(uses)
			for _, observation := range turn.ModelObservations {
				if normalizedModel(observation.Model).Key() == key {
					current.observe(observation.Timestamp, false, usage.TokenUsage{})
				}
			}
			for _, event := range tokenEvents(turn) {
				if normalizedModel(event.Model).Key() != key {
					continue
				}
				current.observe(event.Timestamp, true, event.Usage)
			}
		}
	}
	return result
}

func modelsForTurn(turn usage.Turn) []usage.ModelRef {
	seen := make(map[string]usage.ModelRef)
	for _, observation := range turn.ModelObservations {
		model := normalizedModel(observation.Model)
		seen[model.Key()] = model
	}
	for _, event := range tokenEvents(turn) {
		model := normalizedModel(event.Model)
		seen[model.Key()] = model
	}
	result := make([]usage.ModelRef, 0, len(seen))
	for _, model := range seen {
		result = append(result, model)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key() < result[j].Key() })
	return result
}

type modelAccumulator struct {
	model    usage.ModelRef
	sessions map[string]struct{}
	turns    map[string]struct{}
	prompts  int
	tools    int
	skills   int
	tokens   usage.TokenUsage
	tokenSet bool
	first    time.Time
	last     time.Time
}

func (value *modelAccumulator) observe(timestamp time.Time, hasToken bool, tokens usage.TokenUsage) {
	if hasToken {
		value.tokens.Add(tokens)
		value.tokenSet = true
	}
	if timestamp.IsZero() {
		return
	}
	if value.first.IsZero() || timestamp.Before(value.first) {
		value.first = timestamp
	}
	if value.last.IsZero() || timestamp.After(value.last) {
		value.last = timestamp
	}
}

func modelRows(values map[string]*modelAccumulator) []ModelSummary {
	result := make([]ModelSummary, 0, len(values))
	for _, value := range values {
		result = append(result, ModelSummary{Model: value.model, Sessions: len(value.sessions), Turns: len(value.turns), UserPrompts: value.prompts, ToolCalls: value.tools, SkillUses: value.skills, TokenUsage: value.tokens, TokenUsageAvailable: value.tokenSet, FirstUsed: value.first, LastUsed: value.last})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Turns != result[j].Turns {
			return result[i].Turns > result[j].Turns
		}
		if result[i].Sessions != result[j].Sessions {
			return result[i].Sessions > result[j].Sessions
		}
		if !result[i].LastUsed.Equal(result[j].LastUsed) {
			return result[i].LastUsed.After(result[j].LastUsed)
		}
		return result[i].Model.Key() < result[j].Model.Key()
	})
	return result
}

func mergeSkills(selected []selectedTurn) []usage.SkillUse {
	evidence := make([]usage.SkillEvidence, 0)
	for _, item := range selected {
		evidence = append(evidence, item.turn.SkillEvidence...)
	}
	return usage.MergeSkillEvidence(evidence)
}

type skillAccumulator struct {
	name         string
	uses         []usage.SkillUse
	sessions     map[string]struct{}
	turns        map[string]struct{}
	modeCounts   map[usage.SkillMode]int
	stateCounts  map[usage.SkillState]int
	methodCounts map[usage.SkillEvidenceMethod]int
	first        time.Time
	last         time.Time
}

func aggregateSkills(values []usage.SkillUse) map[string]*skillAccumulator {
	result := make(map[string]*skillAccumulator)
	for _, use := range values {
		key := strings.ToLower(strings.TrimSpace(use.SkillName))
		current := result[key]
		if current == nil {
			current = &skillAccumulator{name: use.SkillName, sessions: make(map[string]struct{}), turns: make(map[string]struct{}), modeCounts: make(map[usage.SkillMode]int), stateCounts: make(map[usage.SkillState]int), methodCounts: make(map[usage.SkillEvidenceMethod]int)}
			result[key] = current
		}
		current.uses = append(current.uses, use)
		current.sessions[sessionKeyFromUse(use)] = struct{}{}
		current.turns[sessionKeyFromUse(use)+"\x00"+use.TurnID] = struct{}{}
		for _, mode := range useModes(use) {
			current.modeCounts[mode]++
			switch mode {
			case usage.ModeExplicit:
				// Kept in the map; row fields are populated below.
			case usage.ModeImplicit:
			}
		}
		current.stateCounts[use.State]++
		for _, method := range use.Methods {
			current.methodCounts[method]++
		}
		if current.first.IsZero() || (!use.Timestamp.IsZero() && use.Timestamp.Before(current.first)) {
			current.first = use.Timestamp
		}
		if current.last.IsZero() || use.Timestamp.After(current.last) {
			current.last = use.Timestamp
		}
	}
	return result
}

func skillRows(values map[string]*skillAccumulator) []SkillSummary {
	result := make([]SkillSummary, 0, len(values))
	for _, value := range values {
		row := SkillSummary{Name: value.name, Uses: len(value.uses), Sessions: len(value.sessions), Turns: len(value.turns), ModeCounts: cloneModeCounts(value.modeCounts), StateCounts: cloneStateCounts(value.stateCounts), MethodCounts: cloneMethodCounts(value.methodCounts), FirstUsed: value.first, LastUsed: value.last}
		row.Explicit = value.modeCounts[usage.ModeExplicit]
		row.Implicit = value.modeCounts[usage.ModeImplicit]
		row.Unknown = value.modeCounts[usage.ModeUnknown]
		row.Confirmed = value.stateCounts[usage.StateConfirmed]
		row.Inferred = value.stateCounts[usage.StateInferred]
		row.Unconfirmed = value.stateCounts[usage.StateUnconfirmed]
		result = append(result, row)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Uses != result[j].Uses {
			return result[i].Uses > result[j].Uses
		}
		if !result[i].LastUsed.Equal(result[j].LastUsed) {
			return result[i].LastUsed.After(result[j].LastUsed)
		}
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})
	return result
}

func useModes(use usage.SkillUse) []usage.SkillMode {
	if len(use.Modes) > 0 {
		return append([]usage.SkillMode(nil), use.Modes...)
	}
	if use.Mode == "" {
		return []usage.SkillMode{usage.ModeUnknown}
	}
	return []usage.SkillMode{use.Mode}
}

func cloneModeCounts(values map[usage.SkillMode]int) map[usage.SkillMode]int {
	result := make(map[usage.SkillMode]int, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func cloneStateCounts(values map[usage.SkillState]int) map[usage.SkillState]int {
	result := make(map[usage.SkillState]int, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func cloneMethodCounts(values map[usage.SkillEvidenceMethod]int) map[usage.SkillEvidenceMethod]int {
	result := make(map[usage.SkillEvidenceMethod]int, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func sessionKeyFromUse(use usage.SkillUse) string {
	return usage.NewSessionKey(use.Source, use.SessionID)
}

func sessionRows(index *sessionIndex, keys map[string]struct{}, turns map[string][]usage.Turn, skillUses []usage.SkillUse) []SessionSummary {
	skillCounts := make(map[string]int)
	for _, use := range skillUses {
		skillCounts[sessionKeyFromUse(use)]++
	}
	result := make([]SessionSummary, 0, len(keys))
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	for _, key := range ordered {
		meta := index.sessions[key]
		value := SessionSummary{Key: key, ID: meta.ID, Title: meta.Title, Source: meta.Source.Source, Agent: usage.CanonicalAgentID(meta.Agent), Provider: meta.Provider, ProviderSessionID: meta.ProviderSessionID, CtxSessionID: meta.CtxSessionID, ProjectPath: meta.ProjectPath, CLIVersion: meta.CLIVersion, CreatedAt: meta.CreatedAt, UpdatedAt: meta.UpdatedAt, SkillUses: skillCounts[key]}
		if value.ID == "" {
			value.ID = key
		}
		if value.Source == "" {
			value.Source = firstTurnSource(turns[key]).Source
		}
		if value.Agent == "unknown" {
			value.Agent = usage.CanonicalAgentID(firstTurnSource(turns[key]).Agent)
		}
		if value.Provider == "" {
			value.Provider = firstTurnSource(turns[key]).Provider
		}
		models := make(map[string]usage.ModelRef)
		for _, turn := range turns[key] {
			value.Turns++
			value.UserPrompts += turn.UserPrompts
			value.ToolCalls += len(usage.EffectiveTools(turn))
			if turn.Aborted {
				value.Aborted = true
			}
			for _, model := range modelsForTurn(turn) {
				models[model.Key()] = model
			}
			for _, event := range tokenEvents(turn) {
				value.TokenUsageAvailable = true
				value.TokenUsage.Add(event.Usage)
			}
		}
		value.StartedAt, value.EndedAt = usage.SessionTimeRange(meta, turns[key])
		value.Models = sortedModels(models)
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		if !result[i].EndedAt.Equal(result[j].EndedAt) {
			return result[i].EndedAt.After(result[j].EndedAt)
		}
		return result[i].Key < result[j].Key
	})
	return result
}

func firstTurnSource(turns []usage.Turn) usage.SourceRef {
	if len(turns) == 0 {
		return usage.SourceRef{}
	}
	return turns[0].Source
}

func sortedModels(values map[string]usage.ModelRef) []usage.ModelRef {
	result := make([]usage.ModelRef, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key() < result[j].Key() })
	return result
}

func (model *ReadModel) buildDetails(index *sessionIndex, sessionKeys map[string]struct{}, turns map[string][]usage.Turn, models map[string]*modelAccumulator, skillUses []usage.SkillUse, skills map[string]*skillAccumulator) {
	for key, value := range models {
		detail := ModelDetail{Summary: ModelSummary{Model: value.model, Sessions: len(value.sessions), Turns: len(value.turns), UserPrompts: value.prompts, ToolCalls: value.tools, SkillUses: value.skills, TokenUsage: value.tokens, TokenUsageAvailable: value.tokenSet, FirstUsed: value.first, LastUsed: value.last}}
		keys := make([]string, 0, len(value.sessions))
		for sessionKey := range value.sessions {
			keys = append(keys, sessionKey)
		}
		sort.Strings(keys)
		for _, sessionKey := range keys {
			meta := index.sessions[sessionKey]
			row := ModelSessionUsage{Key: sessionKey, ID: meta.ID, Title: meta.Title, Source: meta.Source.Source, Agent: usage.CanonicalAgentID(meta.Agent), Project: meta.ProjectPath}
			for _, turn := range turns[sessionKey] {
				if !turnIncludesModel(turn, value.model) {
					continue
				}
				row.Turns++
				for _, event := range tokenEvents(turn) {
					if normalizedModel(event.Model).Key() == key {
						row.TokenUsage.Add(event.Usage)
					}
				}
				for _, timestamp := range modelTimestamps(turn, value.model) {
					if row.FirstUsed.IsZero() || (!timestamp.IsZero() && timestamp.Before(row.FirstUsed)) {
						row.FirstUsed = timestamp
					}
					if row.LastUsed.IsZero() || timestamp.After(row.LastUsed) {
						row.LastUsed = timestamp
					}
				}
			}
			detail.Sessions = append(detail.Sessions, row)
		}
		sort.Slice(detail.Sessions, func(i, j int) bool {
			if !detail.Sessions[i].LastUsed.Equal(detail.Sessions[j].LastUsed) {
				return detail.Sessions[i].LastUsed.After(detail.Sessions[j].LastUsed)
			}
			if detail.Sessions[i].Turns != detail.Sessions[j].Turns {
				return detail.Sessions[i].Turns > detail.Sessions[j].Turns
			}
			return detail.Sessions[i].Key < detail.Sessions[j].Key
		})
		model.modelDetails[key] = detail
	}

	for key, value := range skills {
		detail := SkillDetail{Summary: skillRows(map[string]*skillAccumulator{key: value})[0]}
		grouped := make(map[string]*SkillSessionUsage)
		turnsBySession := make(map[string]map[string]struct{})
		for _, use := range value.uses {
			sessionKey := sessionKeyFromUse(use)
			if _, ok := index.sessions[sessionKey]; !ok {
				for candidate, meta := range index.sessions {
					if sessionMatchesUse(meta, use) {
						sessionKey = candidate
						break
					}
				}
			}
			meta := index.sessions[sessionKey]
			row := grouped[sessionKey]
			if row == nil {
				source := meta.Source.Source
				if source == "" {
					source = use.Source.Source
				}
				sessionID := meta.ID
				if sessionID == "" {
					sessionID = use.SessionID
				}
				agent := meta.Agent
				if agent == "" {
					agent = use.Source.Agent
				}
				row = &SkillSessionUsage{
					SessionKey:   sessionKey,
					SessionID:    sessionID,
					Title:        meta.Title,
					Source:       source,
					Agent:        usage.CanonicalAgentID(agent),
					Project:      meta.ProjectPath,
					ModeCounts:   make(map[usage.SkillMode]int),
					StateCounts:  make(map[usage.SkillState]int),
					MethodCounts: make(map[usage.SkillEvidenceMethod]int),
				}
				grouped[sessionKey] = row
				turnsBySession[sessionKey] = make(map[string]struct{})
			}
			row.Uses++
			if _, ok := turnsBySession[sessionKey][use.TurnID]; !ok {
				turnsBySession[sessionKey][use.TurnID] = struct{}{}
				row.Turns++
			}
			for _, mode := range useModes(use) {
				row.ModeCounts[mode]++
			}
			row.StateCounts[use.State]++
			for _, method := range use.Methods {
				row.MethodCounts[method]++
			}
			if row.FirstUsed.IsZero() || (!use.Timestamp.IsZero() && use.Timestamp.Before(row.FirstUsed)) {
				row.FirstUsed = use.Timestamp
			}
			if row.LastUsed.IsZero() || use.Timestamp.After(row.LastUsed) {
				row.LastUsed = use.Timestamp
			}
		}
		orderedSessions := make([]string, 0, len(grouped))
		for sessionKey := range grouped {
			orderedSessions = append(orderedSessions, sessionKey)
		}
		sort.Slice(orderedSessions, func(i, j int) bool {
			left, right := grouped[orderedSessions[i]], grouped[orderedSessions[j]]
			if !left.LastUsed.Equal(right.LastUsed) {
				return left.LastUsed.After(right.LastUsed)
			}
			if !left.FirstUsed.Equal(right.FirstUsed) {
				return left.FirstUsed.After(right.FirstUsed)
			}
			return left.SessionKey < right.SessionKey
		})
		for _, sessionKey := range orderedSessions {
			detail.Sessions = append(detail.Sessions, *grouped[sessionKey])
		}
		model.skillDetails[key] = detail
	}

	for _, session := range sessionRows(index, sessionKeys, turns, skillUses) {
		detail := SessionDetail{Summary: session}
		values := append([]usage.Turn(nil), turns[session.Key]...)
		sort.SliceStable(values, func(i, j int) bool {
			if values[i].StartedAt.Equal(values[j].StartedAt) {
				if values[i].Ordinal != values[j].Ordinal {
					return values[i].Ordinal < values[j].Ordinal
				}
				return values[i].ID < values[j].ID
			}
			return values[i].StartedAt.Before(values[j].StartedAt)
		})
		for _, turn := range values {
			toolNames := make(map[string]struct{})
			for _, tool := range usage.EffectiveTools(turn) {
				name := tool.CanonicalName
				if name == "" {
					name = tool.RawName
				}
				if name != "" {
					toolNames[name] = struct{}{}
				}
			}
			skillNames := make(map[string]struct{})
			for _, use := range usage.MergeSkillEvidence(turn.SkillEvidence) {
				skillNames[use.SkillName] = struct{}{}
			}
			detail.Turns = append(detail.Turns, TurnSummary{ID: turn.ID, Ordinal: turn.Ordinal, StartedAt: turn.StartedAt, EndedAt: turn.EndedAt, Aborted: turn.Aborted, Models: modelsForTurn(turn), TokenUsage: tokenUsageForTurn(turn), TokenUsageAvailable: len(tokenEvents(turn)) > 0, Tools: sortedStrings(toolNames), Skills: sortedStrings(skillNames)})
		}
		model.sessionDetails[session.Key] = detail
	}
}

func sessionMatchesUse(session usage.Session, use usage.SkillUse) bool {
	return session.ID == use.SessionID && sameSource(session.Source.Source, use.Source.Source)
}

func turnIncludesModel(turn usage.Turn, model usage.ModelRef) bool {
	for _, candidate := range modelsForTurn(turn) {
		if candidate.Key() == model.Key() {
			return true
		}
	}
	return false
}

func modelTimestamps(turn usage.Turn, model usage.ModelRef) []time.Time {
	result := make([]time.Time, 0)
	for _, observation := range turn.ModelObservations {
		if normalizedModel(observation.Model).Key() == model.Key() {
			result = append(result, observation.Timestamp)
		}
	}
	for _, event := range tokenEvents(turn) {
		if normalizedModel(event.Model).Key() == model.Key() {
			result = append(result, event.Timestamp)
		}
	}
	return result
}

func tokenUsageForTurn(turn usage.Turn) usage.TokenUsage {
	result := usage.TokenUsage{}
	for _, event := range tokenEvents(turn) {
		result.Add(event.Usage)
	}
	return result
}

func sortedStrings(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func cloneWarnings(values []usage.Warning) []usage.Warning {
	result := make([]usage.Warning, 0, len(values))
	for _, value := range values {
		value.Path = ""
		value.Line = 0
		result = append(result, value)
	}
	return result
}
