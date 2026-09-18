package githubcopilot

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xkumiyu/catsift/internal/usage"
)

const ParserVersion = "copilot-normalizer-v6"

type IngestResult struct {
	Turns    []usage.Turn
	Sessions []usage.Session
	Agents   []string
	Warnings []usage.Warning
}

type normalizer struct {
	sessions  map[string]*sessionState
	tokenSeen map[string]struct{}
}

type sessionState struct {
	meta                usage.Session
	model               usage.ModelRef
	hasModel            bool
	current             *turnState
	turns               map[string]*turnState
	ordinal             int
	aggregateTokenUsage []usage.TokenUsageEvent
}

type turnState struct {
	turn      usage.Turn
	lastTime  time.Time
	toolIndex map[string]int
	evidence  map[string]struct{}
}

func newNormalizer() *normalizer {
	return &normalizer{sessions: make(map[string]*sessionState), tokenSeen: make(map[string]struct{})}
}

func Normalize(events []Envelope) IngestResult {
	n := newNormalizer()
	for _, event := range events {
		n.consume(event)
	}
	return n.result()
}

func (n *normalizer) consume(event Envelope) {
	session := n.session(event.SessionID, event.Source)
	if title := strings.TrimSpace(event.SessionName); title != "" && session.meta.Title == "" {
		session.meta.Title = title
	}
	n.touchSession(session, event.Timestamp)
	data := event.Data
	switch strings.ToLower(strings.TrimSpace(event.Type)) {
	case "session.start":
		if version := firstString(data, "version", "cliVersion", "cli_version"); version != "" {
			session.meta.CLIVersion = version
			session.meta.Source.CLIVersion = version
		}
		n.observeSessionProject(session, event)
		n.observeSessionModel(session, event)
	case "session.context_changed":
		n.observeSessionProject(session, event)
	case "session.shutdown":
		n.observeSessionTokenUsage(session, event)
		n.finishCurrent(session)
	case "skill.invoked":
		turn := n.ensureTurn(session, event, turnID(data))
		n.touchTurn(turn, event.Timestamp, false)
		if name := copilotSkillName(data); name != "" {
			source := n.sourceFor(session, event.Source)
			n.addEvidence(turn, usage.NewSkillEvidence(turn.turn.SessionID, turn.turn.ID, name, usage.ModeUnknown, usage.MethodSkillInvoked, usage.StateConfirmed, event.Timestamp, source))
		}
	case "session.model_change":
		n.observeSessionModel(session, event)
	case "session.auto_mode_resolved":
		n.observeSessionModel(session, event)
		if session.current != nil {
			n.observeModel(session.current, event)
		}
	case "assistant.turn_start", "model.turn_started":
		if session.current != nil && turnID(data) != "" && session.current.turn.ID != turnID(data) {
			n.finishCurrent(session)
		}
		turn := n.ensureTurn(session, event, turnID(data))
		n.touchTurn(turn, event.Timestamp, false)
		n.observeModel(turn, event)
	case "assistant.turn_end", "model.turn_ended":
		turn := n.ensureTurn(session, event, turnID(data))
		n.touchTurn(turn, event.Timestamp, true)
		n.finishCurrent(session)
	case "user.message":
		if role := strings.ToLower(firstString(data, "role", "author")); role != "" && role != "user" {
			return
		}
		if valueText(firstValue(data, "content", "text", "message")) == "" {
			return
		}
		turn := n.ensureTurn(session, event, turnID(data))
		n.touchTurn(turn, event.Timestamp, false)
		turn.turn.UserPrompts++
		if !event.Timestamp.IsZero() {
			turn.turn.UserPromptTimes = append(turn.turn.UserPromptTimes, event.Timestamp)
		}
		n.observeModel(turn, event)
	case "tool.execution_start", "tool.execution_complete":
		turn := n.ensureTurn(session, event, turnID(data))
		n.touchTurn(turn, event.Timestamp, false)
		n.observeRuntimeTool(turn, event)
	case "assistant.message", "assistant.usage", "model.message", "model.model_call_started", "model.model_call_success", "model.response", "session.compaction_complete":
		if !n.eventHasObservation(event) {
			return
		}
		turn := n.ensureTurn(session, event, turnID(data))
		n.touchTurn(turn, event.Timestamp, false)
		n.observeModel(turn, event)
		n.observeModelTool(turn, event)
		n.observeTokenUsage(turn, event)
	}
}

