package githubcopilot

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xkumiyu/catsift/internal/usage"
)

func TestNormalizeSeparatesSessionsAndCountsOnlyUserMessages(t *testing.T) {
	events := []Envelope{
		testEvent("session-a", "a-1", "session.start", time.Unix(1, 0), map[string]any{"version": "1.0"}),
		testEvent("session-a", "a-2", "assistant.turn_start", time.Unix(2, 0), map[string]any{"turnId": "turn-1"}),
		testEvent("session-a", "a-3", "user.message", time.Unix(3, 0), map[string]any{"role": "user", "content": "synthetic prompt"}),
		testEvent("session-a", "a-4-user-role", "user.message", time.Unix(4, 0), map[string]any{"role": "assistant", "content": "injected assistant-role text"}),
		testEvent("session-a", "a-4", "assistant.message", time.Unix(5, 0), map[string]any{"content": "assistant output", "model": "copilot-model"}),
		testEvent("session-a", "a-5", "tool.execution_complete", time.Unix(6, 0), map[string]any{"toolCallId": "tool-a", "toolName": "shell", "status": "success"}),
		testEvent("session-b", "b-1", "assistant.turn_start", time.Unix(6, 0), map[string]any{"turnId": "turn-1"}),
		testEvent("session-b", "b-2", "user.message", time.Unix(7, 0), map[string]any{"role": "user", "content": "another prompt"}),
	}

	result := Normalize(events)
	if len(result.Sessions) != 2 || len(result.Turns) != 2 {
		t.Fatalf("sessions=%#v turns=%#v", result.Sessions, result.Turns)
	}
	if result.Turns[0].SessionID == result.Turns[1].SessionID {
		t.Fatalf("sessions were merged: %#v", result.Turns)
	}
	for _, turn := range result.Turns {
		if turn.Source.Source != usage.SourceCopilot || turn.Source.Agent != "copilot" || turn.UserPrompts != 1 {
			t.Fatalf("normalized turn = %#v", turn)
		}
	}
	if got := result.Turns[0].UserMessages; len(got) != 1 || got[0] != "synthetic prompt" {
		t.Fatalf("first turn classification messages = %#v", got)
	}
	if got := result.Turns[1].UserMessages; len(got) != 1 || got[0] != "another prompt" {
		t.Fatalf("second turn classification messages = %#v", got)
	}
	if result.Turns[0].ModelObservations[0].Model.Name != "copilot-model" {
		t.Fatalf("model observations = %#v", result.Turns[0].ModelObservations)
	}
}

func TestNormalizeRecordsProjectNameFromContext(t *testing.T) {
	events := []Envelope{
		testEvent("session-a", "start-a", "session.start", time.Unix(1, 0), map[string]any{"projectPath": "/workspace/old"}),
		testEvent("session-a", "context-a", "session.context_changed", time.Unix(2, 0), map[string]any{
			"cwd":        "/workspace/project",
			"gitRoot":    "/workspace/project",
			"repository": "owner/project",
		}),
		testEvent("session-b", "context-b", "session.context_changed", time.Unix(3, 0), map[string]any{
			"cwd":     "/workspace/fallback/subdir",
			"gitRoot": "/workspace/fallback",
		}),
	}

	result := Normalize(events)
	if len(result.Sessions) != 2 {
		t.Fatalf("sessions = %#v", result.Sessions)
	}
	byID := make(map[string]usage.Session, len(result.Sessions))
	for _, session := range result.Sessions {
		byID[session.ID] = session
	}
	if session := byID["session-a"]; session.ProjectName != "owner/project" || session.ProjectPath != "/workspace/project" {
		t.Fatalf("repository project metadata = %#v", session)
	}
	if session := byID["session-b"]; session.ProjectName != "fallback" || session.ProjectPath != "/workspace/fallback/subdir" {
		t.Fatalf("path fallback project metadata = %#v", session)
	}
}

