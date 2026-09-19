package usage

import (
	"sort"
	"strings"
	"time"
	"unicode"
)

// SourceKind identifies the history source selected for an invocation.
type SourceKind string

const (
	SourceCodex    SourceKind = "codex"
	SourceCtx      SourceKind = "ctx"
	SourceOpenCode SourceKind = "opencode"
	SourceCopilot  SourceKind = "copilot"
)

// AllSourceKinds returns sources in stable display order.
func AllSourceKinds() []SourceKind {
	return []SourceKind{SourceCodex, SourceCopilot, SourceOpenCode, SourceCtx}
}

// DefaultSourceKinds returns source kinds eligible for automatic discovery.
// The caller omits sources that are unavailable or have no history. ctx is
// intentionally excluded because it is a cross-agent event stream.
func DefaultSourceKinds() []SourceKind {
	result := make([]SourceKind, 0, len(AllSourceKinds()))
	for _, source := range AllSourceKinds() {
		if source != SourceCtx {
			result = append(result, source)
		}
	}
	return result
}

func (s SourceKind) Valid() bool {
	return s == SourceCodex || s == SourceCtx || s == SourceOpenCode || s == SourceCopilot
}

// CanonicalAgentID returns the stable, lower-case identifier used for
// cross-agent identity and sorting. Provider names are intentionally kept
// recognizable instead of being mapped to a closed list.
func CanonicalAgentID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "unknown"
	}
	var result strings.Builder
	separator := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if separator && result.Len() > 0 {
				result.WriteByte('-')
			}
			result.WriteRune(r)
			separator = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			separator = result.Len() > 0
		}
	}
	if result.Len() == 0 {
		return "unknown"
	}
	return result.String()
}

// AgentDisplayName returns the stable human-facing name for a canonical ID.
func AgentDisplayName(value string) string {
	id := CanonicalAgentID(value)
	switch id {
	case "codex":
		return "Codex"
	case "opencode":
		return "OpenCode"
	case "claude-code":
		return "Claude Code"
	case "copilot":
		return "GitHub Copilot"
	case "unknown":
		return "unknown"
	}
	parts := strings.Split(id, "-")
	for i, part := range parts {
		if part == "" {
			continue
		}
		runes := []rune(part)
		parts[i] = strings.ToUpper(string(runes[0])) + string(runes[1:])
	}
	return strings.Join(parts, " ")
}

// SourceRef identifies the immutable source position of an observation.
type SourceRef struct {
	Path              string     `json:"path"`
	Line              int        `json:"line"`
	CLIVersion        string     `json:"cli_version,omitempty"`
	Source            SourceKind `json:"source,omitempty"`
	Agent             string     `json:"agent,omitempty"`
	Provider          string     `json:"provider,omitempty"`
	ProviderSessionID string     `json:"provider_session_id,omitempty"`
	CtxSessionID      string     `json:"ctx_session_id,omitempty"`
	EventID           string     `json:"event_id,omitempty"`
}

// ModelRef identifies the model reported by a history source. The values are
// canonicalized so model rows can be grouped without relying on display text.
type ModelRef struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
}

const unknownModelPart = "unknown"

// NewModelRef returns a stable model identity. Unknown provider or name is
// retained explicitly instead of dropping usage from model aggregates.
func NewModelRef(provider, name string) ModelRef {
	provider = canonicalModelPart(provider)
	name = canonicalModelPart(name)
	if provider == "" {
		provider = unknownModelPart
	}
	if name == "" {
		name = unknownModelPart
	}
	return ModelRef{Provider: provider, Name: name}
}

func UnknownModel() ModelRef { return ModelRef{Provider: unknownModelPart, Name: unknownModelPart} }

func (model ModelRef) Key() string {
	model = NewModelRef(model.Provider, model.Name)
	return model.Provider + "\x00" + model.Name
}

