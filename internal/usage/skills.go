package usage

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	skillBlockRE         = regexp.MustCompile(`(?is)<(?:skill|skill_content)(?:\s+[^>]*)?>(.*?)</(?:skill|skill_content)\s*>`)
	attrRE               = regexp.MustCompile(`(?i)(?:name|skill|skill_name|path)\s*=\s*["']([^"']+)["']`)
	requestRE            = regexp.MustCompile(`^\s*\$([A-Za-z0-9][A-Za-z0-9_.:-]*)\b`)
	frontNameRE          = regexp.MustCompile(`(?im)^\s*name\s*:\s*["']?([^\s"']+)["']?\s*$`)
	tagNameRE            = regexp.MustCompile(`(?is)<(?:name|skill_name)\s*>\s*([^<\s]+)\s*</(?:name|skill_name)\s*>`)
	skillNameRE          = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	qualifiedSkillNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*(?::[A-Za-z0-9][A-Za-z0-9_.-]*)*$`)
)

const maxSkillFileBytes = 128 * 1024

// DetectInjectedSkills recognizes an explicit structured skill block. It is
// kept as the compatibility helper for callers that already know the block is
// explicit; ingestion uses the mode-aware helpers because a block alone does
// not reveal how it was selected.
func DetectInjectedSkills(text, sessionID, turnID string, timestamp time.Time, source SourceRef) []SkillEvidence {
	return detectInjectedSkills(text, sessionID, turnID, ModeExplicit, MethodExplicitInjected, timestamp, source)
}

// DetectInjectedSkillsWithMode recognizes a structured block while preserving
// an activation mode supplied by the caller. A block is confirmed as loaded
// even when its activation mode is unknown.
func DetectInjectedSkillsWithMode(text, sessionID, turnID string, mode SkillMode, timestamp time.Time, source SourceRef) []SkillEvidence {
	return detectInjectedSkills(text, sessionID, turnID, mode, MethodSkillInjection, timestamp, source)
}

// DetectSelectedSkillInstructions recognizes the Codex metadata-backed
// selected-skill input item. The metadata proves that skill instructions were
// loaded, but not whether the selection was explicit or implicit.
func DetectSelectedSkillInstructions(text, sessionID, turnID string, timestamp time.Time, source SourceRef) []SkillEvidence {
	return detectInjectedSkills(text, sessionID, turnID, ModeUnknown, MethodSelectedSkillInstructions, timestamp, source)
}

// DetectRuntimeSkillItems recognizes Codex's completed UserMessage content
// items whose type is "skill". This is stronger than prose matching and is
// useful on records that contain the normalized user item but not the later
// selected-skill instruction message.
func DetectRuntimeSkillItems(value any, sessionID, turnID string, timestamp time.Time, source SourceRef) []SkillEvidence {
	candidates := make(map[string]struct{})
	collectRuntimeSkillItems(value, candidates)
	result := make([]SkillEvidence, 0, len(candidates))
	for name := range candidates {
		result = append(result, NewSkillEvidence(sessionID, turnID, name, ModeUnknown, MethodRuntimeSkillItem, StateConfirmed, timestamp, source))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].SkillName < result[j].SkillName })
	return result
}

func detectInjectedSkills(text, sessionID, turnID string, mode SkillMode, method SkillEvidenceMethod, timestamp time.Time, source SourceRef) []SkillEvidence {
	matches := skillBlockRE.FindAllStringSubmatch(text, -1)
	result := make([]SkillEvidence, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		name := ""
		if attrs := attrRE.FindStringSubmatch(match[0]); len(attrs) > 1 {
			name = skillNameFromValue(attrs[1])
		}
		if name == "" {
			name = skillNameFromBlock(match[1])
		}
		if name == "" {
			continue
		}
		result = append(result, NewSkillEvidence(sessionID, turnID, name, mode, method, StateConfirmed, timestamp, source))
	}
	return result
}

// DetectExplicitRequest recognizes a canonical skill marker at the beginning
// of an actual user prompt.
func DetectExplicitRequest(text, sessionID, turnID string, timestamp time.Time, source SourceRef) []SkillEvidence {
	match := requestRE.FindStringSubmatch(text)
	if len(match) < 2 {
		return nil
	}
	name := skillNameFromValue(match[1])
	if name == "" {
		return nil
	}
	return []SkillEvidence{NewSkillEvidence(sessionID, turnID, name, ModeExplicit, MethodExplicitRequest, StateUnconfirmed, timestamp, source)}
}

// DetectStructuredSkillTool extracts a skill only when a known Skill tool has
// exactly one unambiguous target in its structured arguments.
func DetectStructuredSkillTool(tool ToolObservation) []SkillEvidence {
	if !isSkillToolName(tool.RawName) && !isSkillToolName(tool.CanonicalName) {
		return nil
	}
	var value any
	if err := json.Unmarshal([]byte(tool.Arguments), &value); err != nil {
		return nil
	}
	candidates := make(map[string]struct{})
	collectSkillCandidates(value, candidates)
	if len(candidates) != 1 {
		return nil
	}
	for name := range candidates {
		return []SkillEvidence{NewSkillEvidence(tool.SessionID, tool.TurnID, name, ModeExplicit, MethodStructuredTool, StateConfirmed, tool.Timestamp, tool.Source)}
	}
	return nil
}

// DetectImplicitAccess recognizes known Skill paths in a runtime command. It
// performs no command execution and does not scan arbitrary filesystem paths.
func DetectImplicitAccess(command, sessionID, turnID string, timestamp time.Time, source SourceRef) []SkillEvidence {
	result := make([]SkillEvidence, 0)
	for _, candidate := range commandPaths(command) {
		name := SkillNameFromPath(candidate)
		if name == "" {
			continue
		}
		if frontmatter := FrontmatterSkillName(candidate); frontmatter != "" {
			name = frontmatter
		}
		result = append(result, NewSkillEvidence(sessionID, turnID, name, ModeImplicit, MethodImplicitAccess, StateInferred, timestamp, source))
	}
	return dedupeEvidence(result)
}

// SkillNameFromPath resolves a safe directory fallback for paths below a
// recognizable skills root, including paths to SKILL.md and scripts.
func SkillNameFromPath(path string) string {
	path = strings.Trim(path, " \t\r\n\"'`;,()")
	if strings.ContainsAny(path, "*?{}[]$|") {
		return ""
	}
	path = filepath.ToSlash(filepath.Clean(path))
	parts := strings.Split(path, "/")
	for i := 0; i < len(parts); i++ {
		if !strings.EqualFold(parts[i], "skills") || i+1 >= len(parts) {
			continue
		}
		nameIndex := i + 1
		// The bundled Codex skills use .codex/skills/.system/<name>/SKILL.md.
		// Treat the scope directory as part of the root, not the skill name.
		if strings.EqualFold(parts[nameIndex], ".system") {
			nameIndex++
		}
		if nameIndex >= len(parts) {
			continue
		}
		name := strings.TrimSpace(parts[nameIndex])
		if name == "" || !skillNameRE.MatchString(name) || nameIndex+1 >= len(parts) || !skillPathTarget(parts[nameIndex+1]) {
			continue
		}
		return qualifiedSkillName(skillNamespaceAt(parts, i), name)
	}
	return ""
}