func TestNormalizeRecordsSkillInvokedWithoutRetainingPayload(t *testing.T) {
	events := []Envelope{
		testEvent("session-a", "turn-start", "assistant.turn_start", time.Unix(1, 0), map[string]any{"turnId": "turn-1"}),
		testEvent("session-a", "skill-1", "skill.invoked", time.Unix(2, 0), map[string]any{
			"name":    "review",
			"path":    "/private/.agents/skills/review/SKILL.md",
			"content": "synthetic skill instructions must not be retained",
		}),
		testEvent("session-a", "turn-end", "assistant.turn_end", time.Unix(3, 0), map[string]any{"turnId": "turn-1"}),
	}

	result := Normalize(events)
	if len(result.Turns) != 1 || len(result.Turns[0].SkillEvidence) != 1 {
		t.Fatalf("skill evidence = %#v", result.Turns)
	}
	evidence := result.Turns[0].SkillEvidence[0]
	if evidence.SkillName != "review" || evidence.Method != usage.MethodSkillInvoked || evidence.Mode != usage.ModeUnknown || evidence.State != usage.StateConfirmed {
		t.Fatalf("skill evidence = %#v", evidence)
	}
	serialized, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(serialized), "synthetic skill instructions") || strings.Contains(string(serialized), "/private/.agents/skills/review") {
		t.Fatalf("skill payload leaked: %s", serialized)
	}
}

func TestNormalizeMergesRuntimeToolAndSuppressesArguments(t *testing.T) {
	events := []Envelope{
		testEvent("session-a", "e-1", "assistant.turn_start", time.Unix(1, 0), map[string]any{"turnId": "turn-1"}),
		testEvent("session-a", "e-1-model", "model.message", time.Unix(1, 500000000), map[string]any{"turnId": "turn-1", "type": "tool_call", "toolCallId": "call-1", "toolName": "skills.read", "arguments": map[string]any{"path": "/tmp/skills/review/SKILL.md"}}),
		testEvent("session-a", "e-2", "tool.execution_start", time.Unix(2, 0), map[string]any{"turnId": "turn-1", "toolCallId": "call-1", "toolName": "skills.read", "arguments": map[string]any{"path": "/tmp/skills/review/SKILL.md", "secret": "do-not-store"}}),
		testEvent("session-a", "e-3", "tool.execution_complete", time.Unix(3, 0), map[string]any{"turnId": "turn-1", "toolCallId": "call-1", "toolName": "skills.read", "success": false, "error": "synthetic error", "arguments": map[string]any{"secret": "do-not-store"}}),
	}
	result := Normalize(events)
	if len(result.Turns) != 1 || len(result.Turns[0].RuntimeTools) != 1 {
		t.Fatalf("runtime tools = %#v", result.Turns)
	}
	tool := result.Turns[0].RuntimeTools[0]
	if tool.CallID != "call-1" || tool.Status != usage.StatusFailure || tool.Arguments != "" {
		t.Fatalf("runtime tool = %#v", tool)
	}
	if effective := usage.EffectiveTools(result.Turns[0]); len(effective) != 1 || effective[0].Layer != usage.LayerEffective {
		t.Fatalf("effective tools = %#v", effective)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "do-not-store") || strings.Contains(string(data), "synthetic error") {
		t.Fatalf("raw tool payload leaked: %s", data)
	}
	if len(result.Turns[0].SkillEvidence) != 1 || result.Turns[0].SkillEvidence[0].SkillName != "review" {
		t.Fatalf("skill evidence = %#v", result.Turns[0].SkillEvidence)
	}
}

