package opencode

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xkumiyu/catsift/internal/usage"
)

type normalizer struct {
	databasePath   string
	sessions       map[string]*sessionState
	currentMessage *messageState
	warnings       warningCollector
}

type sessionState struct {
	rawID   string
	meta    SessionMetadata
	current *turnState
	turns   []*turnState
	ordinal int
}

type turnState struct {
	turn      usage.Turn
	userTexts []textObservation
	toolIndex map[string]int
	lastTime  time.Time
	evidence  map[string]struct{}
}

type textObservation struct {
	text      string
	timestamp time.Time
}

type tokenObservation struct {
	timestamp time.Time
	usage     usage.TokenUsage
	model     usage.ModelRef
	hasModel  bool
}

type pendingTool struct {
	part PartRow
	raw  map[string]any
}

type messageState struct {
	row       MessageRow
	raw       map[string]any
	role      string
	model     usage.ModelRef
	hasModel  bool
	texts     []textObservation
	tools     []pendingTool
	tokens    []tokenObservation
	userInput bool
	valid     bool
}

func newNormalizer(databasePath string) *normalizer {
	return &normalizer{databasePath: databasePath, sessions: make(map[string]*sessionState)}
}

func (n *normalizer) consume(row Row) error {
	switch row.Kind {
	case RowSession:
		n.finishMessage()
		n.consumeSession(row.Session)
	case RowMessage:
		n.finishMessage()
		n.startMessage(row.Message)
	case RowPart:
		n.consumePart(row.Part)
	}
	return nil
}

func (n *normalizer) consumeSession(row SessionRow) {
	if strings.TrimSpace(row.ID) == "" {
		n.warnings.add("opencode_invalid_session", n.databasePath, "session")
		return
	}
	session := n.session(row.ID)
	session.meta.ProjectPath = row.Directory
	session.meta.Title = strings.TrimSpace(row.Title)
	session.meta.CLIVersion = row.Version
	session.meta.CreatedAt = row.CreatedAt
	session.meta.UpdatedAt = row.UpdatedAt
	session.meta.Source = n.source(row.ID, row.Version)
}

func (n *normalizer) startMessage(row MessageRow) {
	raw, err := decodeObject(row.Data)
	if err != nil {
		n.warnings.add("opencode_malformed_message", n.databasePath, "message")
		n.currentMessage = &messageState{row: row}
		return
	}
	model, hasModel := usage.ModelFromMap(raw, "opencode")
	n.currentMessage = &messageState{
		row:      row,
		raw:      raw,
		role:     strings.ToLower(strings.TrimSpace(firstString(raw, "role", "author"))),
		model:    model,
		hasModel: hasModel,
		valid:    true,
	}
}

func (n *normalizer) consumePart(row PartRow) {
	message := n.currentMessage
	if message == nil || message.row.ID != row.MessageID || !message.valid {
		n.warnings.add("opencode_orphan_part", n.databasePath, "part")
		return
	}
	raw, err := decodeObject(row.Data)
	if err != nil {
		n.warnings.add("opencode_malformed_part", n.databasePath, "part")
		return
	}
	switch compactType(stringValue(raw, "type")) {
	case "text":
		if ignoredText(raw) {
			return
		}
		text := valueText(rawValue(raw, "text", "content", "message"))
		if text != "" {
			when := row.CreatedAt
			if when.IsZero() {
				when = message.row.CreatedAt
			}
			message.texts = append(message.texts, textObservation{text: text, timestamp: when})
		}
	case "tool":
		message.tools = append(message.tools, pendingTool{part: row, raw: raw})
	case "file":
		if message.role == "user" {
			message.userInput = true
		}
	case "stepfinish":
		if tokenUsage, ok := tokenUsageFromMessage(raw); ok {
			when := row.CreatedAt
			if when.IsZero() {
				when = message.row.CreatedAt
			}
			model, hasModel := usage.ModelFromMap(raw, "opencode")
			if !hasModel {
				model, hasModel = message.model, message.hasModel
			}
			message.tokens = append(message.tokens, tokenObservation{timestamp: when, usage: tokenUsage, model: model, hasModel: hasModel})
		}
	case "reasoning", "stepstart", "patch", "snapshot", "compaction", "subtask", "agent", "retry":
		// These parts do not add an observation to the current aggregate.
	default:
		n.warnings.addType("opencode_unknown_part", stringValue(raw, "type"), n.databasePath, "part")
	}
}