func skillPathTarget(value string) bool {
	return strings.EqualFold(value, "SKILL.md") || strings.EqualFold(value, "scripts")
}

func skillNamespaceAt(parts []string, skillsIndex int) string {
	// Plugin cache paths have the generic shape
	// plugins/cache/<plugin>/<namespace>/<version>/skills/<skill>.
	if skillsIndex < 5 || !strings.EqualFold(parts[skillsIndex-5], "plugins") || !strings.EqualFold(parts[skillsIndex-4], "cache") {
		return ""
	}
	namespace := strings.TrimSpace(parts[skillsIndex-2])
	if !skillNameRE.MatchString(namespace) {
		return ""
	}
	return namespace
}

func qualifiedSkillName(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + ":" + name
}

// FrontmatterSkillName reads only an already-recognized SKILL.md path. A
// missing/deleted skill file is intentionally treated as a normal fallback.
func FrontmatterSkillName(path string) string {
	pathName := SkillNameFromPath(path)
	if pathName == "" || !strings.EqualFold(filepath.Base(path), "SKILL.md") || !knownSkillRoot(path) {
		return ""
	}
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxSkillFileBytes+1))
	if err != nil || len(data) > maxSkillFileBytes {
		return ""
	}
	match := frontNameRE.FindSubmatch(data)
	if len(match) < 2 {
		return ""
	}
	name := strings.TrimSpace(string(match[1]))
	if !qualifiedSkillNameRE.MatchString(name) {
		return ""
	}
	if separator := strings.IndexByte(pathName, ':'); separator > 0 && !strings.Contains(name, ":") {
		return qualifiedSkillName(pathName[:separator], name)
	}
	return name
}