func TestNormalizeDeduplicatesTerminalTokenUsageAndKeepsModelSwitch(t *testing.T) {
	events := []Envelope{
		testEvent("session-a", "e-1", "model.turn_started", time.Unix(1, 0), map[string]any{"turn": 1, "model": "model-a"}),
		testEvent("session-a", "e-2", "model.model_call_success", time.Unix(2, 0), map[string]any{"turn": 1, "callId": "call-1", "model": "model-a", "responseUsage": map[string]any{"prompt_tokens": 10, "completion_tokens": 4, "total_tokens": 14}}),
		testEvent("session-a", "e-3", "model.response", time.Unix(3, 0), map[string]any{"turn": 1, "callId": "call-1", "model": "model-a", "usage": map[string]any{"inputTokens": 10, "outputTokens": 4, "totalTokens": 14}}),
		testEvent("session-a", "e-4", "session.model_change", time.Unix(4, 0), map[string]any{"model": "model-b"}),
		testEvent("session-a", "e-5", "model.turn_started", time.Unix(5, 0), map[string]any{"turnId": "turn-2", "model": "model-b"}),
		testEvent("session-a", "e-6", "session.shutdown", time.Unix(6, 0), map[string]any{"modelMetrics": map[string]any{
			"model-a": map[string]any{"usage": map[string]any{"inputTokens": 100, "outputTokens": 20}},
		}}),
	}
	result := Normalize(events)
	if len(result.Turns) != 2 || len(result.Turns[0].TokenUsageEvents) != 1 {
		t.Fatalf("turns/token events = %#v", result.Turns)
	}
	if result.Turns[0].TokenUsageEvents[0].Usage.TotalTokens != 14 || result.Turns[0].TokenUsageEvents[0].Model.Name != "model-a" {
		t.Fatalf("token usage = %#v", result.Turns[0].TokenUsageEvents)
	}
	if result.Turns[1].ModelObservations[0].Model.Name != "model-b" {
		t.Fatalf("model switch = %#v", result.Turns[1].ModelObservations)
	}
	for _, observation := range result.Turns[0].ModelObservations {
		if observation.Model.Name == "model-b" {
			t.Fatalf("model switch rewrote previous turn = %#v", result.Turns[0].ModelObservations)
		}
	}
}

func TestNormalizeReadsCopilotShutdownTokenMetrics(t *testing.T) {
	result := Normalize([]Envelope{
		testEvent("session-a", "turn-start", "model.turn_started", time.Unix(1, 0), map[string]any{"turnId": "turn-1", "model": "model-a"}),
		testEvent("session-a", "turn-end", "model.turn_ended", time.Unix(2, 0), map[string]any{"turnId": "turn-1"}),
		testEvent("session-a", "shutdown", "session.shutdown", time.Unix(3, 0), map[string]any{
			"modelMetrics": map[string]any{
				"model-a": map[string]any{
					"usage": map[string]any{
						"inputTokens":      100,
						"outputTokens":     20,
						"cacheReadTokens":  40,
						"cacheWriteTokens": 3,
						"reasoningTokens":  5,
					},
				},
			},
		}),
	})

	if len(result.Turns) != 1 || len(result.Turns[0].TokenUsageEvents) != 1 {
		t.Fatalf("shutdown token metrics = %#v", result.Turns)
	}
	event := result.Turns[0].TokenUsageEvents[0]
	if event.Model.Name != "model-a" || event.Usage.InputTokens != 100 || event.Usage.OutputTokens != 20 || event.Usage.TotalTokens != 120 || event.Usage.CachedInputTokens != 40 || event.Usage.CacheWriteInputTokens != 3 || event.Usage.ReasoningOutputTokens != 5 {
		t.Fatalf("shutdown token usage = %#v", event)
	}
}

func TestNormalizeUsesAutoModeResolvedModel(t *testing.T) {
	result := Normalize([]Envelope{
		testEvent("session-a", "auto-mode", "session.auto_mode_resolved", time.Unix(1, 0), map[string]any{"chosenModel": "model-a"}),
		testEvent("session-a", "prompt", "user.message", time.Unix(2, 0), map[string]any{"role": "user", "content": "synthetic prompt"}),
	})

	if len(result.Turns) != 1 || len(result.Turns[0].ModelObservations) != 1 || result.Turns[0].ModelObservations[0].Model.Name != "model-a" {
		t.Fatalf("auto mode model = %#v", result.Turns)
	}
}

func TestNormalizeReadsCompactionTokenUsage(t *testing.T) {
	result := Normalize([]Envelope{
		testEvent("session-a", "compaction", "session.compaction_complete", time.Unix(1, 0), map[string]any{
			"compactionTokensUsed": map[string]any{
				"model":            "model-a",
				"inputTokens":      10,
				"outputTokens":     4,
				"cacheReadTokens":  2,
				"cacheWriteTokens": 1,
			},
		}),
	})

	if len(result.Turns) != 1 || len(result.Turns[0].TokenUsageEvents) != 1 {
		t.Fatalf("compaction token usage = %#v", result.Turns)
	}
	event := result.Turns[0].TokenUsageEvents[0]
	if event.Model.Name != "model-a" || event.Usage.InputTokens != 10 || event.Usage.OutputTokens != 4 || event.Usage.TotalTokens != 14 || event.Usage.CachedInputTokens != 2 || event.Usage.CacheWriteInputTokens != 1 {
		t.Fatalf("compaction token usage event = %#v", event)
	}
}