func canonicalModelPart(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// ModelFromValues finds a model in source payload values without exposing the
// payload itself to downstream consumers. Nested provider payloads are
// inspected because adapters receive both wrapper and provider-native shapes.
func ModelFromValues(values []any, fallbackProvider string) (ModelRef, bool) {
	for _, value := range values {
		if object, ok := value.(map[string]any); ok {
			if model, found := ModelFromMap(object, fallbackProvider); found {
				return model, true
			}
		}
	}
	return ModelRef{}, false
}

func ModelFromMap(value map[string]any, fallbackProvider string) (ModelRef, bool) {
	if len(value) == 0 {
		return ModelRef{}, false
	}
	provider := firstModelString(value, "provider", "provider_name", "providerName", "provider_id", "providerId", "providerID", "model_provider", "modelProvider", "model_provider_id", "modelProviderId", "vendor")
	if model, factProvider, found := modelFactValue(value); found {
		if provider == "" {
			provider = factProvider
		}
		if provider == "" {
			provider = fallbackProvider
		}
		return NewModelRef(provider, model), true
	}
	for _, key := range []string{"model", "model_name", "modelName", "model_id", "modelId", "modelID"} {
		raw, ok := value[key]
		if !ok {
			continue
		}
		name, nestedProvider := modelValue(raw)
		if name != "" {
			if provider == "" {
				provider = nestedProvider
			}
			if provider == "" {
				provider = fallbackProvider
			}
			return NewModelRef(provider, name), true
		}
	}
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		switch typed := value[key].(type) {
		case map[string]any:
			if model, found := ModelFromMap(typed, fallbackProvider); found {
				return model, true
			}
		case []any:
			if model, found := ModelFromValues(typed, fallbackProvider); found {
				return model, true
			}
		}
	}
	return ModelRef{}, false
}

func modelFactValue(value map[string]any) (name, provider string, found bool) {
	kind := compactModelKind(firstModelString(value, "kind", "fact_kind", "factKind", "type"))
	switch kind {
	case "model", "modelname", "modelid":
		name, provider = modelValue(value["value"])
		return name, provider, name != ""
	default:
		return "", "", false
	}
}

func compactModelKind(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, "-", "")
	value = strings.ReplaceAll(value, " ", "")
	return value
}

func firstModelString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if text, ok := value[key].(string); ok && strings.TrimSpace(text) != "" {
			return text
		}
	}
	return ""
}

func modelValue(value any) (name, provider string) {
	switch typed := value.(type) {
	case string:
		return typed, ""
	case map[string]any:
		return firstModelString(typed, "name", "id", "model", "model_name", "modelName", "model_id", "modelId", "modelID"), firstModelString(typed, "provider", "provider_name", "providerName", "provider_id", "providerId", "providerID", "model_provider", "modelProvider", "model_provider_id", "modelProviderId", "vendor")
	default:
		return "", ""
	}
}

// Session is the source-neutral metadata used to identify a conversation in
// detail views and cache snapshots.
type Session struct {
	ID                string    `json:"id"`
	Title             string    `json:"title,omitempty"`
	Key               string    `json:"key,omitempty"`
	ProjectName       string    `json:"project_name,omitempty"`
	ProjectPath       string    `json:"project_path,omitempty"`
	CLIVersion        string    `json:"cli_version,omitempty"`
	Agent             string    `json:"agent,omitempty"`
	Provider          string    `json:"provider,omitempty"`
	ProviderSessionID string    `json:"provider_session_id,omitempty"`
	CtxSessionID      string    `json:"ctx_session_id,omitempty"`
	CreatedAt         time.Time `json:"created_at,omitempty"`
	UpdatedAt         time.Time `json:"updated_at,omitempty"`
	Source            SourceRef `json:"source"`
}

func NewSession(id string, source SourceRef) Session {
	session := Session{
		ID:                strings.TrimSpace(id),
		ProjectPath:       "",
		CLIVersion:        source.CLIVersion,
		Agent:             CanonicalAgentID(source.Agent),
		Provider:          canonicalModelPart(source.Provider),
		ProviderSessionID: strings.TrimSpace(source.ProviderSessionID),
		CtxSessionID:      strings.TrimSpace(source.CtxSessionID),
		Source:            source,
	}
	if session.ID == "" {
		session.ID = "unknown"
	}
	if session.Agent == "" {
		session.Agent = unknownModelPart
	}
	if session.Provider == "" {
		session.Provider = unknownModelPart
	}
	session.Key = NewSessionKey(source, session.ID)
	return session
}