func knownSkillRoot(path string) bool {
	parts := strings.Split(filepath.ToSlash(strings.TrimSpace(path)), "/")
	for i, part := range parts {
		if !strings.EqualFold(part, "skills") {
			continue
		}
		if i == 0 || (i > 0 && (strings.EqualFold(parts[i-1], ".agents") || strings.EqualFold(parts[i-1], ".codex"))) || skillNamespaceAt(parts, i) != "" {
			return true
		}
	}
	return false
}

// MergeSkillEvidence deduplicates evidence by (session, turn, skill), keeping
// all methods and the strongest state.
func MergeSkillEvidence(evidence []SkillEvidence) []SkillUse {
	type entry struct {
		use     SkillUse
		methods map[SkillEvidenceMethod]struct{}
		modes   map[SkillMode]struct{}
	}
	// A user request and a later injected block may be recorded as separate
	// events. Resolve that relationship before deduplicating so event order does
	// not turn an explicit load into an unknown one.
	explicitRequests := make(map[string]struct{}, len(evidence))
	for _, item := range evidence {
		if item.Method == MethodExplicitRequest {
			explicitRequests[skillEvidenceKey(item)] = struct{}{}
		}
	}
	entries := make(map[string]*entry, len(evidence))
	for _, item := range evidence {
		if strings.TrimSpace(item.SkillName) == "" {
			continue
		}
		item.Mode, item.Method = resolveInjectionMode(item, explicitRequests)
		if item.Mode == "" {
			item.Mode = ModeUnknown
		}
		key := skillEvidenceKey(item)
		current, ok := entries[key]
		if !ok {
			current = &entry{use: NewSkillUse(item.SessionID, item.TurnID, item.SkillName, item.Mode, item.State, item.Timestamp, item.Source), methods: make(map[SkillEvidenceMethod]struct{}), modes: make(map[SkillMode]struct{})}
			entries[key] = current
		}
		current.methods[item.Method] = struct{}{}
		current.modes[item.Mode] = struct{}{}
		if stateRank(item.State) > stateRank(current.use.State) {
			current.use.State = item.State
		}
		if modeRank(item.Mode) > modeRank(current.use.Mode) {
			current.use.Mode = item.Mode
		}
		if current.use.Timestamp.IsZero() || (!item.Timestamp.IsZero() && item.Timestamp.Before(current.use.Timestamp)) {
			current.use.Timestamp = item.Timestamp
			current.use.Source = item.Source
		}
	}
	result := make([]SkillUse, 0, len(entries))
	for _, current := range entries {
		current.use.Methods = make([]SkillEvidenceMethod, 0, len(current.methods))
		for method := range current.methods {
			current.use.Methods = append(current.use.Methods, method)
		}
		sort.Slice(current.use.Methods, func(i, j int) bool { return current.use.Methods[i] < current.use.Methods[j] })
		current.use.Modes = make([]SkillMode, 0, len(current.modes))
		for mode := range current.modes {
			current.use.Modes = append(current.use.Modes, mode)
		}
		sort.Slice(current.use.Modes, func(i, j int) bool { return current.use.Modes[i] < current.use.Modes[j] })
		result = append(result, current.use)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Timestamp.Equal(result[j].Timestamp) {
			if result[i].SessionID == result[j].SessionID {
				if result[i].TurnID == result[j].TurnID {
					return result[i].SkillName < result[j].SkillName
				}
				return result[i].TurnID < result[j].TurnID
			}
			return result[i].SessionID < result[j].SessionID
		}
		return result[i].Timestamp.Before(result[j].Timestamp)
	})
	return result
}

func resolveInjectionMode(item SkillEvidence, explicitRequests map[string]struct{}) (SkillMode, SkillEvidenceMethod) {
	if item.Method != MethodSkillInjection && item.Method != MethodSelectedSkillInstructions && item.Method != MethodRuntimeSkillItem {
		return item.Mode, item.Method
	}
	key := skillEvidenceKey(item)
	if _, ok := explicitRequests[key]; ok {
		return ModeExplicit, item.Method
	}
	if item.Mode == "" {
		return ModeUnknown, item.Method
	}
	return item.Mode, item.Method
}

func skillEvidenceKey(item SkillEvidence) string {
	return sourceIdentity(item.Source) + "\x00" + item.SessionID + "\x00" + item.TurnID + "\x00" + item.SkillName
}

