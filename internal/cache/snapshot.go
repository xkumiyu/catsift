package cache

import (
	"time"

	"github.com/xkumiyu/catsift/internal/usage"
)

// Snapshot is the compact, source-neutral data needed by aggregate reports.
// It deliberately has no prompt text, provider payload, or tool arguments.
type Snapshot struct {
	Turns    []Turn          `json:"turns,omitempty"`
	Sessions []Session       `json:"sessions,omitempty"`
	Agents   []string        `json:"agents,omitempty"`
	Warnings []usage.Warning `json:"warnings,omitempty"`
}

type Session struct {
	ID                string    `json:"id"`
	ProjectPath       string    `json:"project_path,omitempty"`
	CLIVersion        string    `json:"cli_version,omitempty"`
	Agent             string    `json:"agent,omitempty"`
	Provider          string    `json:"provider,omitempty"`
	ProviderSessionID string    `json:"provider_session_id,omitempty"`
	CtxSessionID      string    `json:"ctx_session_id,omitempty"`
	Source            SourceRef `json:"source"`
}

type Turn struct {
	SessionID        string                  `json:"session_id"`
	ID               string                  `json:"id"`
	Ordinal          int                     `json:"ordinal"`
	StartedAt        time.Time               `json:"started_at,omitempty"`
	EndedAt          time.Time               `json:"ended_at,omitempty"`
	Aborted          bool                    `json:"aborted,omitempty"`
	Source           SourceRef               `json:"source"`
	UserPrompts      int                     `json:"user_prompts,omitempty"`
	PromptTimes      []time.Time             `json:"prompt_times,omitempty"`
	ModelTools       []tool                  `json:"model_tools,omitempty"`
	RuntimeTools     []tool                  `json:"runtime_tools,omitempty"`
	SkillEvidence    []skillEvidence         `json:"skill_evidence,omitempty"`
	TokenUsage       *usage.TokenUsage       `json:"token_usage,omitempty"`
	TokenUsageEvents []usage.TokenUsageEvent `json:"token_usage_events,omitempty"`
}

type tool struct {
	SessionID     string    `json:"session_id"`
	TurnID        string    `json:"turn_id"`
	RawName       string    `json:"raw_name"`
	CanonicalName string    `json:"canonical_name"`
	CallID        string    `json:"call_id,omitempty"`
	ItemID        string    `json:"item_id,omitempty"`
	Timestamp     time.Time `json:"timestamp"`
	Layer         string    `json:"layer"`
	Status        string    `json:"status"`
	Source        SourceRef `json:"source"`
}

type skillEvidence struct {
	SessionID string    `json:"session_id"`
	TurnID    string    `json:"turn_id"`
	SkillName string    `json:"skill_name"`
	Mode      string    `json:"mode"`
	Method    string    `json:"method"`
	State     string    `json:"state"`
	Timestamp time.Time `json:"timestamp"`
	Source    SourceRef `json:"source"`
}

// TurnFromUsage converts a normalized turn to its cache representation.
func TurnFromUsage(value usage.Turn) Turn {
	result := Turn{
		SessionID:   value.SessionID,
		ID:          value.ID,
		Ordinal:     value.Ordinal,
		StartedAt:   value.StartedAt,
		EndedAt:     value.EndedAt,
		Aborted:     value.Aborted,
		Source:      SourceRefFromUsage(value.Source),
		UserPrompts: value.UserPrompts,
		PromptTimes: append([]time.Time(nil), value.UserPromptTimes...),
	}
	if value.TokenUsage != nil {
		copy := *value.TokenUsage
		result.TokenUsage = &copy
	}
	result.TokenUsageEvents = append([]usage.TokenUsageEvent(nil), value.TokenUsageEvents...)
	result.ModelTools = toolsFromUsage(value.ModelTools)
	result.RuntimeTools = toolsFromUsage(value.RuntimeTools)
	result.SkillEvidence = skillsFromUsage(value.SkillEvidence)
	return result
}