func TestNormalizeReadsAssistantUsageAliases(t *testing.T) {
	result := Normalize([]Envelope{
		testEvent("session-a", "turn-start", "assistant.turn_start", time.Unix(1, 0), map[string]any{"turnId": "turn-1", "model": "model-a"}),
		testEvent("session-a", "usage", "assistant.usage", time.Unix(2, 0), map[string]any{
			"turnId": "turn-1",
			"callId": "call-1",
			"model":  "model-a",
			"usage": map[string]any{
				"inputTokens":      10,
				"outputTokens":     4,
				"cacheReadTokens":  2,
				"cacheWriteTokens": 1,
				"reasoningTokens":  3,
			},
		}),
	})

	if len(result.Turns) != 1 || len(result.Turns[0].TokenUsageEvents) != 1 {
		t.Fatalf("assistant usage = %#v", result.Turns)
	}
	usageValue := result.Turns[0].TokenUsageEvents[0].Usage
	if usageValue.CachedInputTokens != 2 || usageValue.CacheWriteInputTokens != 1 || usageValue.ReasoningOutputTokens != 3 || usageValue.TotalTokens != 14 {
		t.Fatalf("assistant usage aliases = %#v", usageValue)
	}
}

func TestNormalizeReadsSessionUsageRecord(t *testing.T) {
	result := Normalize([]Envelope{
		testEvent("session-a", "usage-record", "session.usage_record", time.Unix(2, 0), map[string]any{
			"model": "model-a",
			"usage": map[string]any{"inputTokens": 10, "outputTokens": 4},
		}),
	})

	if len(result.Turns) != 1 || len(result.Turns[0].TokenUsageEvents) != 1 {
		t.Fatalf("session usage record = %#v", result.Turns)
	}
	got := result.Turns[0].TokenUsageEvents[0]
	if got.Model.Name != "model-a" || got.Usage.InputTokens != 10 || got.Usage.OutputTokens != 4 || got.Usage.TotalTokens != 14 {
		t.Fatalf("session usage record token usage = %#v", got)
	}

	ignored := Normalize([]Envelope{testEvent("session-b", "metadata-only", "session.usage_record", time.Unix(3, 0), map[string]any{"kind": "checkpoint"})})
	if len(ignored.Turns) != 0 {
		t.Fatalf("unrecognized session usage record created a turn: %#v", ignored.Turns)
	}
}

func TestNormalizeKeepsShutdownTokenMetricsWithoutTurnEvents(t *testing.T) {
	result := Normalize([]Envelope{
		testEvent("session-a", "shutdown", "session.shutdown", time.Unix(3, 0), map[string]any{
			"modelMetrics": map[string]any{
				"model-a": map[string]any{"usage": map[string]any{"inputTokens": 1, "outputTokens": 2}},
			},
		}),
	})

	if len(result.Turns) != 1 || result.Turns[0].ID != "turn-aggregate" || result.Turns[0].TokenUsage == nil || result.Turns[0].TokenUsage.TotalTokens != 3 {
		t.Fatalf("aggregate-only turn = %#v", result.Turns)
	}
}

func testEvent(sessionID, id, eventType string, timestamp time.Time, data map[string]any) Envelope {
	return Envelope{
		ID: id, Type: eventType, Timestamp: timestamp, Data: data, SessionID: sessionID,
		Source: usage.SourceRef{Path: "/synthetic/session-state/" + sessionID + "/events.jsonl", Line: 1, Source: usage.SourceCopilot, Agent: "copilot", Provider: "copilot", ProviderSessionID: sessionID, EventID: id},
	}
}