func (n *normalizer) finishMessage() {
	message := n.currentMessage
	if message == nil {
		return
	}
	n.currentMessage = nil
	if !message.valid {
		return
	}
	sessionID := message.row.SessionID
	if sessionID == "" {
		sessionID = stringValue(message.raw, "session_id")
	}
	session := n.session(sessionID)
	switch message.role {
	case "user":
		n.finishUserMessage(session, message)
	case "assistant":
		turn := n.ensureTurn(session, "", message.row.CreatedAt)
		if message.hasModel {
			turn.turn.ObserveModelAt(message.model, message.row.CreatedAt, turn.turn.Source)
		}
		if tokenUsage, ok := tokenUsageFromMessage(message.raw); ok {
			if message.hasModel {
				turn.turn.AddTokenUsageForModelAt(message.model, message.row.CreatedAt, tokenUsage)
			} else {
				turn.turn.AddTokenUsageAt(message.row.CreatedAt, tokenUsage)
			}
			turn.touch(message.row.CreatedAt)
		} else {
			for _, token := range message.tokens {
				if token.hasModel {
					turn.turn.AddTokenUsageForModelAt(token.model, token.timestamp, token.usage)
				} else {
					turn.turn.AddTokenUsageAt(token.timestamp, token.usage)
				}
				turn.touch(token.timestamp)
			}
		}
		n.applyTools(turn, message.tools)
	default:
		if session.current != nil {
			n.applyTools(session.current, message.tools)
		}
	}
}

func (n *normalizer) finishUserMessage(session *sessionState, message *messageState) {
	text := joinTexts(message.texts)
	if text == "" && !message.userInput {
		return
	}
	injectedOnly := !message.userInput && onlyInjectedSkill(text)
	if !injectedOnly && session.current != nil && (session.current.turn.UserPrompts > 0 || len(session.current.userTexts) == 0) {
		n.finishTurn(session)
	}
	turn := n.ensureTurn(session, message.row.ID, message.row.CreatedAt)
	if message.hasModel {
		turn.turn.ObserveModelAt(message.model, message.row.CreatedAt, turn.turn.Source)
	}
	if !injectedOnly {
		turn.turn.UserPrompts++
		turn.turn.UserPromptTimes = append(turn.turn.UserPromptTimes, message.row.CreatedAt)
	}
	turn.userTexts = append(turn.userTexts, message.texts...)
	turn.touch(message.row.CreatedAt)
	n.applyTools(turn, message.tools)
}

func ignoredText(raw map[string]any) bool {
	if boolValue(raw, "synthetic") || boolValue(raw, "ignored") {
		return true
	}
	return boolValue(mapValue(raw, "metadata"), "compaction_continue")
}

func (n *normalizer) applyTools(turn *turnState, pending []pendingTool) {
	for _, value := range pending {
		observation := normalizeTool(turn, value)
		key := toolKey(observation, value.part.ID)
		if index, ok := turn.toolIndex[key]; ok {
			previous := &turn.turn.RuntimeTools[index]
			if observation.Timestamp.After(previous.Timestamp) || previous.Timestamp.IsZero() {
				previous.Timestamp = observation.Timestamp
			}
			if observation.Status != usage.StatusUnknown || previous.Status == usage.StatusUnknown {
				previous.Status = observation.Status
			}
			if previous.Arguments == "" {
				previous.Arguments = observation.Arguments
			}
			turn.touch(observation.Timestamp)
			continue
		}
		turn.toolIndex[key] = len(turn.turn.RuntimeTools)
		turn.turn.RuntimeTools = append(turn.turn.RuntimeTools, observation)
		turn.touch(observation.Timestamp)
	}
}