func sourceIdentity(source SourceRef) string {
	return string(source.Source) + "\x00" + source.Agent + "\x00" + source.Provider + "\x00" + source.ProviderSessionID + "\x00" + source.CtxSessionID
}

func modeRank(mode SkillMode) int {
	switch mode {
	case ModeExplicit:
		return 3
	case ModeImplicit:
		return 2
	case ModeUnknown:
		return 1
	default:
		return 0
	}
}

func stateRank(state SkillState) int {
	switch state {
	case StateConfirmed:
		return 3
	case StateInferred:
		return 2
	case StateUnconfirmed:
		return 1
	default:
		return 0
	}
}

func skillNameFromBlock(body string) string {
	if match := tagNameRE.FindStringSubmatch(body); len(match) > 1 {
		if name := skillNameFromValue(match[1]); name != "" {
			return name
		}
	}
	if match := frontNameRE.FindStringSubmatch(body); len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if name := skillNameFromValue(line); name != "" && !strings.ContainsAny(line, " \t") {
			return name
		}
		break
	}
	return ""
}

func skillNameFromValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "$")
	if strings.Contains(value, "/") || strings.EqualFold(filepath.Base(value), "SKILL.md") {
		if name := SkillNameFromPath(value); name != "" {
			return name
		}
	}
	value = strings.Trim(value, " \t\r\n\"'`;,()[]{}")
	if strings.ContainsAny(value, " \t\r\n") {
		return ""
	}
	if !qualifiedSkillNameRE.MatchString(value) {
		return ""
	}
	return value
}

func isSkillToolName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return name == "skill" || name == "skills.read" || strings.HasSuffix(name, ".skills.read") || strings.HasSuffix(name, "/skills.read") || strings.HasSuffix(name, "__skills.read")
}

func collectSkillCandidates(value any, result map[string]struct{}) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			lower := strings.ToLower(strings.TrimSpace(key))
			if lower == "name" || lower == "skill" || lower == "skill_name" || lower == "skillname" || lower == "slug" || lower == "path" {
				if text, ok := child.(string); ok {
					if name := skillNameFromValue(text); name != "" {
						result[name] = struct{}{}
					}
				}
			}
			collectSkillCandidates(child, result)
		}
	case []any:
		for _, child := range typed {
			collectSkillCandidates(child, result)
		}
	}
}

func collectRuntimeSkillItems(value any, result map[string]struct{}) {
	switch typed := value.(type) {
	case map[string]any:
		typeName, _ := typed["type"].(string)
		if compactSkillType(typeName) == "skill" {
			for _, key := range []string{"name", "skill", "skill_name", "skillName", "path"} {
				if text, ok := typed[key].(string); ok {
					if name := skillNameFromValue(text); name != "" {
						result[name] = struct{}{}
					}
				}
			}
		}
		for _, child := range typed {
			collectRuntimeSkillItems(child, result)
		}
	case []any:
		for _, child := range typed {
			collectRuntimeSkillItems(child, result)
		}
	}
}

func compactSkillType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, "-", "")
	value = strings.ReplaceAll(value, " ", "")
	return value
}

func commandPaths(command string) []string {
	result := make([]string, 0)
	seen := make(map[string]struct{})
	collectCommandPaths(command, &result, seen, 0)
	return result
}

// shellToken is deliberately smaller than a shell parser. It preserves quoted
// words and recognizes command separators, which is enough to identify literal
// paths passed to a small allowlist of file readers and script runners.
type shellToken struct {
	text      string
	separator bool
}

func collectCommandPaths(command string, result *[]string, seen map[string]struct{}, depth int) {
	if depth > 8 {
		return
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return
	}
	if argv, ok := decodeCommandArgv(command); ok {
		collectCommandArgv(argv, result, seen, depth+1)
		return
	}
	for _, segment := range shellCommandSegments(shellTokens(command)) {
		collectCommandSegment(segment, result, seen, depth+1)
	}
}

func decodeCommandArgv(command string) ([]string, bool) {
	if !strings.HasPrefix(strings.TrimSpace(command), "[") {
		return nil, false
	}
	var argv []string
	if err := json.Unmarshal([]byte(command), &argv); err != nil || len(argv) == 0 {
		return nil, false
	}
	return argv, true
}

func collectCommandArgv(argv []string, result *[]string, seen map[string]struct{}, depth int) {
	if len(argv) == 0 || depth > 8 {
		return
	}
	collectCommandSegment(argv, result, seen, depth)
}