func (n *normalizer) session(providerID string, source usage.SourceRef) *sessionState {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		providerID = "unknown"
	}
	source.ProviderSessionID = providerID
	source.Source = usage.SourceCopilot
	source.Agent = "copilot"
	source.Provider = "copilot"
	meta := usage.NewSession(providerID, source)
	key := meta.QualifiedKey()
	if current, ok := n.sessions[key]; ok {
		return current
	}
	current := &sessionState{meta: meta, turns: make(map[string]*turnState)}
	n.sessions[key] = current
	return current
}

func (n *normalizer) ensureTurn(session *sessionState, event Envelope, explicitID string) *turnState {
	if explicitID != "" {
		if current, ok := session.turns[explicitID]; ok {
			session.current = current
			return current
		}
		if session.current != nil && session.current.turn.ID != explicitID {
			n.finishCurrent(session)
		}
	}
	if explicitID == "" && session.current != nil {
		return session.current
	}
	session.ordinal++
	if explicitID == "" {
		explicitID = "turn-" + strconv.Itoa(session.ordinal)
	}
	source := n.sourceFor(session, event.Source)
	turn := usage.NewTurn(session.meta.QualifiedKey(), explicitID, session.ordinal, source)
	current := &turnState{turn: turn, toolIndex: make(map[string]int), evidence: make(map[string]struct{})}
	session.turns[explicitID] = current
	session.current = current
	if session.hasModel {
		current.turn.ObserveModelAt(session.model, event.Timestamp, source)
	}
	return current
}

func (n *normalizer) finishCurrent(session *sessionState) {
	if session.current == nil {
		return
	}
	if session.current.turn.EndedAt.IsZero() {
		session.current.turn.EndedAt = session.current.lastTime
	}
	session.current = nil
}

func (n *normalizer) touchSession(session *sessionState, timestamp time.Time) {
	if timestamp.IsZero() {
		return
	}
	if session.meta.CreatedAt.IsZero() || timestamp.Before(session.meta.CreatedAt) {
		session.meta.CreatedAt = timestamp
	}
	if session.meta.UpdatedAt.IsZero() || timestamp.After(session.meta.UpdatedAt) {
		session.meta.UpdatedAt = timestamp
	}
}

func (n *normalizer) touchTurn(turn *turnState, timestamp time.Time, end bool) {
	if timestamp.IsZero() {
		return
	}
	if turn.turn.StartedAt.IsZero() || timestamp.Before(turn.turn.StartedAt) {
		turn.turn.StartedAt = timestamp
	}
	if timestamp.After(turn.lastTime) {
		turn.lastTime = timestamp
	}
	if end {
		turn.turn.EndedAt = timestamp
	}
}

func (n *normalizer) sourceFor(session *sessionState, source usage.SourceRef) usage.SourceRef {
	source.Source = usage.SourceCopilot
	source.Agent = "copilot"
	source.Provider = "copilot"
	source.ProviderSessionID = session.meta.ProviderSessionID
	source.CLIVersion = session.meta.CLIVersion
	return source
}

func (n *normalizer) observeSessionModel(session *sessionState, event Envelope) {
	model, ok := modelFromData(event.Data)
	if !ok {
		return
	}
	session.model, session.hasModel = model, true
}

func (n *normalizer) observeSessionProject(session *sessionState, event Envelope) {
	data := event.Data
	if strings.EqualFold(strings.TrimSpace(event.Type), "session.context_changed") {
		if project := firstString(data, "cwd", "workingDirectory", "working_directory", "projectPath", "project_path", "gitRoot", "git_root"); project != "" {
			session.meta.ProjectPath = project
		}
	} else if project := firstString(data, "projectPath", "project_path", "cwd", "workingDirectory", "working_directory", "gitRoot", "git_root"); project != "" {
		session.meta.ProjectPath = project
	}
	if projectName := firstString(data, "repository", "projectName", "project_name"); projectName != "" {
		if projectName = normalizedCopilotProjectName(projectName); projectName != "" {
			session.meta.ProjectName = projectName
			return
		}
	}
	if session.meta.ProjectName == "" {
		session.meta.ProjectName = projectNameFromPath(firstString(data, "gitRoot", "git_root", "cwd", "workingDirectory", "working_directory", "projectPath", "project_path"))
	}
}