func TestNormalizeSkillInvocationReferences(t *testing.T) {
	for _, order := range [][]string{{"skill.invoked", "skill.invoked_ref"}, {"skill.invoked_ref", "skill.invoked"}, {"skill.invoked_ref", "skill.invoked_ref"}} {
		t.Run(strings.Join(order, "/"), func(t *testing.T) {
			events := []Envelope{testEvent("session-a", "start", "assistant.turn_start", time.Unix(1, 0), map[string]any{"turnId": "turn-1"})}
			for i, kind := range order {
				events = append(events, testEvent("session-a", kind, kind, time.Unix(int64(i+2), 0), map[string]any{"name": "review", "path": "/private/skills/review/SKILL.md", "content": "synthetic-reference-body", "contentId": "sha256:synthetic-reference-id", "contentLength": 24, "invokedAtTurn": 42}))
			}
			events = append(events,
				testEvent("session-a", "end", "assistant.turn_end", time.Unix(4, 0), map[string]any{"turnId": "turn-1"}),
				testEvent("session-a", "next", "assistant.turn_start", time.Unix(5, 0), map[string]any{"turnId": "turn-2"}),
				testEvent("session-a", "ref-next", "skill.invoked_ref", time.Unix(6, 0), map[string]any{"name": "review", "contentId": "sha256:synthetic-reference-id"}),
			)
			result := Normalize(events)
			if len(result.Turns) != 2 {
				t.Fatalf("turns = %#v", result.Turns)
			}
			for _, turn := range result.Turns {
				if len(turn.SkillEvidence) != 1 {
					t.Fatalf("evidence = %#v", turn.SkillEvidence)
				}
				e := turn.SkillEvidence[0]
				if e.SkillName != "review" || e.Method != usage.MethodSkillInvoked || e.State != usage.StateConfirmed {
					t.Fatalf("evidence = %#v", e)
				}
			}
			serialized, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{"/private/skills", "synthetic-reference-body", "synthetic-reference-id"} {
				if strings.Contains(string(serialized), forbidden) {
					t.Fatalf("raw reference payload leaked: %s", serialized)
				}
			}
		})
	}
}

func TestNormalizeSkillReferenceUsesOnlyDocumentedIdentity(t *testing.T) {
	for _, tt := range []struct {
		name string
		data map[string]any
		want string
	}{
		{"path", map[string]any{"path": "/private/skills/review/SKILL.md", "contentId": "sha256:synthetic"}, "review"},
		{"name", map[string]any{"name": "review"}, "review"},
		{"no identity", map[string]any{"contentId": "sha256:synthetic"}, ""},
		{"unsupported aliases", map[string]any{"skillName": "guessed", "skillPath": "/private/skills/guessed/SKILL.md"}, ""},
		{"invalid identity", map[string]any{"name": 42, "path": false}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			events := []Envelope{}
			if tt.want != "" {
				events = append(events, testEvent("session-a", "start", "assistant.turn_start", time.Unix(0, 0), map[string]any{"turnId": "turn-1"}))
			}
			result := Normalize(append(events, testEvent("session-a", "ref", "skill.invoked_ref", time.Unix(1, 0), tt.data)))
			var evidence []usage.SkillEvidence
			for _, turn := range result.Turns {
				evidence = append(evidence, turn.SkillEvidence...)
			}
			if tt.want == "" {
				if len(result.Turns) != 0 {
					t.Fatalf("unidentified reference invented turns: %#v", result.Turns)
				}
				if len(evidence) != 0 {
					t.Fatalf("invented evidence: %#v", evidence)
				}
				return
			}
			if len(evidence) != 1 || evidence[0].SkillName != tt.want {
				t.Fatalf("evidence = %#v", evidence)
			}
		})
	}
}

func TestNormalizeUnidentifiedSkillsLeaveTurnsUnchanged(t *testing.T) {
	start := testEvent("session-a", "start", "assistant.turn_start", time.Unix(2, 0), map[string]any{"turnId": "turn-1"})
	end := testEvent("session-a", "end", "assistant.turn_end", time.Unix(4, 0), map[string]any{"turnId": "turn-1"})
	for _, state := range []struct {
		name   string
		events []Envelope
	}{
		{"empty", nil}, {"active", []Envelope{start}}, {"finished", []Envelope{start, end}},
	} {
		for _, kind := range []string{"skill.invoked", "skill.invoked_ref"} {
			for _, timestamp := range []int64{1, 6} {
				t.Run(state.name+"/"+kind+"/"+time.Unix(timestamp, 0).Format(time.RFC3339), func(t *testing.T) {
					baseline := Normalize(state.events)
					events := append([]Envelope(nil), state.events...)
					events = append(events, testEvent("session-a", "unidentified", kind, time.Unix(timestamp, 0), map[string]any{"contentId": "sha256:synthetic"}))
					result := Normalize(events)
					if !reflect.DeepEqual(result.Turns, baseline.Turns) {
						t.Fatalf("unidentified skill changed turns: got=%#v want=%#v", result.Turns, baseline.Turns)
					}
				})
			}
		}
	}
}