func normalizeTool(turn *turnState, pending pendingTool) usage.ToolObservation {
	rawName := firstString(pending.raw, "tool", "name", "tool_name")
	canonical := canonicalToolName(rawName, pending.raw)
	state := mapValue(pending.raw, "state")
	status := toolStatus(firstString(state, "status", "state"))
	if status == usage.StatusUnknown {
		status = toolStatus(firstString(pending.raw, "status", "state"))
	}
	arguments := toolArguments(pending.raw, state)
	timestamp := pending.part.CreatedAt
	obs := usage.NewToolObservation(turn.turn.SessionID, turn.turn.ID, rawName, canonical, usage.LayerRuntime, status, timestamp, turn.turn.Source)
	obs.CallID = firstString(pending.raw, "callID", "call_id", "callId")
	obs.ItemID = firstString(pending.raw, "itemID", "item_id", "itemId", "id")
	if obs.ItemID == "" {
		obs.ItemID = pending.part.ID
	}
	obs.Arguments = arguments
	return obs
}

func canonicalToolName(rawName string, raw map[string]any) string {
	name := strings.ToLower(strings.TrimSpace(rawName))
	switch name {
	case "bash", "shell", "exec", "command", "run":
		return "shell"
	case "read", "edit", "write", "glob", "grep", "webfetch", "websearch", "task":
		return name
	}
	if strings.Contains(name, "mcp") {
		server := firstString(raw, "server", "server_name", "serverName", "mcp_server", "mcpServer")
		tool := firstString(raw, "tool", "tool_name", "toolName", "name")
		if server != "" && tool != "" {
			return "mcp:" + strings.ToLower(server) + "/" + strings.ToLower(tool)
		}
	}
	if name == "" {
		return "unknown"
	}
	return name
}

func toolStatus(value string) usage.ToolStatus {
	switch compactType(value) {
	case "completed", "complete", "success", "succeeded", "done", "ok":
		return usage.StatusSuccess
	case "error", "errored", "failed", "failure", "cancelled", "canceled", "aborted":
		return usage.StatusFailure
	default:
		return usage.StatusUnknown
	}
}

func toolArguments(raw, state map[string]any) string {
	var value any
	if state != nil {
		value = state["input"]
	}
	if value == nil {
		value = raw["input"]
	}
	if value == nil {
		value = raw["arguments"]
	}
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

func toolKey(value usage.ToolObservation, fallback string) string {
	if value.CallID != "" {
		return "call:" + value.CallID
	}
	if value.ItemID != "" {
		return "item:" + value.ItemID
	}
	return "part:" + fallback
}

func (n *normalizer) finishTurn(session *sessionState) {
	if session.current == nil {
		return
	}
	current := session.current
	if current.turn.EndedAt.IsZero() {
		current.turn.EndedAt = current.lastTime
	}
	session.turns = append(session.turns, current)
	session.current = nil
}

func (n *normalizer) ensureTurn(session *sessionState, id string, timestamp time.Time) *turnState {
	if session.current != nil {
		return session.current
	}
	session.ordinal++
	if strings.TrimSpace(id) == "" {
		id = "turn-" + strconv.Itoa(session.ordinal)
	}
	turn := usage.NewTurn(openCodeSessionID(session.rawID), id, session.ordinal, session.meta.Source)
	current := &turnState{turn: turn, toolIndex: make(map[string]int), evidence: make(map[string]struct{})}
	session.current = current
	current.touch(timestamp)
	return current
}

func (turn *turnState) touch(timestamp time.Time) {
	if timestamp.IsZero() {
		return
	}
	if turn.turn.StartedAt.IsZero() || timestamp.Before(turn.turn.StartedAt) {
		turn.turn.StartedAt = timestamp
	}
	if timestamp.After(turn.lastTime) {
		turn.lastTime = timestamp
	}
}

func (n *normalizer) session(rawID string) *sessionState {
	rawID = strings.TrimSpace(rawID)
	if rawID == "" {
		rawID = "unknown"
	}
	key := openCodeSessionID(rawID)
	if session, ok := n.sessions[key]; ok {
		return session
	}
	source := n.source(rawID, "")
	session := &sessionState{rawID: rawID, meta: usage.NewSession(rawID, source)}
	n.sessions[key] = session
	return session
}

func (n *normalizer) source(rawID, version string) usage.SourceRef {
	source := usage.NewOpenCodeSourceRef(n.databasePath, version)
	source.ProviderSessionID = rawID
	return source
}

func openCodeSessionID(rawID string) string {
	return string(usage.SourceOpenCode) + "\x00" + rawID
}

func (n *normalizer) result() IngestResult {
	n.finishMessage()
	for _, session := range n.sessions {
		n.finishTurn(session)
		for _, turn := range session.turns {
			n.deriveEvidence(turn)
		}
	}
	sessions := make([]SessionMetadata, 0, len(n.sessions))
	turns := make([]*turnState, 0)
	for _, session := range n.sessions {
		sessions = append(sessions, session.meta)
		turns = append(turns, session.turns...)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].ID < sessions[j].ID })
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
	result := IngestResult{Sessions: sessions, Agents: []string{"opencode"}, Warnings: n.warnings.values()}
	for _, turn := range turns {
		result.Turns = append(result.Turns, turn.turn)
	}
	return result
}