func (n *normalizer) observeSessionTokenUsage(session *sessionState, event Envelope) {
	values := tokenUsageEventsFromShutdown(event.Data, event.Timestamp)
	if len(values) > 0 {
		session.aggregateTokenUsage = values
	}
}

func (n *normalizer) observeWorkspaceMetadata(sessionID string, metadata workspaceMetadata, source usage.SourceRef) {
	key := usage.NewSession(sessionID, source).QualifiedKey()
	session, ok := n.sessions[key]
	if !ok {
		return
	}
	if session.meta.Title == "" {
		session.meta.Title = strings.TrimSpace(metadata.Name)
	}
	if session.meta.ProjectPath == "" {
		session.meta.ProjectPath = strings.TrimSpace(metadata.CWD)
		if session.meta.ProjectPath == "" {
			session.meta.ProjectPath = strings.TrimSpace(metadata.GitRoot)
		}
	}
	if session.meta.ProjectName == "" {
		session.meta.ProjectName = normalizedCopilotProjectName(metadata.Repository)
		if session.meta.ProjectName == "" {
			session.meta.ProjectName = projectNameFromPath(firstNonEmpty(metadata.GitRoot, metadata.CWD))
		}
	}
}

func normalizedCopilotProjectName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\x00\r\n\t") {
		return ""
	}
	return value
}

func projectNameFromPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	name := filepath.Base(filepath.Clean(value))
	if name == "." || name == string(filepath.Separator) || strings.ContainsAny(name, "\x00\r\n\t") {
		return ""
	}
	return name
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (n *normalizer) observeModel(turn *turnState, event Envelope) {
	model, ok := modelFromData(event.Data)
	if !ok {
		return
	}
	source := event.Source
	source.Source = usage.SourceCopilot
	source.Agent = "copilot"
	source.Provider = "copilot"
	source.ProviderSessionID = turn.turn.Source.ProviderSessionID
	source.CLIVersion = turn.turn.Source.CLIVersion
	turn.turn.ObserveModelAt(model, event.Timestamp, source)
}

func (n *normalizer) observeRuntimeTool(turn *turnState, event Envelope) {
	rawName := firstString(event.Data, "toolName", "tool_name", "name", "tool")
	if rawName == "" {
		return
	}
	canonical := canonicalToolName(rawName, event.Data)
	status := toolStatusFromData(event.Data, strings.EqualFold(event.Type, "tool.execution_complete"))
	callID := firstString(event.Data, "toolCallId", "tool_call_id", "callId", "call_id", "id")
	key := toolIndexKey(callID, event.ID)
	if index, ok := turn.toolIndex[key]; ok {
		observation := &turn.turn.RuntimeTools[index]
		if observation.Timestamp.IsZero() || event.Timestamp.After(observation.Timestamp) {
			observation.Timestamp = event.Timestamp
		}
		if status != usage.StatusUnknown || observation.Status == usage.StatusUnknown {
			observation.Status = status
		}
		n.observeSkillEvidence(turn, *observation, transientArguments(event.Data), event)
		return
	}
	observation := usage.NewToolObservation(turn.turn.SessionID, turn.turn.ID, rawName, canonical, usage.LayerRuntime, status, event.Timestamp, event.Source)
	observation.CallID = callID
	turn.toolIndex[key] = len(turn.turn.RuntimeTools)
	turn.turn.RuntimeTools = append(turn.turn.RuntimeTools, observation)
	n.observeSkillEvidence(turn, observation, transientArguments(event.Data), event)
}

func (n *normalizer) observeModelTool(turn *turnState, event Envelope) {
	n.observeModelToolMap(turn, event, event.Data)
	requests, _ := event.Data["toolRequests"].([]any)
	for _, value := range requests {
		request, ok := value.(map[string]any)
		if ok {
			n.observeModelToolMap(turn, event, request)
		}
	}
}