func shellTokens(command string) []shellToken {
	tokens := make([]shellToken, 0)
	var word strings.Builder
	inSingle, inDouble, inBacktick, escaped, started := false, false, false, false, false
	flush := func() {
		if started {
			tokens = append(tokens, shellToken{text: word.String()})
			word.Reset()
			started = false
		}
	}
	appendSeparator := func(value string) {
		flush()
		tokens = append(tokens, shellToken{text: value, separator: true})
	}
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if escaped {
			word.WriteByte(ch)
			started = true
			escaped = false
			continue
		}
		if inSingle {
			if ch == '\'' {
				inSingle = false
			} else {
				word.WriteByte(ch)
			}
			started = true
			continue
		}
		if inDouble {
			switch ch {
			case '"':
				inDouble = false
			case '\\':
				escaped = true
			default:
				word.WriteByte(ch)
			}
			started = true
			continue
		}
		if inBacktick {
			if ch == '`' {
				inBacktick = false
			} else {
				word.WriteByte(ch)
			}
			started = true
			continue
		}
		switch ch {
		case '\\':
			escaped = true
			started = true
		case '\'', '"', '`':
			inSingle, inDouble, inBacktick = ch == '\'', ch == '"', ch == '`'
			started = true
		case ' ', '\t', '\r', '\n':
			flush()
		case ';':
			appendSeparator(";")
		case '|':
			if i+1 < len(command) && command[i+1] == '|' {
				i++
				appendSeparator("||")
			} else {
				appendSeparator("|")
			}
		case '&':
			if i+1 < len(command) && command[i+1] == '&' {
				i++
				appendSeparator("&&")
			} else {
				appendSeparator("&")
			}
		case '<', '>':
			appendSeparator(string(ch))
		default:
			word.WriteByte(ch)
			started = true
		}
	}
	if escaped {
		word.WriteByte('\\')
		started = true
	}
	flush()
	return tokens
}

func shellCommandSegments(tokens []shellToken) [][]string {
	segments := make([][]string, 0)
	current := make([]string, 0)
	for _, token := range tokens {
		if token.separator {
			if len(current) > 0 {
				segments = append(segments, current)
				current = nil
			}
			continue
		}
		current = append(current, token.text)
	}
	if len(current) > 0 {
		segments = append(segments, current)
	}
	return segments
}

func collectCommandSegment(words []string, result *[]string, seen map[string]struct{}, depth int) {
	if len(words) == 0 || depth > 8 {
		return
	}
	words = stripAssignments(words)
	if nested, ok := nestedRTKCommand(words); ok {
		collectCommandPaths(nested, result, seen, depth+1)
		return
	}
	for len(words) > 0 {
		base := commandBase(words[0])
		if shellCommand(base) {
			if nested, ok := nestedShellCommand(words); ok {
				collectCommandPaths(nested, result, seen, depth+1)
				return
			}
			break
		}
		if wrapperCommand(base) {
			words = unwrapCommand(words)
			words = stripAssignments(words)
			continue
		}
		break
	}
	if len(words) == 0 {
		return
	}
	base := commandBase(words[0])
	if readerCommand(base) {
		for _, path := range words[1:] {
			appendCommandPath(path, result, seen)
		}
		return
	}
	if scriptRunnerCommand(base) {
		if path, ok := scriptPath(words[1:], base); ok {
			appendCommandPath(path, result, seen)
		}
	}
}

// nestedRTKCommand unwraps rtk's command-string form. Unlike rtk proxy,
// rtk run -c passes the actual shell command as one argument, so the normal
// wrapper unwrapping would stop at the -c flag before reaching the reader.
func nestedRTKCommand(words []string) (string, bool) {
	if len(words) < 4 || commandBase(words[0]) != "rtk" {
		return "", false
	}
	switch commandBase(words[1]) {
	case "run", "exec", "command":
	default:
		return "", false
	}
	for i := 2; i+1 < len(words); i++ {
		switch strings.ToLower(words[i]) {
		case "-c", "--command":
			if strings.TrimSpace(words[i+1]) != "" {
				return words[i+1], true
			}
		}
	}
	return "", false
}

func stripAssignments(words []string) []string {
	for len(words) > 0 && shellAssignment(words[0]) {
		words = words[1:]
	}
	return words
}