func (n *normalizer) deriveEvidence(turn *turnState) {
	for _, text := range turn.userTexts {
		n.addEvidence(turn, usage.DetectExplicitRequest(text.text, turn.turn.SessionID, turn.turn.ID, text.timestamp, turn.turn.Source)...)
		n.addEvidence(turn, usage.DetectInjectedSkills(text.text, turn.turn.SessionID, turn.turn.ID, text.timestamp, turn.turn.Source)...)
	}
	for _, tool := range turn.turn.RuntimeTools {
		n.addEvidence(turn, usage.DetectStructuredSkillTool(tool)...)
		if tool.Status != usage.StatusFailure {
			if command := toolAccessCommand(tool); command != "" {
				n.addEvidence(turn, usage.DetectImplicitAccess(command, turn.turn.SessionID, turn.turn.ID, tool.Timestamp, turn.turn.Source)...)
			}
		}
	}
}

func (n *normalizer) addEvidence(turn *turnState, values ...usage.SkillEvidence) {
	for _, value := range values {
		key := string(value.Method) + "\x00" + value.SkillName
		if _, ok := turn.evidence[key]; ok {
			continue
		}
		turn.evidence[key] = struct{}{}
		turn.turn.SkillEvidence = append(turn.turn.SkillEvidence, value)
	}
}

func toolAccessCommand(tool usage.ToolObservation) string {
	arguments := strings.TrimSpace(tool.Arguments)
	if arguments == "" {
		return ""
	}
	if tool.CanonicalName == "shell" {
		if value := commandValue(arguments); value != "" {
			return value
		}
		return arguments
	}
	if tool.RawName == "read" || tool.RawName == "edit" || tool.RawName == "write" || tool.RawName == "glob" || tool.RawName == "grep" {
		if path := argumentPath(arguments); path != "" {
			return "cat " + path
		}
	}
	return ""
}

func commandValue(arguments string) string {
	var value any
	if json.Unmarshal([]byte(arguments), &value) != nil {
		return ""
	}
	if object, ok := value.(map[string]any); ok {
		return firstString(object, "command", "cmd", "script")
	}
	return ""
}

func argumentPath(arguments string) string {
	var value any
	if json.Unmarshal([]byte(arguments), &value) != nil {
		return ""
	}
	if object, ok := value.(map[string]any); ok {
		return firstString(object, "path", "file", "filename", "filePath", "file_path")
	}
	return ""
}