func (n *normalizer) observeModelToolMap(turn *turnState, event Envelope, data map[string]any) {
	rawName := firstString(data, "toolName", "tool_name", "name", "tool")
	kind := compact(firstString(data, "type", "kind"))
	if rawName == "" || (kind != "functioncall" && kind != "customtoolcall" && kind != "toolcall" && firstString(data, "toolCallId", "tool_call_id") == "") {
		return
	}
	callID := firstString(data, "toolCallId", "tool_call_id", "callId", "call_id", "id")
	for _, item := range turn.turn.ModelTools {
		if callID != "" && item.CallID == callID {
			return
		}
	}
	observation := usage.NewToolObservation(turn.turn.SessionID, turn.turn.ID, rawName, canonicalToolName(rawName, data), usage.LayerModel, usage.StatusUnknown, event.Timestamp, event.Source)
	observation.CallID = callID
	turn.turn.ModelTools = append(turn.turn.ModelTools, observation)
	requestEvent := event
	requestEvent.Data = data
	n.observeSkillEvidence(turn, observation, transientArguments(data), requestEvent)
}

func (n *normalizer) observeSkillEvidence(turn *turnState, observation usage.ToolObservation, arguments string, event Envelope) {
	if arguments != "" {
		copy := observation
		copy.Arguments = arguments
		n.addEvidence(turn, usage.DetectStructuredSkillTool(copy)...)
	}
	if observation.Status != usage.StatusFailure {
		access := accessValue(event.Data)
		n.addEvidence(turn, usage.DetectImplicitAccess(access, turn.turn.SessionID, turn.turn.ID, event.Timestamp, event.Source)...)
	}
}

func (n *normalizer) observeTokenUsage(turn *turnState, event Envelope) {
	tokenUsage, ok := tokenUsageFromData(event.Data)
	if !ok {
		return
	}
	callID := firstString(event.Data, "callId", "call_id", "modelCallId", "model_call_id", "responseId", "response_id", "id")
	if callID == "" {
		callID = event.ID
	}
	key := turn.turn.SessionID + "\x00" + callID
	if _, seen := n.tokenSeen[key]; seen {
		return
	}
	n.tokenSeen[key] = struct{}{}
	model, ok := modelFromData(event.Data)
	if !ok {
		model = latestModel(turn.turn)
	}
	turn.turn.AddTokenUsageForModelAt(model, event.Timestamp, tokenUsage)
}

func (n *normalizer) eventHasObservation(event Envelope) bool {
	_, model := modelFromData(event.Data)
	_, tokens := tokenUsageFromData(event.Data)
	_, hasToolRequests := event.Data["toolRequests"]
	return model || tokens || firstString(event.Data, "toolName", "tool_name", "name", "tool") != "" || hasToolRequests
}

func (n *normalizer) addEvidence(turn *turnState, values ...usage.SkillEvidence) {
	for _, value := range values {
		key := string(value.Method) + "\x00" + value.SkillName
		if value.SkillName == "" {
			continue
		}
		if _, exists := turn.evidence[key]; exists {
			continue
		}
		turn.evidence[key] = struct{}{}
		turn.turn.SkillEvidence = append(turn.turn.SkillEvidence, value)
	}
}

func (n *normalizer) result() IngestResult {
	for _, session := range n.sessions {
		n.finishCurrent(session)
		n.applySessionTokenUsage(session)
	}
	result := IngestResult{Agents: []string{"copilot"}}
	turns := make([]*turnState, 0)
	for _, session := range n.sessions {
		result.Sessions = append(result.Sessions, session.meta)
		for _, turn := range session.turns {
			turns = append(turns, turn)
		}
	}
	sort.Slice(result.Sessions, func(i, j int) bool { return result.Sessions[i].QualifiedKey() < result.Sessions[j].QualifiedKey() })
	sort.SliceStable(turns, func(i, j int) bool {
		left, right := turns[i].turn, turns[j].turn
		if left.StartedAt.Equal(right.StartedAt) {
			if left.SessionID == right.SessionID {
				return left.Ordinal < right.Ordinal
			}
			return left.SessionID < right.SessionID
		}
		return left.StartedAt.Before(right.StartedAt)
	})
	for _, turn := range turns {
		result.Turns = append(result.Turns, turn.turn)
	}
	return result
}

