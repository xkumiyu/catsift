package ctx

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/xkumiyu/catsift/internal/usage"
)

func jsonLine(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func eventLine(t *testing.T, id, provider, session, eventType, role, when, text string, activity map[string]any) string {
	t.Helper()
	event := map[string]any{
		"ctx_event_id":        id,
		"ctx_session_id":      "ctx-" + provider,
		"event_type":          eventType,
		"occurred_at":         when,
		"provider":            provider,
		"provider_session_id": session,
		"role":                role,
		"content":             map[string]any{"text": text, "activity": activity},
	}
	return jsonLine(t, map[string]any{"record_type": "event_range_event", "event": event})
}

func completionLine(t *testing.T, generation, cursor string, terminal bool) string {
	t.Helper()
	value := map[string]any{
		"record_type":   "event_range_completion",
		"generation_id": generation,
		"terminal":      terminal,
		"truncated":     !terminal,
	}
	if cursor != "" {
		value["next_cursor"] = cursor
	}
	return jsonLine(t, value)
}

func TestLoadReadsAllPagesAndKeepsAgentSessionIdentity(t *testing.T) {
	first := strings.Join([]string{
		eventLine(t, "codex-message", "codex", "shared", "message", "user", "2026-01-01T00:00:00Z", "$review", nil),
		eventLine(t, "codex-tool", "codex", "shared", "tool_call", "assistant", "2026-01-01T00:00:01Z", "", map[string]any{
			"invocation": map[string]any{"id": "call-codex", "protocol": "command", "name": "exec", "arguments": map[string]any{"cmd": "true"}},
		}),
		completionLine(t, "generation-1", "cursor-1", false),
	}, "\n") + "\n"
	second := strings.Join([]string{
		eventLine(t, "opencode-message", "opencode", "shared", "message", "user", "2026-01-01T00:00:02Z", "$review", nil),
		eventLine(t, "opencode-tool", "opencode", "shared", "command_completion", "tool", "2026-01-01T00:00:03Z", "", map[string]any{
			"invocation": map[string]any{"id": "call-opencode", "protocol": "command", "name": "exec", "arguments": map[string]any{"cmd": "false"}},
			"result":     map[string]any{"status": "failed"},
		}),
		completionLine(t, "generation-1", "", true),
	}, "\n") + "\n"
	var calls [][]string
	runner := func(args []string) (CommandResult, error) {
		calls = append(calls, append([]string(nil), args...))
		for _, arg := range args {
			if arg == "cursor-1" {
				return CommandResult{Stdout: []byte(second)}, nil
			}
		}
		return CommandResult{Stdout: []byte(first)}, nil
	}

	result, err := Load("/tmp/ctx", IngestOptions{DataRoot: "/tmp/ctx", Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || !containsPair(calls[1], "--cursor", "cursor-1") {
		t.Fatalf("pagination calls = %#v", calls)
	}
	if len(result.Sessions) != 2 || len(result.Agents) != 2 || result.Agents[0] != "codex" || result.Agents[1] != "opencode" {
		t.Fatalf("sessions/agents = %#v / %#v", result.Sessions, result.Agents)
	}
	if len(result.Turns) != 2 || result.Turns[0].Source.Agent != "codex" || result.Turns[1].Source.Agent != "opencode" {
		t.Fatalf("turns = %#v", result.Turns)
	}
	for _, session := range result.Sessions {
		if session.Provider == "codex" && (!session.CreatedAt.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) || !session.UpdatedAt.Equal(time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC))) {
			t.Fatalf("codex session time range = %v to %v", session.CreatedAt, session.UpdatedAt)
		}
		if session.Provider == "opencode" && (!session.CreatedAt.Equal(time.Date(2026, 1, 1, 0, 0, 2, 0, time.UTC)) || !session.UpdatedAt.Equal(time.Date(2026, 1, 1, 0, 0, 3, 0, time.UTC))) {
			t.Fatalf("opencode session time range = %v to %v", session.CreatedAt, session.UpdatedAt)
		}
	}
	if result.Turns[0].SessionID == result.Turns[1].SessionID {
		t.Fatal("same provider session ID collided across agents")
	}
	if len(usage.EffectiveTools(result.Turns[0])) != 1 || len(usage.EffectiveTools(result.Turns[1])) != 1 {
		t.Fatalf("effective tools = %#v / %#v", result.Turns[0], result.Turns[1])
	}
	if got := usage.EffectiveTools(result.Turns[1])[0].Status; got != usage.StatusFailure {
		t.Fatalf("failed tool status = %q", got)
	}
}

func TestLoadCapturesModelFromProviderPayload(t *testing.T) {
	data := strings.Join([]string{
		eventLine(t, "model-event", "codex", "session", "message", "assistant", "2026-01-01T00:00:00Z", "", map[string]any{
			"model": "gpt-example",
		}),
		completionLine(t, "generation-1", "", true),
	}, "\n") + "\n"
	runner := func([]string) (CommandResult, error) {
		return CommandResult{Stdout: []byte(data)}, nil
	}
	result, err := Load("/tmp/ctx", IngestOptions{Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || len(result.Turns[0].ModelObservations) != 1 {
		t.Fatalf("model observations = %#v", result.Turns)
	}
	want := usage.NewModelRef("codex", "gpt-example")
	if got := result.Turns[0].ModelObservations[0].Model; got != want {
		t.Fatalf("model = %#v, want %#v", got, want)
	}
}

func TestFilterCtxTurnPreservesModelObservations(t *testing.T) {
	when := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	model := usage.NewModelRef("ctx", "gpt-example")
	turn := usage.NewTurn("session", "turn", 1, usage.NewCtxSourceRef("ctx://event", "ctx", "provider-session", "ctx-session", "event"))
	turn.StartedAt = when.Add(-time.Hour)
	turn.EndedAt = when.Add(-time.Hour + time.Second)
	turn.ModelObservations = []usage.ModelObservation{{Model: model, Timestamp: when}}

	filtered, ok := filterCtxTurn(turn, when.Add(-time.Second), when.Add(time.Second))
	if !ok {
		t.Fatal("model observation should keep the turn")
	}
	if len(filtered.ModelObservations) != 1 || filtered.ModelObservations[0].Model != model {
		t.Fatalf("model observations = %#v", filtered.ModelObservations)
	}
}

func TestLoadRejectsIncompleteEventStream(t *testing.T) {
	runner := func([]string) (CommandResult, error) {
		return CommandResult{Stdout: []byte(`{"record_type":"event_range_event","event":{"event_type":"message"}}
`)}, nil
	}
	if _, err := Load("/tmp/ctx", IngestOptions{Runner: runner}); err == nil || !strings.Contains(err.Error(), "completion") {
		t.Fatalf("incomplete stream error = %v", err)
	}
}

func TestLoadRecoversUnknownAndMalformedEventsWithWarnings(t *testing.T) {
	data := strings.Join([]string{
		eventLine(t, "known", "codex", "session", "message", "user", time.Unix(1, 0).UTC().Format(time.RFC3339), "hello", nil),
		`{"record_type":"event_range_event","event":{"ctx_event_id":"future","event_type":"future_tool_event","provider":"codex"}}`,
		"not-json",
		completionLine(t, "generation-1", "", true),
	}, "\n") + "\n"
	runner := func([]string) (CommandResult, error) {
		return CommandResult{Stdout: []byte(data)}, nil
	}
	result, err := Load("/tmp/ctx", IngestOptions{Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || len(result.Warnings) != 2 {
		t.Fatalf("turns/warnings = %#v / %#v", result.Turns, result.Warnings)
	}
}

func TestLoadIgnoresKnownSummaryEvents(t *testing.T) {
	data := strings.Join([]string{
		eventLine(t, "summary", "codex", "session", "summary", "assistant", "2026-01-01T00:00:00Z", "session summary", nil),
		completionLine(t, "generation-1", "", true),
	}, "\n") + "\n"
	runner := func([]string) (CommandResult, error) {
		return CommandResult{Stdout: []byte(data)}, nil
	}
	result, err := Load("/tmp/ctx", IngestOptions{Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 0 || len(result.Warnings) != 0 {
		t.Fatalf("summary event = %#v warnings = %#v", result.Turns, result.Warnings)
	}
}

func TestLoadReportsCommandFailure(t *testing.T) {
	runner := func([]string) (CommandResult, error) {
		return CommandResult{Stderr: []byte("data root is unavailable"), ExitCode: 1}, nil
	}
	if _, err := Load("/tmp/ctx", IngestOptions{Runner: runner}); err == nil || !strings.Contains(err.Error(), "data root is unavailable") {
		t.Fatalf("command error = %v", err)
	}
}

func TestLoadIsolatesEventsWithoutAgentIdentity(t *testing.T) {
	data := strings.Join([]string{
		`{"record_type":"event_range_event","event":{"ctx_event_id":"unknown","ctx_session_id":"ctx-session","event_type":"message","role":"user","occurred_at":"2026-01-01T00:00:00Z","content":{"text":"hello"}}}`,
		completionLine(t, "generation-1", "", true),
	}, "\n") + "\n"
	runner := func([]string) (CommandResult, error) {
		return CommandResult{Stdout: []byte(data)}, nil
	}
	result, err := Load("/tmp/ctx", IngestOptions{Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Agents) != 1 || result.Agents[0] != "unknown" || len(result.Warnings) != 1 || result.Warnings[0].Reason != "ctx_missing_agent" {
		t.Fatalf("unknown agent = %#v warnings = %#v", result.Agents, result.Warnings)
	}
	if len(result.Turns) != 1 || result.Turns[0].Source.Agent != "unknown" {
		t.Fatalf("unknown turn = %#v", result.Turns)
	}
}

func TestLoadForwardsMillisecondAlignedTimeRange(t *testing.T) {
	now := time.Date(2026, 1, 10, 0, 0, 0, 123456789, time.UTC)
	var args []string
	runner := func(value []string) (CommandResult, error) {
		args = append([]string(nil), value...)
		return CommandResult{Stdout: []byte(completionLine(t, "generation-1", "", true) + "\n")}, nil
	}
	if _, err := Load("/tmp/ctx", IngestOptions{Days: 2, DaysSet: true, Now: now, Runner: runner}); err != nil {
		t.Fatal(err)
	}
	if !containsPair(args, "--since", "2026-01-08T00:00:00.123Z") || !containsPair(args, "--until", "2026-01-10T00:00:00.123Z") {
		t.Fatalf("time range args = %#v", args)
	}
}

func TestLoadForwardsDateRange(t *testing.T) {
	var args []string
	runner := func(value []string) (CommandResult, error) {
		args = append([]string(nil), value...)
		return CommandResult{Stdout: []byte(completionLine(t, "generation-1", "", true) + "\n")}, nil
	}
	from := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC)
	if _, err := Load("/tmp/ctx", IngestOptions{From: from, To: to, Now: time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC), Runner: runner}); err != nil {
		t.Fatal(err)
	}
	if !containsPair(args, "--since", "2026-01-02T00:00:00Z") || !containsPair(args, "--until", "2026-01-04T00:00:00Z") {
		t.Fatalf("date range args = %#v", args)
	}
}

func TestLoadForwardsNowAsUpperBoundForOpenEndedFrom(t *testing.T) {
	var args []string
	now := time.Date(2026, 1, 10, 12, 34, 56, 789000000, time.UTC)
	runner := func(value []string) (CommandResult, error) {
		args = append([]string(nil), value...)
		return CommandResult{Stdout: []byte(completionLine(t, "generation-1", "", true) + "\n")}, nil
	}
	from := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	if _, err := Load("/tmp/ctx", IngestOptions{From: from, Now: now, Runner: runner}); err != nil {
		t.Fatal(err)
	}
	if !containsPair(args, "--since", "2026-01-02T00:00:00Z") || !containsPair(args, "--until", "2026-01-10T12:34:56.789Z") {
		t.Fatalf("open-ended from args = %#v", args)
	}
}

func TestLoadKeepsOpenEndedToAsLocalFilter(t *testing.T) {
	var args []string
	runner := func(value []string) (CommandResult, error) {
		args = append([]string(nil), value...)
		return CommandResult{Stdout: []byte(completionLine(t, "generation-1", "", true) + "\n")}, nil
	}
	to := time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC)
	if _, err := Load("/tmp/ctx", IngestOptions{To: to, Now: time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC), Runner: runner}); err != nil {
		t.Fatal(err)
	}
	for _, arg := range args {
		if arg == "--since" || arg == "--until" {
			t.Fatalf("open-ended to args = %#v", args)
		}
	}
}

func TestLoadFiltersDateRange(t *testing.T) {
	data := strings.Join([]string{
		eventLine(t, "old", "codex", "old", "message", "user", "2026-01-01T23:59:59Z", "old", nil),
		eventLine(t, "selected", "codex", "selected", "message", "user", "2026-01-02T12:00:00Z", "selected", nil),
		eventLine(t, "new", "codex", "new", "message", "user", "2026-01-03T00:00:00Z", "new", nil),
		completionLine(t, "generation-1", "", true),
	}, "\n") + "\n"
	runner := func([]string) (CommandResult, error) {
		return CommandResult{Stdout: []byte(data)}, nil
	}
	result, err := Load("/tmp/ctx", IngestOptions{
		From:   time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		To:     time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC),
		Now:    time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC),
		Runner: runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || result.Turns[0].UserPrompts != 1 || result.Turns[0].Source.ProviderSessionID != "selected" || len(result.Sessions) != 1 {
		t.Fatalf("date range result = %#v", result)
	}
}

func TestLoadRejectsUnchangedCursor(t *testing.T) {
	data := completionLine(t, "generation-1", "cursor-1", false) + "\n"
	runner := func([]string) (CommandResult, error) {
		return CommandResult{Stdout: []byte(data)}, nil
	}
	if _, err := Load("/tmp/ctx", IngestOptions{Runner: runner}); err == nil || !strings.Contains(err.Error(), "unchanged cursor") {
		t.Fatalf("cursor error = %v", err)
	}
}

func TestLoadNormalizesSkillEvidenceAndIgnoresToolOutputAsPrompt(t *testing.T) {
	lines := []string{
		eventLine(t, "injected", "codex", "session", "message", "user", "2026-01-01T00:00:00Z", `<skill name="review">instructions</skill>`, nil),
		eventLine(t, "skill-tool", "codex", "session", "tool_call", "assistant", "2026-01-01T00:00:01Z", "", map[string]any{
			"invocation": map[string]any{"id": "skill-call", "name": "Skill", "arguments": map[string]any{"name": "review"}},
		}),
		eventLine(t, "runtime", "codex", "session", "command_completion", "tool", "2026-01-01T00:00:02Z", "", map[string]any{
			"invocation": map[string]any{"id": "runtime-call", "protocol": "command", "name": "exec", "arguments": map[string]any{"cmd": "cat /fixture-home/.agents/skills/review/SKILL.md"}},
			"result":     map[string]any{"status": "completed"},
		}),
		eventLine(t, "output", "codex", "session", "tool_result", "tool", "2026-01-01T00:00:03Z", "tool output", nil),
		completionLine(t, "generation-1", "", true),
	}
	runner := func([]string) (CommandResult, error) {
		return CommandResult{Stdout: []byte(strings.Join(lines, "\n") + "\n")}, nil
	}
	result, err := Load("/tmp/ctx", IngestOptions{Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || result.Turns[0].UserPrompts != 0 {
		t.Fatalf("injected prompt count = %#v", result.Turns)
	}
	turn := result.Turns[0]
	if len(turn.ModelTools) != 1 || turn.ModelTools[0].RawName != "Skill" {
		t.Fatalf("model tools = %#v", turn.ModelTools)
	}
	if len(turn.RuntimeTools) != 1 || turn.RuntimeTools[0].CanonicalName != "shell" {
		t.Fatalf("runtime tools = %#v", turn.RuntimeTools)
	}
	uses := usage.MergeSkillEvidence(turn.SkillEvidence)
	if len(uses) != 1 || uses[0].SkillName != "review" || uses[0].State != usage.StateConfirmed {
		t.Fatalf("skill uses = %#v", uses)
	}
	methods := strings.Join(stringMethods(uses[0].Methods), ",")
	if !strings.Contains(methods, string(usage.MethodSkillInjection)) || !strings.Contains(methods, string(usage.MethodStructuredTool)) || !strings.Contains(methods, string(usage.MethodImplicitAccess)) {
		t.Fatalf("skill methods = %q", methods)
	}
}

func TestLoadUnwrapsCtxContentSourceProjection(t *testing.T) {
	contentSourceLine := func(id, eventType, role, text string, sourceValue any) string {
		event := map[string]any{
			"ctx_event_id":        id,
			"ctx_session_id":      "ctx-session",
			"event_type":          eventType,
			"occurred_at":         "2026-01-01T00:00:00Z",
			"provider":            "codex",
			"provider_session_id": "provider-session",
			"role":                role,
			"content":             map[string]any{"complete": true, "source": map[string]any{"text": text}},
		}
		if sourceValue != nil {
			event["content"].(map[string]any)["source"].(map[string]any)["text"] = jsonLine(t, sourceValue)
		}
		return jsonLine(t, map[string]any{"record_type": "event_range_event", "event": event})
	}
	lines := []string{
		contentSourceLine("message", "message", "user", "", []any{map[string]any{"type": "input_text", "text": "$review"}}),
		contentSourceLine("tool-call", "tool_call", "assistant", "", map[string]any{
			"type":    "custom_tool_call",
			"name":    "exec",
			"call_id": "call-1",
			"input":   "{\"cmd\":\"true\"}",
			"internal_chat_message_metadata_passthrough": map[string]any{"turn_id": "provider-turn"},
		}),
		contentSourceLine("runtime-call", "tool_call", "tool", "", map[string]any{
			"type":    "CommandExecution",
			"id":      "runtime-1",
			"command": "true",
			"status":  "completed",
			"internal_chat_message_metadata_passthrough": map[string]any{"turn_id": "provider-turn"},
		}),
		contentSourceLine("tool-output", "tool_output", "tool", "", []any{map[string]any{"type": "input_text", "text": "output"}}),
		completionLine(t, "generation-1", "", true),
	}
	runner := func([]string) (CommandResult, error) {
		return CommandResult{Stdout: []byte(strings.Join(lines, "\n") + "\n")}, nil
	}
	result, err := Load("/tmp/ctx", IngestOptions{Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || result.Turns[0].UserPrompts != 1 || len(result.Turns[0].ModelTools) != 1 || len(result.Turns[0].RuntimeTools) != 1 {
		t.Fatalf("content source projection = %#v", result.Turns)
	}
	if got := result.Turns[0].ModelTools[0].CanonicalName; got != "exec" {
		t.Fatalf("model tool name = %q", got)
	}
	if got := usage.EffectiveTools(result.Turns[0]); len(got) != 1 || got[0].CanonicalName != "shell" {
		t.Fatalf("effective runtime tool = %#v", got)
	}
	uses := usage.MergeSkillEvidence(result.Turns[0].SkillEvidence)
	if len(uses) != 1 || uses[0].SkillName != "review" || uses[0].State != usage.StateUnconfirmed {
		t.Fatalf("skill marker = %#v", uses)
	}
}

func TestLoadUnwrapsCtxTopLevelSourceProjection(t *testing.T) {
	event := func(id, eventType, role string, sourceValue any) string {
		raw := map[string]any{
			"ctx_event_id":        id,
			"ctx_session_id":      "ctx-session",
			"event_type":          eventType,
			"occurred_at":         "2026-01-01T00:00:00Z",
			"provider":            "codex",
			"provider_session_id": "provider-session",
			"role":                role,
			"content":             map[string]any{"complete": true},
			"source":              map[string]any{"text": jsonLine(t, sourceValue)},
		}
		return jsonLine(t, map[string]any{"record_type": "event_range_event", "event": raw})
	}
	lines := []string{
		event("message", "message", "user", []any{map[string]any{"type": "input_text", "text": "$review"}}),
		event("tool-call", "tool_call", "assistant", map[string]any{
			"type":    "custom_tool_call",
			"name":    "exec",
			"call_id": "call-1",
			"input":   "{\"cmd\":\"true\"}",
		}),
		completionLine(t, "generation-1", "", true),
	}
	runner := func([]string) (CommandResult, error) {
		return CommandResult{Stdout: []byte(strings.Join(lines, "\n") + "\n")}, nil
	}

	result, err := Load("/tmp/ctx", IngestOptions{Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || len(result.Turns[0].ModelTools) != 1 {
		t.Fatalf("top-level source tool = %#v", result.Turns)
	}
	uses := usage.MergeSkillEvidence(result.Turns[0].SkillEvidence)
	if len(uses) != 1 || uses[0].SkillName != "review" {
		t.Fatalf("top-level source skill = %#v", uses)
	}
}

func TestLoadUnwrapsCtxEventTextProjection(t *testing.T) {
	event := func(id, eventType, role string, textValue any) string {
		raw := map[string]any{
			"ctx_event_id":        id,
			"ctx_session_id":      "ctx-session",
			"event_type":          eventType,
			"occurred_at":         "2026-01-01T00:00:00Z",
			"provider":            "codex",
			"provider_session_id": "provider-session",
			"role":                role,
			"content":             map[string]any{"complete": true},
			"text":                jsonLine(t, textValue),
		}
		return jsonLine(t, map[string]any{"record_type": "event_range_event", "event": raw})
	}
	lines := []string{
		event("message", "message", "user", []any{map[string]any{"type": "input_text", "text": "$review"}}),
		event("tool-call", "tool_call", "assistant", map[string]any{
			"type":    "custom_tool_call",
			"name":    "exec",
			"call_id": "call-1",
			"input":   "{\"cmd\":\"true\"}",
		}),
		completionLine(t, "generation-1", "", true),
	}
	runner := func([]string) (CommandResult, error) {
		return CommandResult{Stdout: []byte(strings.Join(lines, "\n") + "\n")}, nil
	}

	result, err := Load("/tmp/ctx", IngestOptions{Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || len(result.Turns[0].ModelTools) != 1 {
		t.Fatalf("event text tool = %#v", result.Turns)
	}
	uses := usage.MergeSkillEvidence(result.Turns[0].SkillEvidence)
	if len(uses) != 1 || uses[0].SkillName != "review" {
		t.Fatalf("event text skill = %#v", uses)
	}
}

func TestLoadDetectsSkillAccessInCtxExecPayload(t *testing.T) {
	event := func(id, eventType, role string, textValue any) string {
		raw := map[string]any{
			"ctx_event_id":        id,
			"ctx_session_id":      "ctx-session",
			"event_type":          eventType,
			"occurred_at":         "2026-01-01T00:00:00Z",
			"provider":            "codex",
			"provider_session_id": "provider-session",
			"role":                role,
			"content":             map[string]any{"complete": true},
			"text":                jsonLine(t, textValue),
		}
		return jsonLine(t, map[string]any{"record_type": "event_range_event", "event": raw})
	}
	lines := []string{
		event("message", "message", "user", "inspect the skill instructions"),
		event("tool-call", "tool_call", "assistant", map[string]any{
			"type":    "custom_tool_call",
			"name":    "exec",
			"call_id": "call-1",
			"status":  "completed",
			"input":   `const r = await tools.exec_command({"cmd":"rtk run -c 'cat /fixture-home/.agents/skills/review/SKILL.md'"});`,
		}),
		completionLine(t, "generation-1", "", true),
	}
	runner := func([]string) (CommandResult, error) {
		return CommandResult{Stdout: []byte(strings.Join(lines, "\n") + "\n")}, nil
	}

	result, err := Load("/tmp/ctx", IngestOptions{Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	uses := usage.MergeSkillEvidence(result.Turns[0].SkillEvidence)
	if len(uses) != 1 || uses[0].SkillName != "review" || uses[0].Mode != usage.ModeImplicit || uses[0].State != usage.StateInferred {
		t.Fatalf("exec payload skill = %#v", uses)
	}
	if len(uses[0].Methods) != 1 || uses[0].Methods[0] != usage.MethodImplicitAccess {
		t.Fatalf("exec payload skill methods = %#v", uses[0].Methods)
	}
}

func TestLoadDetectsSkillAccessInJavaScriptExecPayload(t *testing.T) {
	raw := map[string]any{
		"ctx_event_id":        "tool-call",
		"ctx_session_id":      "ctx-session",
		"event_type":          "tool_call",
		"occurred_at":         "2026-01-01T00:00:00Z",
		"provider":            "codex",
		"provider_session_id": "provider-session",
		"role":                "assistant",
		"content":             map[string]any{"complete": true},
		"text": jsonLine(t, map[string]any{
			"type":    "custom_tool_call",
			"name":    "exec",
			"call_id": "call-1",
			"status":  "completed",
			"input": `const r = await tools.exec_command({
  cmd: "cat /fixture-home/.agents/skills/review/SKILL.md",
  workdir: "/tmp"
});
text(r.output);`,
		}),
	}
	lines := []string{
		jsonLine(t, map[string]any{"record_type": "event_range_event", "event": raw}),
		completionLine(t, "generation-1", "", true),
	}
	runner := func([]string) (CommandResult, error) {
		return CommandResult{Stdout: []byte(strings.Join(lines, "\n") + "\n")}, nil
	}

	result, err := Load("/tmp/ctx", IngestOptions{Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	uses := usage.MergeSkillEvidence(result.Turns[0].SkillEvidence)
	if len(uses) != 1 || uses[0].SkillName != "review" || uses[0].Mode != usage.ModeImplicit {
		t.Fatalf("javascript exec payload skill = %#v", uses)
	}
}

func TestLoadDetectsSkillAccessInMultipleJavaScriptExecCalls(t *testing.T) {
	raw := map[string]any{
		"ctx_event_id":        "tool-call",
		"ctx_session_id":      "ctx-session",
		"event_type":          "tool_call",
		"occurred_at":         "2026-01-01T00:00:00Z",
		"provider":            "codex",
		"provider_session_id": "provider-session",
		"role":                "assistant",
		"content":             map[string]any{"complete": true},
		"text": jsonLine(t, map[string]any{
			"type":    "custom_tool_call",
			"name":    "exec",
			"call_id": "call-1",
			"status":  "completed",
			"input": `const results = await Promise.all([
  tools.exec_command({cmd: "echo ready"}),
  tools.exec_command({cmd: "cat /fixture-home/.agents/skills/review/SKILL.md"}),
]);
text(results);`,
		}),
	}
	lines := []string{
		jsonLine(t, map[string]any{"record_type": "event_range_event", "event": raw}),
		completionLine(t, "generation-1", "", true),
	}
	runner := func([]string) (CommandResult, error) {
		return CommandResult{Stdout: []byte(strings.Join(lines, "\n") + "\n")}, nil
	}

	result, err := Load("/tmp/ctx", IngestOptions{Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	uses := usage.MergeSkillEvidence(result.Turns[0].SkillEvidence)
	if len(uses) != 1 || uses[0].SkillName != "review" || uses[0].Mode != usage.ModeImplicit {
		t.Fatalf("multiple javascript exec payload skill = %#v", uses)
	}
}

func TestLoadDetectsSkillAccessFromJavaScriptExecCommandArray(t *testing.T) {
	raw := map[string]any{
		"ctx_event_id":        "tool-call",
		"ctx_session_id":      "ctx-session",
		"event_type":          "tool_call",
		"occurred_at":         "2026-01-01T00:00:00Z",
		"provider":            "codex",
		"provider_session_id": "provider-session",
		"role":                "assistant",
		"content":             map[string]any{"complete": true},
		"text": jsonLine(t, map[string]any{
			"type":    "custom_tool_call",
			"name":    "exec",
			"call_id": "call-1",
			"status":  "completed",
			"input": `const cmds = [
  "echo ready",
  "cat /fixture-home/.agents/skills/review/SKILL.md",
];
const results = await Promise.all(cmds.map(cmd => tools.exec_command({cmd})));`,
		}),
	}
	lines := []string{
		jsonLine(t, map[string]any{"record_type": "event_range_event", "event": raw}),
		completionLine(t, "generation-1", "", true),
	}
	runner := func([]string) (CommandResult, error) {
		return CommandResult{Stdout: []byte(strings.Join(lines, "\n") + "\n")}, nil
	}

	result, err := Load("/tmp/ctx", IngestOptions{Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	uses := usage.MergeSkillEvidence(result.Turns[0].SkillEvidence)
	if len(uses) != 1 || uses[0].SkillName != "review" || uses[0].Mode != usage.ModeImplicit {
		t.Fatalf("javascript exec command array skill = %#v", uses)
	}
}

func TestLoadDoesNotTreatUnrelatedJavaScriptStringsAsExecCommands(t *testing.T) {
	raw := map[string]any{
		"ctx_event_id":        "tool-call",
		"ctx_session_id":      "ctx-session",
		"event_type":          "tool_call",
		"occurred_at":         "2026-01-01T00:00:00Z",
		"provider":            "codex",
		"provider_session_id": "provider-session",
		"role":                "assistant",
		"content":             map[string]any{"complete": true},
		"text": jsonLine(t, map[string]any{
			"type":    "custom_tool_call",
			"name":    "exec",
			"call_id": "call-1",
			"status":  "completed",
			"input": `const cmds = ["echo ready"];
const result = await tools.exec_command({cmd});
text("cat /fixture-home/.agents/skills/review/SKILL.md is only an example");`,
		}),
	}
	lines := []string{
		jsonLine(t, map[string]any{"record_type": "event_range_event", "event": raw}),
		completionLine(t, "generation-1", "", true),
	}
	runner := func([]string) (CommandResult, error) {
		return CommandResult{Stdout: []byte(strings.Join(lines, "\n") + "\n")}, nil
	}

	result, err := Load("/tmp/ctx", IngestOptions{Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	if uses := usage.MergeSkillEvidence(result.Turns[0].SkillEvidence); len(uses) != 0 {
		t.Fatalf("unrelated javascript string was treated as command: %#v", uses)
	}
}

func TestLoadDetectsRuntimeSkillItemsInCtxPayload(t *testing.T) {
	event := func(id, eventType, role string, textValue any) string {
		raw := map[string]any{
			"ctx_event_id":        id,
			"ctx_session_id":      "ctx-session",
			"event_type":          eventType,
			"occurred_at":         "2026-01-01T00:00:00Z",
			"provider":            "codex",
			"provider_session_id": "provider-session",
			"role":                role,
			"content":             map[string]any{"complete": true},
			"text":                jsonLine(t, textValue),
		}
		return jsonLine(t, map[string]any{"record_type": "event_range_event", "event": raw})
	}
	lines := []string{
		event("message", "message", "user", map[string]any{
			"type": "UserMessage",
			"content": []any{
				map[string]any{"type": "text", "text": "inspect the skill"},
				map[string]any{"type": "skill", "name": "review", "path": "/fixture-home/.agents/skills/review/SKILL.md"},
			},
		}),
		completionLine(t, "generation-1", "", true),
	}
	runner := func([]string) (CommandResult, error) {
		return CommandResult{Stdout: []byte(strings.Join(lines, "\n") + "\n")}, nil
	}

	result, err := Load("/tmp/ctx", IngestOptions{Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	uses := usage.MergeSkillEvidence(result.Turns[0].SkillEvidence)
	if len(uses) != 1 || uses[0].SkillName != "review" || uses[0].State != usage.StateConfirmed {
		t.Fatalf("runtime skill item = %#v", uses)
	}
	if len(uses[0].Methods) != 1 || uses[0].Methods[0] != usage.MethodRuntimeSkillItem {
		t.Fatalf("runtime skill item methods = %#v", uses[0].Methods)
	}
}

func TestLoadDetectsSkillBlockInStructuredCtxMessage(t *testing.T) {
	event := func(id, eventType, role string, textValue any) string {
		raw := map[string]any{
			"ctx_event_id":        id,
			"ctx_session_id":      "ctx-session",
			"event_type":          eventType,
			"occurred_at":         "2026-01-01T00:00:00Z",
			"provider":            "codex",
			"provider_session_id": "provider-session",
			"role":                role,
			"content":             map[string]any{"complete": true},
			"text":                jsonLine(t, textValue),
		}
		return jsonLine(t, map[string]any{"record_type": "event_range_event", "event": raw})
	}
	lines := []string{
		event("message", "message", "user", map[string]any{
			"type": "UserMessage",
			"content": []any{
				map[string]any{"type": "input_text", "text": `<skill name="review">instructions</skill>`},
			},
		}),
		completionLine(t, "generation-1", "", true),
	}
	runner := func([]string) (CommandResult, error) {
		return CommandResult{Stdout: []byte(strings.Join(lines, "\n") + "\n")}, nil
	}

	result, err := Load("/tmp/ctx", IngestOptions{Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	uses := usage.MergeSkillEvidence(result.Turns[0].SkillEvidence)
	if len(uses) != 1 || uses[0].SkillName != "review" || uses[0].State != usage.StateConfirmed {
		t.Fatalf("structured ctx skill block = %#v", uses)
	}
}

func TestLoadPreservesSelectedSkillInstructionEvidence(t *testing.T) {
	event := func(id, eventType, role string, textValue any) string {
		raw := map[string]any{
			"ctx_event_id":        id,
			"ctx_session_id":      "ctx-session",
			"event_type":          eventType,
			"occurred_at":         "2026-01-01T00:00:00Z",
			"provider":            "codex",
			"provider_session_id": "provider-session",
			"role":                role,
			"content":             map[string]any{"complete": true},
			"text":                jsonLine(t, textValue),
		}
		return jsonLine(t, map[string]any{"record_type": "event_range_event", "event": raw})
	}
	lines := []string{
		event("message", "message", "user", map[string]any{
			"internal_chat_message_metadata_passthrough": map[string]any{
				"content_item_kinds": []any{"skills.selected_skill_instructions"},
			},
			"content": []any{
				map[string]any{"type": "input_text", "text": `<skill name="review">instructions</skill>`},
			},
		}),
		completionLine(t, "generation-1", "", true),
	}
	runner := func([]string) (CommandResult, error) {
		return CommandResult{Stdout: []byte(strings.Join(lines, "\n") + "\n")}, nil
	}

	result, err := Load("/tmp/ctx", IngestOptions{Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	uses := usage.MergeSkillEvidence(result.Turns[0].SkillEvidence)
	if len(uses) != 1 || uses[0].SkillName != "review" || len(uses[0].Methods) != 1 || uses[0].Methods[0] != usage.MethodSelectedSkillInstructions {
		t.Fatalf("selected skill evidence = %#v", uses)
	}
}

func TestLoadDetectsSkillInCtxStructuredContentProjection(t *testing.T) {
	raw := map[string]any{
		"ctx_event_id":        "message",
		"ctx_session_id":      "ctx-session",
		"event_type":          "message",
		"occurred_at":         "2026-01-01T00:00:00Z",
		"provider":            "codex",
		"provider_session_id": "provider-session",
		"role":                "user",
		"text":                "ordinary user prompt",
		"content":             map[string]any{"complete": true},
		"structured_content": map[string]any{
			"internal_chat_message_metadata_passthrough": map[string]any{
				"content_item_kinds": []any{"skills.selected_skill_instructions"},
			},
			"content": []any{
				map[string]any{"type": "input_text", "text": `<skill name="review">instructions</skill>`},
			},
		},
	}
	lines := []string{
		jsonLine(t, map[string]any{"record_type": "event_range_event", "event": raw}),
		completionLine(t, "generation-1", "", true),
	}
	runner := func([]string) (CommandResult, error) {
		return CommandResult{Stdout: []byte(strings.Join(lines, "\n") + "\n")}, nil
	}

	result, err := Load("/tmp/ctx", IngestOptions{Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	uses := usage.MergeSkillEvidence(result.Turns[0].SkillEvidence)
	if len(uses) != 1 || uses[0].SkillName != "review" || len(uses[0].Methods) != 1 || uses[0].Methods[0] != usage.MethodSelectedSkillInstructions {
		t.Fatalf("structured content skill evidence = %#v", uses)
	}
}

func TestLoadDoesNotTreatSelectedSkillPhraseAsMetadata(t *testing.T) {
	raw := map[string]any{
		"ctx_event_id":        "message",
		"ctx_session_id":      "ctx-session",
		"event_type":          "message",
		"occurred_at":         "2026-01-01T00:00:00Z",
		"provider":            "codex",
		"provider_session_id": "provider-session",
		"role":                "user",
		"text":                "ordinary user prompt",
		"content":             map[string]any{"complete": true},
		"structured_content": map[string]any{
			"content": []any{
				map[string]any{"type": "input_text", "text": `<skill name="review">instructions</skill>`},
				map[string]any{"type": "input_text", "text": "skills.selected_skill_instructions"},
			},
		},
	}
	lines := []string{
		jsonLine(t, map[string]any{"record_type": "event_range_event", "event": raw}),
		completionLine(t, "generation-1", "", true),
	}
	runner := func([]string) (CommandResult, error) {
		return CommandResult{Stdout: []byte(strings.Join(lines, "\n") + "\n")}, nil
	}

	result, err := Load("/tmp/ctx", IngestOptions{Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	uses := usage.MergeSkillEvidence(result.Turns[0].SkillEvidence)
	if len(uses) != 1 || uses[0].SkillName != "review" || len(uses[0].Methods) != 1 || uses[0].Methods[0] != usage.MethodSkillInjection {
		t.Fatalf("phrase metadata false positive = %#v", uses)
	}
}

func TestLoadNormalizesKnownRuntimeActivityKinds(t *testing.T) {
	lines := []string{
		eventLine(t, "mcp", "opencode", "session", "activity", "assistant", "2026-01-01T00:00:00Z", "", map[string]any{
			"type":   "McpToolCall",
			"id":     "mcp-1",
			"server": "browser",
			"tool":   "search",
		}),
		eventLine(t, "file", "opencode", "session", "activity", "assistant", "2026-01-01T00:00:01Z", "", map[string]any{
			"type": "FileChange",
			"id":   "file-1",
		}),
		completionLine(t, "generation-1", "", true),
	}
	runner := func([]string) (CommandResult, error) {
		return CommandResult{Stdout: []byte(strings.Join(lines, "\n") + "\n")}, nil
	}
	result, err := Load("/tmp/ctx", IngestOptions{Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || len(result.Turns[0].RuntimeTools) != 2 {
		t.Fatalf("runtime activity = %#v", result.Turns)
	}
	if got := usage.EffectiveTools(result.Turns[0]); len(got) != 2 || got[0].CanonicalName != "mcp:browser/search" || got[1].CanonicalName != "file_change" {
		t.Fatalf("effective runtime activity = %#v", got)
	}
}

func stringMethods(methods []usage.SkillEvidenceMethod) []string {
	result := make([]string, 0, len(methods))
	for _, method := range methods {
		result = append(result, string(method))
	}
	return result
}

func containsPair(args []string, first, second string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == first && args[i+1] == second {
			return true
		}
	}
	return false
}