func decodeObject(data []byte) (map[string]any, error) {
	var value map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if value == nil {
		return nil, fmt.Errorf("payload is not an object")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("payload contains trailing data")
		}
		return nil, err
	}
	return value, nil
}

func tokenUsageFromMessage(raw map[string]any) (usage.TokenUsage, bool) {
	tokens := mapValue(raw, "tokens")
	if tokens == nil {
		tokens = mapValue(raw, "usage")
	}
	if tokens == nil {
		return usage.TokenUsage{}, false
	}
	result := usage.TokenUsage{}
	result.InputTokens = integerValue(tokens, "input", "input_tokens")
	result.OutputTokens = integerValue(tokens, "output", "output_tokens")
	result.ReasoningOutputTokens = integerValue(tokens, "reasoning", "reasoning_tokens", "reasoning_output_tokens")
	cache := mapValue(tokens, "cache")
	if cache != nil {
		result.CachedInputTokens = integerValue(cache, "read", "cached", "cached_input_tokens")
		result.CacheWriteInputTokens = integerValue(cache, "write", "cache_write", "cache_write_input_tokens")
	}
	if total, ok := integerValueOK(tokens, "total", "total_tokens"); ok {
		result.TotalTokens = total
	} else {
		result.TotalTokens = result.InputTokens + result.OutputTokens
	}
	return result, true
}

func integerValue(value map[string]any, keys ...string) int64 {
	result, _ := integerValueOK(value, keys...)
	return result
}

func integerValueOK(value map[string]any, keys ...string) (int64, bool) {
	for _, key := range keys {
		raw, ok := value[key]
		if !ok {
			continue
		}
		switch typed := raw.(type) {
		case json.Number:
			parsed, err := typed.Int64()
			if err == nil {
				return parsed, true
			}
		case float64:
			return int64(typed), true
		case int64:
			return typed, true
		case int:
			return int64(typed), true
		}
	}
	return 0, false
}

func onlyInjectedSkill(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || !strings.HasPrefix(strings.ToLower(text), "<skill") {
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

func joinTexts(values []textObservation) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value.text) != "" {
			parts = append(parts, value.text)
		}
	}
	return strings.Join(parts, "\n")
}

func compactType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, "-", "")
	value = strings.ReplaceAll(value, " ", "")
	return value
}

func rawValue(value map[string]any, keys ...string) any {
	for _, key := range keys {
		if child, ok := value[key]; ok {
			return child
		}
	}
	return nil
}

func mapValue(value map[string]any, key string) map[string]any {
	child, _ := value[key].(map[string]any)
	return child
}

func boolValue(value map[string]any, key string) bool {
	child, _ := value[key].(bool)
	return child
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
	child, ok := value[key]
	if !ok || child == nil {
		return ""
	}
	if text, ok := child.(string); ok {
		return strings.TrimSpace(text)
	}
	if number, ok := child.(json.Number); ok {
		return number.String()
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
		for _, key := range []string{"text", "value", "content", "input", "message"} {
			if text := valueText(typed[key]); text != "" {
				return text
			}
		}
	}
	return ""
}

type warningCollector struct {
	items []usage.Warning
}

func (c *warningCollector) add(reason, path, typ string) {
	c.addValue(usage.Warning{Reason: reason, Path: path, Type: typ, Count: 1})
}

func (c *warningCollector) addType(reason, typ, path, location string) {
	c.addValue(usage.Warning{Reason: reason, Type: typ, Path: path, Count: 1})
}

func (c *warningCollector) addValue(value usage.Warning) {
	for i := range c.items {
		current := &c.items[i]
		if current.Reason == value.Reason && current.Type == value.Type && current.Path == value.Path {
			current.Count += value.Count
			return
		}
	}
	c.items = append(c.items, value)
}

func (c *warningCollector) values() []usage.Warning {
	return append([]usage.Warning(nil), c.items...)
}