func (n *normalizer) applySessionTokenUsage(session *sessionState) {
	if len(session.aggregateTokenUsage) == 0 {
		return
	}
	for _, turn := range session.turns {
		if len(turn.turn.TokenUsageEvents) > 0 {
			return
		}
	}

	// ponytail: Copilot persists shutdown usage at session/model granularity;
	// attach it to the terminal turn until per-turn usage is persisted.
	var latest *turnState
	for _, turn := range session.turns {
		if latest == nil || turn.lastTime.After(latest.lastTime) || (turn.lastTime.Equal(latest.lastTime) && turn.turn.Ordinal > latest.turn.Ordinal) {
			latest = turn
		}
	}
	if latest == nil {
		session.ordinal++
		source := n.sourceFor(session, session.meta.Source)
		aggregate := usage.NewTurn(session.meta.QualifiedKey(), "turn-aggregate", session.ordinal, source)
		aggregate.StartedAt = session.meta.UpdatedAt
		aggregate.EndedAt = session.meta.UpdatedAt
		latest = &turnState{turn: aggregate, lastTime: session.meta.UpdatedAt, toolIndex: make(map[string]int), evidence: make(map[string]struct{})}
		session.turns[aggregate.ID] = latest
	}
	for _, event := range session.aggregateTokenUsage {
		latest.turn.AddTokenUsageForModelAt(event.Model, event.Timestamp, event.Usage)
	}
}

func turnID(data map[string]any) string {
	if value := firstString(data, "turnId", "turn_id", "turnID"); value != "" {
		return value
	}
	return scalarString(data["turn"])
}

func modelFromData(data map[string]any) (usage.ModelRef, bool) {
	if model, ok := usage.ModelFromMap(data, "copilot"); ok {
		return model, true
	}
	for _, key := range []string{"model", "modelName", "model_name", "modelId", "model_id", "chosenModel", "chosen_model"} {
		if nested, ok := data[key].(string); ok && strings.TrimSpace(nested) != "" {
			return usage.NewModelRef("copilot", nested), true
		}
	}
	return usage.ModelRef{}, false
}

func latestModel(turn usage.Turn) usage.ModelRef {
	if len(turn.ModelObservations) == 0 {
		return usage.UnknownModel()
	}
	return turn.ModelObservations[len(turn.ModelObservations)-1].Model
}

func canonicalToolName(rawName string, data map[string]any) string {
	name := strings.ToLower(strings.TrimSpace(rawName))
	switch name {
	case "bash", "shell", "terminal", "command", "exec":
		return "shell"
	case "read", "edit", "write", "glob", "grep", "websearch", "web_search":
		return strings.ReplaceAll(name, "_", "")
	}
	if strings.Contains(name, "mcp") {
		server := firstString(data, "server", "serverName", "server_name")
		tool := firstString(data, "tool", "toolName", "tool_name", "name")
		if server != "" && tool != "" {
			return "mcp:" + strings.ToLower(server) + "/" + strings.ToLower(tool)
		}
	}
	if name == "" {
		return "unknown"
	}
	return name
}

func toolStatus(value string, complete bool) usage.ToolStatus {
	switch compact(value) {
	case "completed", "complete", "success", "succeeded", "done", "ok":
		return usage.StatusSuccess
	case "error", "errored", "failed", "failure", "cancelled", "canceled", "aborted":
		return usage.StatusFailure
	default:
		if complete {
			return usage.StatusSuccess
		}
		return usage.StatusUnknown
	}
}

func toolStatusFromData(data map[string]any, complete bool) usage.ToolStatus {
	if success, ok := data["success"].(bool); ok {
		if success {
			return usage.StatusSuccess
		}
		return usage.StatusFailure
	}
	return toolStatus(firstString(data, "status", "state", "result"), complete)
}

func toolIndexKey(callID, eventID string) string {
	if callID != "" {
		return "call:" + callID
	}
	return "event:" + eventID
}

func transientArguments(data map[string]any) string {
	for _, key := range []string{"arguments", "input", "params", "path", "command", "cmd", "script"} {
		if value, ok := data[key]; ok {
			encoded, err := json.Marshal(value)
			if err == nil {
				return string(encoded)
			}
		}
	}
	return ""
}

func accessValue(data map[string]any) string {
	if value := firstString(data, "command", "cmd", "script", "path", "file", "filePath", "file_path"); value != "" {
		return value
	}
	for _, key := range []string{"arguments", "input", "params"} {
		if nested, ok := data[key].(map[string]any); ok {
			if value := firstString(nested, "command", "cmd", "script", "path", "file", "filePath", "file_path"); value != "" {
				return value
			}
		}
	}
	return ""
}