func TestNormalizeUnattributedSkillReferencesDoNotChangeStatistics(t *testing.T) {
	start := testEvent("session-a", "start", "assistant.turn_start", time.Unix(1, 0), map[string]any{"turnId": "turn-1"})
	end := testEvent("session-a", "end", "assistant.turn_end", time.Unix(3, 0), map[string]any{"turnId": "turn-1"})
	shutdown := testEvent("session-a", "shutdown", "session.shutdown", time.Unix(4, 0), map[string]any{"modelMetrics": map[string]any{"model-a": map[string]any{"usage": map[string]any{"inputTokens": 10, "outputTokens": 5}}}})
	for _, state := range []struct {
		name   string
		events []Envelope
	}{
		{"empty", nil}, {"ended", []Envelope{start, end}}, {"shutdown", []Envelope{start, shutdown}}, {"aggregate-only", []Envelope{shutdown}},
	} {
		t.Run(state.name, func(t *testing.T) {
			baseline := Normalize(state.events)
			events := append([]Envelope(nil), state.events...)
			for i := range 2 {
				ref := testEvent("session-a", "late-ref", "skill.invoked_ref", time.Unix(int64(6+i), 0), map[string]any{"name": "review", "path": "/private/skills/review/SKILL.md", "contentId": "sha256:synthetic-private-ref", "invokedAtTurn": 1, "model": "unattributed-model"})
				ref.Source.Line = i + 10
				events = append(events, ref)
			}
			result := Normalize(events)
			if !reflect.DeepEqual(result.Turns, baseline.Turns) {
				t.Fatalf("unattributed references changed turns: got=%#v want=%#v", result.Turns, baseline.Turns)
			}
			if !reflect.DeepEqual(result.Sessions, baseline.Sessions) {
				t.Fatalf("unattributed references changed session metadata")
			}
			if len(result.Warnings) != 1 {
				t.Fatalf("warnings = %#v", result.Warnings)
			}
			warning := result.Warnings[0]
			if warning.Reason != "copilot_unattributed_skill" || warning.Type != "skill.invoked_ref" || warning.Source != usage.SourceCopilot || warning.Count != 2 || warning.Line != 0 {
				t.Fatalf("warning = %#v", warning)
			}
			data, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{"/private/skills", "synthetic-private-ref", "unattributed-model"} {
				if strings.Contains(string(data), forbidden) {
					t.Fatalf("raw receipt payload leaked: %s", data)
				}
			}
		})
	}
}

func TestNormalizeLateReferenceKeepsCountedInlineAndDiagnostic(t *testing.T) {
	events := []Envelope{
		testEvent("session-a", "start", "assistant.turn_start", time.Unix(1, 0), map[string]any{"turnId": "turn-1"}),
		testEvent("session-a", "inline", "skill.invoked", time.Unix(2, 0), map[string]any{"name": "review"}),
		testEvent("session-a", "end", "assistant.turn_end", time.Unix(3, 0), map[string]any{"turnId": "turn-1"}),
	}
	baseline := Normalize(events)
	result := Normalize(append(events, testEvent("session-a", "late", "skill.invoked_ref", time.Unix(4, 0), map[string]any{"name": "review"})))
	if !reflect.DeepEqual(result.Turns, baseline.Turns) || len(result.Turns) != 1 || len(result.Turns[0].SkillEvidence) != 1 {
		t.Fatalf("counted inline evidence changed: %#v", result.Turns)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Reason != "copilot_unattributed_skill" || result.Warnings[0].Count != 1 {
		t.Fatalf("unattributed receipt diagnostic lost: %#v", result.Warnings)
	}
}

func TestNormalizeInlineSkillPreservesScalarIdentityCompatibility(t *testing.T) {
	result := Normalize([]Envelope{testEvent("session-a", "inline", "skill.invoked", time.Unix(1, 0), map[string]any{"name": 42})})
	if len(result.Turns) != 1 || len(result.Turns[0].SkillEvidence) != 1 || result.Turns[0].SkillEvidence[0].SkillName != "42" {
		t.Fatalf("legacy inline scalar identity changed: %#v", result.Turns)
	}
}