// NewSessionKey uses source-native IDs when available and falls back to the
// normalized display ID. Source and Agent are always part of the identity.
func NewSessionKey(source SourceRef, id string) string {
	sourceName := strings.TrimSpace(string(source.Source))
	if sourceName == "" {
		sourceName = unknownModelPart
	}
	agent := CanonicalAgentID(source.Agent)
	provider := canonicalModelPart(source.Provider)
	if provider == "" {
		provider = unknownModelPart
	}
	identity := strings.TrimSpace(id)
	if source.ProviderSessionID != "" || source.CtxSessionID != "" {
		identity = strings.Join([]string{strings.TrimSpace(source.ProviderSessionID), strings.TrimSpace(source.CtxSessionID)}, "\x00")
	}
	if identity == "" {
		identity = strings.TrimSpace(source.EventID)
	}
	if identity == "" {
		identity = unknownModelPart
	}
	return strings.Join([]string{sourceName, agent, provider, identity}, "\x00")
}

func (session Session) QualifiedKey() string {
	if strings.TrimSpace(session.Key) != "" {
		return session.Key
	}
	return NewSessionKey(session.Source, session.ID)
}

// SessionTimeRange returns the best available time range for a session. When
// source metadata is incomplete, observation times from its turns fill the
// missing bounds.
func SessionTimeRange(session Session, turns []Turn) (from, to time.Time) {
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
	add(session.CreatedAt)
	add(session.UpdatedAt)
	for _, turn := range turns {
		if NewSessionKey(turn.Source, turn.SessionID) != session.QualifiedKey() {
			continue
		}
		add(turn.StartedAt)
		add(turn.EndedAt)
		for _, timestamp := range turn.UserPromptTimes {
			add(timestamp)
		}
		for _, model := range turn.ModelObservations {
			add(model.Timestamp)
		}
		for _, tool := range append(append([]ToolObservation{}, turn.ModelTools...), turn.RuntimeTools...) {
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

// ToolLayer identifies where a tool observation was made.
type ToolLayer string

const (
	LayerModel     ToolLayer = "model"
	LayerRuntime   ToolLayer = "runtime"
	LayerEffective ToolLayer = "effective"
)

// ToolStatus describes whether a completed tool call succeeded.
type ToolStatus string

const (
	StatusSuccess ToolStatus = "success"
	StatusFailure ToolStatus = "failure"
	StatusUnknown ToolStatus = "unknown"
)

// TokenUsage is the provider-reported token usage for one or more model
// responses within a turn.
type TokenUsage struct {
	InputTokens           int64 `json:"input_tokens,omitempty"`
	CachedInputTokens     int64 `json:"cached_input_tokens,omitempty"`
	CacheWriteInputTokens int64 `json:"cache_write_input_tokens,omitempty"`
	OutputTokens          int64 `json:"output_tokens,omitempty"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens,omitempty"`
	TotalTokens           int64 `json:"total_tokens,omitempty"`
}

func (usage *TokenUsage) Add(value TokenUsage) {
	if usage == nil {
		return
	}
	usage.InputTokens += value.InputTokens
	usage.CachedInputTokens += value.CachedInputTokens
	usage.CacheWriteInputTokens += value.CacheWriteInputTokens
	usage.OutputTokens += value.OutputTokens
	usage.ReasoningOutputTokens += value.ReasoningOutputTokens
	usage.TotalTokens += value.TotalTokens
}

// TokenUsageEvent keeps the timestamp needed to apply period filters after a
// turn has been written to the cache.
type TokenUsageEvent struct {
	Timestamp time.Time  `json:"timestamp"`
	Model     ModelRef   `json:"model"`
	Usage     TokenUsage `json:"usage"`
}

// ModelObservation records a model seen in a turn, including observations
// that did not carry token usage.
type ModelObservation struct {
	Model     ModelRef  `json:"model"`
	Timestamp time.Time `json:"timestamp"`
	Source    SourceRef `json:"source"`
}

// Turn is the bounded normalization unit for a conversation turn.
type Turn struct {
	SessionID         string             `json:"session_id"`
	ID                string             `json:"id"`
	Ordinal           int                `json:"ordinal"`
	StartedAt         time.Time          `json:"started_at,omitempty"`
	EndedAt           time.Time          `json:"ended_at,omitempty"`
	Aborted           bool               `json:"aborted,omitempty"`
	Source            SourceRef          `json:"source"`
	ModelObservations []ModelObservation `json:"model_observations,omitempty"`
	UserPrompts       int                `json:"user_prompts,omitempty"`
	UserPromptTimes   []time.Time        `json:"-"`
	ModelTools        []ToolObservation  `json:"model_tools,omitempty"`
	RuntimeTools      []ToolObservation  `json:"runtime_tools,omitempty"`
	SkillEvidence     []SkillEvidence    `json:"skill_evidence,omitempty"`
	TokenUsage        *TokenUsage        `json:"token_usage,omitempty"`
	TokenUsageEvents  []TokenUsageEvent  `json:"-"`
}

func (turn *Turn) ObserveModelAt(model ModelRef, timestamp time.Time, source SourceRef) {
	if turn == nil {
		return
	}
	if model == (ModelRef{}) {
		model = UnknownModel()
	}
	turn.ModelObservations = append(turn.ModelObservations, ModelObservation{Model: model, Timestamp: timestamp, Source: source})
}

func (turn *Turn) AddTokenUsage(value TokenUsage) {
	if turn.TokenUsage == nil {
		turn.TokenUsage = &TokenUsage{}
	}
	turn.TokenUsage.Add(value)
}

func (turn *Turn) AddTokenUsageAt(timestamp time.Time, value TokenUsage) {
	turn.AddTokenUsageForModelAt(UnknownModel(), timestamp, value)
}

func (turn *Turn) AddTokenUsageForModelAt(model ModelRef, timestamp time.Time, value TokenUsage) {
	turn.AddTokenUsage(value)
	if model == (ModelRef{}) {
		model = UnknownModel()
	}
	turn.ObserveModelAt(model, timestamp, turn.Source)
	turn.TokenUsageEvents = append(turn.TokenUsageEvents, TokenUsageEvent{Timestamp: timestamp, Model: model, Usage: value})
}

// ToolObservation is a model or runtime observation of one tool invocation.
type ToolObservation struct {
	SessionID     string     `json:"session_id"`
	TurnID        string     `json:"turn_id"`
	RawName       string     `json:"raw_name"`
	CanonicalName string     `json:"canonical_name"`
	CallID        string     `json:"call_id,omitempty"`
	ItemID        string     `json:"item_id,omitempty"`
	Arguments     string     `json:"arguments,omitempty"`
	Timestamp     time.Time  `json:"timestamp"`
	Layer         ToolLayer  `json:"layer"`
	Status        ToolStatus `json:"status"`
	Source        SourceRef  `json:"source"`
}

// SkillEvidenceMethod describes how a skill use was detected.
type SkillEvidenceMethod string

const (
	MethodExplicitInjected          SkillEvidenceMethod = "explicit-injected"
	MethodSelectedSkillInstructions SkillEvidenceMethod = "selected-skill-instructions"
	MethodSkillInjection            SkillEvidenceMethod = "skill-injected"
	MethodRuntimeSkillItem          SkillEvidenceMethod = "runtime-skill-item"
	MethodStructuredTool            SkillEvidenceMethod = "structured-tool"
	MethodSkillInvoked              SkillEvidenceMethod = "skill-invoked"
	MethodExplicitRequest           SkillEvidenceMethod = "explicit-request"
	MethodImplicitAccess            SkillEvidenceMethod = "implicit-access"
)

type SkillMode string

const (
	ModeExplicit SkillMode = "explicit"
	ModeImplicit SkillMode = "implicit"
	ModeUnknown  SkillMode = "unknown"
)

type SkillState string

const (
	StateConfirmed   SkillState = "confirmed"
	StateInferred    SkillState = "inferred"
	StateUnconfirmed SkillState = "unconfirmed"
)

// SkillEvidence is one independently collected indication of skill use.
type SkillEvidence struct {
	SessionID string              `json:"session_id"`
	TurnID    string              `json:"turn_id"`
	SkillName string              `json:"skill_name"`
	Mode      SkillMode           `json:"mode"`
	Method    SkillEvidenceMethod `json:"method"`
	State     SkillState          `json:"state"`
	Timestamp time.Time           `json:"timestamp"`
	Source    SourceRef           `json:"source"`
}

// SkillUse is the deduplicated representation of evidence for one turn.
type SkillUse struct {
	SessionID string                `json:"session_id"`
	TurnID    string                `json:"turn_id"`
	SkillName string                `json:"skill_name"`
	Mode      SkillMode             `json:"mode"`
	Modes     []SkillMode           `json:"modes,omitempty"`
	Methods   []SkillEvidenceMethod `json:"methods"`
	State     SkillState            `json:"state"`
	Timestamp time.Time             `json:"timestamp"`
	Source    SourceRef             `json:"source"`
}

// Warning records a recoverable input problem.
type Warning struct {
	Reason string     `json:"reason"`
	Type   string     `json:"type,omitempty"`
	Source SourceKind `json:"source,omitempty"`
	Path   string     `json:"path,omitempty"`
	Line   int        `json:"line,omitempty"`
	Count  int        `json:"count"`
}

// WarningAdvice returns concise user-facing guidance for a recoverable input
// problem. An empty result means the caller should use generic guidance.
func WarningAdvice(reason string) string {
	switch strings.TrimSpace(reason) {
	case "unknown_type", "ctx_unknown_record", "ctx_unknown_event":
		return "statistics may be incomplete; update catsift or report this record type"
	case "opencode_unknown_part":
		return "some message details may be missing; update catsift or report this part type"
	case "large_line", "ctx_large_line":
		return "statistics may be incomplete; inspect the source history or report the oversized record"
	case "malformed_json", "ctx_malformed_json":
		return "statistics may be incomplete; check or restore the source history"
	case "invalid_timestamp", "missing_timestamp", "ctx_invalid_timestamp":
		return "date-filtered statistics may be incomplete; update catsift if the source format changed"
	case "read_file", "read_session_index":
		return "check the source path and file permissions"
	case "read_workspace":
		return "check the Copilot metadata path and file permissions"
	case "opencode_multiple_databases":
		return "only the selected database was read; remove stale databases or consolidate history if statistics look incomplete"
	case "source_unavailable":
		return "check that the source is installed and readable"
	case "stale_cache":
		return "rerun when the source is available to refresh the result"
	case "empty_line":
		return "empty lines are ignored; no action is usually needed"
	case "cannot read skill inventory path":
		return "check the skill inventory path and file permissions"
	default:
		return ""
	}
}

// WarningDiagnosticLevel returns the human-facing diagnostic level for a
// recoverable input problem.
func WarningDiagnosticLevel(warning Warning) string {
	switch strings.TrimSpace(warning.Reason) {
	case "large_line", "empty_line":
		return "info"
	default:
		return "warning"
	}
}

// WarningDescription returns a human-readable description for a known input
// problem. An empty result means the reason is source-specific or unknown.
func WarningDescription(reason string) string {
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
	case "ctx_large_line":
		return "skipped oversized ctx event record"
	case "ctx_missing_agent":
		return "ctx event has no agent identity"
	case "unknown_type":
		return "skipped unknown record type"
	case "invalid_timestamp":
		return "record has an invalid timestamp"
	case "missing_timestamp":
		return "record has no timestamp"
	case "read_file":
		return "could not read file"
	case "read_workspace":
		return "could not read Copilot session metadata"
	case "source_unavailable":
		return "history source unavailable"
	case "stale_cache":
		return "using stale history cache"
	case "opencode_invalid_session":
		return "skipped invalid OpenCode session"
	case "opencode_malformed_message":
		return "skipped malformed OpenCode message"
	case "opencode_orphan_part":
		return "skipped orphan OpenCode part"
	case "opencode_malformed_part":
		return "skipped malformed OpenCode part"
	case "opencode_multiple_databases":
		return "found multiple OpenCode databases"
	case "opencode_unknown_part":
		return "skipped unknown OpenCode part"
	case "cannot read skill inventory path":
		return "could not read skill inventory path"
	default:
		return ""
	}
}

// Result is the typed aggregate input shared by renderers.
type Result struct {
	Sessions    int               `json:"sessions"`
	UserPrompts int               `json:"user_prompts"`
	ToolCalls   []ToolObservation `json:"tool_calls"`
	SkillUses   []SkillUse        `json:"skill_uses"`
	Warnings    []Warning         `json:"warnings,omitempty"`
}

func NewSourceRef(path string, line int, cliVersion string) SourceRef {
	return SourceRef{Path: path, Line: line, CLIVersion: cliVersion}
}

func NewCodexSourceRef(path string, line int, cliVersion string) SourceRef {
	source := NewSourceRef(path, line, cliVersion)
	source.Source = SourceCodex
	source.Agent = "codex"
	source.Provider = "codex"
	return source
}

func NewOpenCodeSourceRef(path, cliVersion string) SourceRef {
	return SourceRef{
		Path:       path,
		CLIVersion: cliVersion,
		Source:     SourceOpenCode,
		Agent:      "opencode",
		Provider:   "opencode",
	}
}

func NewCtxSourceRef(path, provider, providerSessionID, ctxSessionID, eventID string) SourceRef {
	agent := CanonicalAgentID(provider)
	return SourceRef{
		Path:              path,
		Source:            SourceCtx,
		Agent:             agent,
		Provider:          strings.TrimSpace(provider),
		ProviderSessionID: strings.TrimSpace(providerSessionID),
		CtxSessionID:      strings.TrimSpace(ctxSessionID),
		EventID:           strings.TrimSpace(eventID),
	}
}

func NewTurn(sessionID, id string, ordinal int, source SourceRef) Turn {
	return Turn{SessionID: sessionID, ID: id, Ordinal: ordinal, Source: source}
}

func NewToolObservation(sessionID, turnID, rawName, canonicalName string, layer ToolLayer, status ToolStatus, timestamp time.Time, source SourceRef) ToolObservation {
	return ToolObservation{SessionID: sessionID, TurnID: turnID, RawName: rawName, CanonicalName: canonicalName, Layer: layer, Status: status, Timestamp: timestamp, Source: source}
}

func NewSkillEvidence(sessionID, turnID, skill string, mode SkillMode, method SkillEvidenceMethod, state SkillState, timestamp time.Time, source SourceRef) SkillEvidence {
	return SkillEvidence{SessionID: sessionID, TurnID: turnID, SkillName: skill, Mode: mode, Method: method, State: state, Timestamp: timestamp, Source: source}
}

func NewSkillUse(sessionID, turnID, skill string, mode SkillMode, state SkillState, timestamp time.Time, source SourceRef) SkillUse {
	use := SkillUse{SessionID: sessionID, TurnID: turnID, SkillName: skill, Mode: mode, State: state, Timestamp: timestamp, Source: source}
	if mode != "" {
		use.Modes = []SkillMode{mode}
	}
	return use
}

// HasMode reports whether a deduplicated use has evidence for mode. Older
// callers may construct SkillUse values without Modes, so Mode remains a
// compatible fallback.
func (u SkillUse) HasMode(mode SkillMode) bool {
	for _, observed := range u.Modes {
		if observed == mode {
			return true
		}
	}
	return len(u.Modes) == 0 && u.Mode == mode
}