func copilotSkillName(data map[string]any) string {
	for _, key := range []string{"name", "skillName", "skill_name", "skill"} {
		if value := firstString(data, key); value != "" {
			if name := normalizedCopilotSkillName(value); name != "" {
				return name
			}
		}
	}
	for _, key := range []string{"path", "skillPath", "skill_path"} {
		if value := firstString(data, key); value != "" {
			if name := usage.SkillNameFromPath(value); name != "" {
				return name
			}
		}
	}
	return ""
}

func normalizedCopilotSkillName(value string) string {
	value = strings.Trim(value, " \t\r\n\"'`;,()[]{}")
	if value == "" || strings.ContainsAny(value, " /\\\t\r\n") {
		return ""
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' || r == ':' {
			continue
		}
		return ""
	}
	return value
}

func tokenUsageFromData(data map[string]any) (usage.TokenUsage, bool) {
	for _, key := range []string{"usage", "tokenUsage", "token_usage", "usageMetadata", "responseUsage", "response_usage", "copilotUsage", "copilot_usage", "compactionTokensUsed", "compaction_tokens_used"} {
		if usageMap, ok := data[key].(map[string]any); ok {
			value := tokenUsageFromMap(usageMap)
			if value != (usage.TokenUsage{}) {
				return value, true
			}
		}
	}
	value := tokenUsageFromMap(data)
	return value, value != (usage.TokenUsage{})
}

func tokenUsageFromMap(data map[string]any) usage.TokenUsage {
	value := usage.TokenUsage{
		InputTokens:           integerValue(data, "inputTokens", "input_tokens", "promptTokens", "prompt_tokens"),
		CachedInputTokens:     integerValue(data, "cachedInputTokens", "cached_input_tokens", "cacheReadTokens", "cache_read_tokens"),
		CacheWriteInputTokens: integerValue(data, "cacheWriteInputTokens", "cache_write_input_tokens", "cacheWriteTokens", "cache_write_tokens"),
		OutputTokens:          integerValue(data, "outputTokens", "output_tokens", "completionTokens", "completion_tokens"),
		ReasoningOutputTokens: integerValue(data, "reasoningOutputTokens", "reasoning_output_tokens", "reasoningTokens", "reasoning_tokens"),
		TotalTokens:           integerValue(data, "totalTokens", "total_tokens"),
	}
	if value.TotalTokens == 0 && (value.InputTokens != 0 || value.OutputTokens != 0) {
		value.TotalTokens = value.InputTokens + value.OutputTokens
	}
	return value
}

func tokenUsageEventsFromShutdown(data map[string]any, timestamp time.Time) []usage.TokenUsageEvent {
	metrics, ok := data["modelMetrics"].(map[string]any)
	if !ok {
		return nil
	}
	models := make([]string, 0, len(metrics))
	for model := range metrics {
		models = append(models, model)
	}
	sort.Strings(models)
	result := make([]usage.TokenUsageEvent, 0, len(models))
	for _, modelName := range models {
		metric, ok := metrics[modelName].(map[string]any)
		if !ok {
			continue
		}
		value, ok := tokenUsageFromData(metric)
		if !ok {
			continue
		}
		result = append(result, usage.TokenUsageEvent{Timestamp: timestamp, Model: usage.NewModelRef("copilot", modelName), Usage: value})
	}
	return result
}

func integerValue(data map[string]any, keys ...string) int64 {
	for _, key := range keys {
		value, ok := data[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case float64:
			return int64(typed)
		case json.Number:
			parsed, _ := typed.Int64()
			return parsed
		case string:
			parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
			return parsed
		case int:
			return int64(typed)
		case int64:
			return typed
		}
	}
	return 0
}

func firstString(data map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := scalarString(data[key]); value != "" {
			return value
		}
	}
	return ""
}

func scalarString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		return ""
	}
}

func firstValue(data map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := data[key]; ok {
			return value
		}
	}
	return nil
}

func valueText(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		for _, item := range typed {
			if text := valueText(item); text != "" {
				return text
			}
		}
	case map[string]any:
		for _, key := range []string{"text", "value", "content", "message"} {
			if text := valueText(typed[key]); text != "" {
				return text
			}
		}
	}
	return ""
}

func compact(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("_", "", "-", "", " ", "").Replace(value)
	return value
}