func shellAssignment(value string) bool {
	index := strings.IndexByte(value, '=')
	if index <= 0 {
		return false
	}
	name := value[:index]
	for i, ch := range name {
		valid := ch == '_' || ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || i > 0 && ch >= '0' && ch <= '9'
		if !valid {
			return false
		}
	}
	return true
}

func commandBase(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "<>;,()[]{}")
	value = filepath.ToSlash(value)
	return strings.ToLower(filepath.Base(value))
}

func wrapperCommand(base string) bool {
	switch base {
	case "rtk", "proxy", "run", "exec", "command", "sudo", "env":
		return true
	default:
		return false
	}
}

func unwrapCommand(words []string) []string {
	if len(words) == 0 {
		return words
	}
	base := commandBase(words[0])
	words = words[1:]
	switch base {
	case "rtk":
		if len(words) > 0 {
			subcommand := commandBase(words[0])
			if subcommand == "proxy" || subcommand == "run" || subcommand == "exec" || subcommand == "command" {
				words = words[1:]
			}
		}
	case "sudo":
		for len(words) > 0 && strings.HasPrefix(words[0], "-") {
			if words[0] == "--" {
				return words[1:]
			}
			option := words[0]
			words = words[1:]
			if sudoOptionTakesValue(option) && len(words) > 0 {
				words = words[1:]
			}
		}
	case "env":
		for len(words) > 0 && (shellAssignment(words[0]) || strings.HasPrefix(words[0], "-")) {
			words = words[1:]
		}
	}
	return words
}

func nestedShellCommand(words []string) (string, bool) {
	for i := 1; i+1 < len(words); i++ {
		switch strings.ToLower(words[i]) {
		case "-c", "-lc", "-ic", "--command", "-command":
			return words[i+1], true
		}
	}
	return "", false
}

func shellCommand(base string) bool {
	switch base {
	case "sh", "bash", "dash", "zsh", "ksh", "fish", "csh", "tcsh", "cmd", "pwsh", "powershell":
		return true
	default:
		return false
	}
}

func readerCommand(base string) bool {
	switch base {
	case "cat", "sed", "head", "tail", "less", "more", "bat", "awk", "gawk", "type", "get-content", "gc":
		return true
	default:
		return false
	}
}

func scriptRunnerCommand(base string) bool {
	if base == "python" || base == "python3" || base == "node" || base == "deno" || base == "ruby" || base == "perl" || base == "pwsh" || base == "powershell" {
		return true
	}
	if base == "sh" || base == "bash" || base == "dash" || base == "zsh" || base == "ksh" || base == "fish" {
		return true
	}
	return strings.HasPrefix(base, "python3.") || strings.HasPrefix(base, "python2.")
}

func scriptPath(args []string, runner string) (string, bool) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if i+1 < len(args) && SkillNameFromPath(args[i+1]) != "" {
				return args[i+1], true
			}
			return "", false
		}
		if !strings.HasPrefix(arg, "-") {
			if SkillNameFromPath(arg) != "" {
				return arg, true
			}
			continue
		}
		lower := strings.ToLower(arg)
		if lower == "-c" || lower == "--command" || (lower == "-e" || lower == "--eval") && !shellScriptRunner(runner) || shellScriptRunner(runner) && lower == "-s" {
			return "", false
		}
		if lower == "-m" || lower == "--module" {
			i++
		}
	}
	return "", false
}

func shellScriptRunner(runner string) bool {
	switch runner {
	case "sh", "bash", "dash", "zsh", "ksh", "fish":
		return true
	default:
		return false
	}
}

func sudoOptionTakesValue(option string) bool {
	option = strings.ToLower(option)
	for _, value := range []string{"-u", "--user", "-g", "--group", "-h", "--host", "-r", "--chroot", "-c", "--chdir", "-c", "--close-from"} {
		if option == value {
			return true
		}
	}
	return false
}

func appendCommandPath(path string, result *[]string, seen map[string]struct{}) {
	path = strings.TrimSpace(path)
	if path == "" || SkillNameFromPath(path) == "" {
		return
	}
	key := filepath.Clean(path)
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	*result = append(*result, path)
}

func dedupeEvidence(in []SkillEvidence) []SkillEvidence {
	seen := make(map[string]struct{}, len(in))
	out := make([]SkillEvidence, 0, len(in))
	for _, item := range in {
		key := sourceIdentity(item.Source) + "\x00" + item.SessionID + "\x00" + item.TurnID + "\x00" + item.SkillName + "\x00" + string(item.Method)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}