func (value Turn) Usage() usage.Turn {
	result := usage.Turn{
		SessionID:       value.SessionID,
		ID:              value.ID,
		Ordinal:         value.Ordinal,
		StartedAt:       value.StartedAt,
		EndedAt:         value.EndedAt,
		Aborted:         value.Aborted,
		Source:          value.Source.Usage(),
		UserPrompts:     value.UserPrompts,
		UserPromptTimes: append([]time.Time(nil), value.PromptTimes...),
		ModelTools:      make([]usage.ToolObservation, 0, len(value.ModelTools)),
		RuntimeTools:    make([]usage.ToolObservation, 0, len(value.RuntimeTools)),
		SkillEvidence:   make([]usage.SkillEvidence, 0, len(value.SkillEvidence)),
	}
	if value.TokenUsage != nil {
		copy := *value.TokenUsage
		result.TokenUsage = &copy
	}
	result.TokenUsageEvents = append([]usage.TokenUsageEvent(nil), value.TokenUsageEvents...)
	for _, item := range value.ModelTools {
		result.ModelTools = append(result.ModelTools, item.Usage())
	}
	for _, item := range value.RuntimeTools {
		result.RuntimeTools = append(result.RuntimeTools, item.Usage())
	}
	for _, item := range value.SkillEvidence {
		result.SkillEvidence = append(result.SkillEvidence, item.Usage())
	}
	return result
}

func toolsFromUsage(values []usage.ToolObservation) []tool {
	result := make([]tool, 0, len(values))
	for _, value := range values {
		result = append(result, tool{
			SessionID:     value.SessionID,
			TurnID:        value.TurnID,
			RawName:       value.RawName,
			CanonicalName: value.CanonicalName,
			CallID:        value.CallID,
			ItemID:        value.ItemID,
			Timestamp:     value.Timestamp,
			Layer:         string(value.Layer),
			Status:        string(value.Status),
			Source:        SourceRefFromUsage(value.Source),
		})
	}
	return result
}

func (value tool) Usage() usage.ToolObservation {
	return usage.ToolObservation{
		SessionID:     value.SessionID,
		TurnID:        value.TurnID,
		RawName:       value.RawName,
		CanonicalName: value.CanonicalName,
		CallID:        value.CallID,
		ItemID:        value.ItemID,
		Timestamp:     value.Timestamp,
		Layer:         usage.ToolLayer(value.Layer),
		Status:        usage.ToolStatus(value.Status),
		Source:        value.Source.Usage(),
	}
}

func skillsFromUsage(values []usage.SkillEvidence) []skillEvidence {
	result := make([]skillEvidence, 0, len(values))
	for _, value := range values {
		result = append(result, skillEvidence{
			SessionID: value.SessionID,
			TurnID:    value.TurnID,
			SkillName: value.SkillName,
			Mode:      string(value.Mode),
			Method:    string(value.Method),
			State:     string(value.State),
			Timestamp: value.Timestamp,
			Source:    SourceRefFromUsage(value.Source),
		})
	}
	return result
}

func (value skillEvidence) Usage() usage.SkillEvidence {
	return usage.SkillEvidence{
		SessionID: value.SessionID,
		TurnID:    value.TurnID,
		SkillName: value.SkillName,
		Mode:      usage.SkillMode(value.Mode),
		Method:    usage.SkillEvidenceMethod(value.Method),
		State:     usage.SkillState(value.State),
		Timestamp: value.Timestamp,
		Source:    value.Source.Usage(),
	}
}

// SourceRef keeps identity fields used by aggregation while omitting raw file
// positions and other source-local details.
type SourceRef struct {
	CLIVersion        string `json:"cli_version,omitempty"`
	Source            string `json:"source,omitempty"`
	Agent             string `json:"agent,omitempty"`
	Provider          string `json:"provider,omitempty"`
	ProviderSessionID string `json:"provider_session_id,omitempty"`
	CtxSessionID      string `json:"ctx_session_id,omitempty"`
	EventID           string `json:"event_id,omitempty"`
}

// SourceRefFromUsage converts a usage source reference to its compact form.
func SourceRefFromUsage(value usage.SourceRef) SourceRef {
	return SourceRef{
		CLIVersion:        value.CLIVersion,
		Source:            string(value.Source),
		Agent:             value.Agent,
		Provider:          value.Provider,
		ProviderSessionID: value.ProviderSessionID,
		CtxSessionID:      value.CtxSessionID,
		EventID:           value.EventID,
	}
}

func (value SourceRef) Usage() usage.SourceRef {
	return usage.SourceRef{
		CLIVersion:        value.CLIVersion,
		Source:            usage.SourceKind(value.Source),
		Agent:             value.Agent,
		Provider:          value.Provider,
		ProviderSessionID: value.ProviderSessionID,
		CtxSessionID:      value.CtxSessionID,
		EventID:           value.EventID,
	}
}
