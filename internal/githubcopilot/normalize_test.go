package githubcopilot

import (
	"encoding/json"
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
		testEvent("session-a", "a-4", "assistant.message", time.Unix(4, 0), map[string]any{"content": "assistant output", "model": "copilot-model"}),
		testEvent("session-a", "a-5", "tool.execution_complete", time.Unix(5, 0), map[string]any{"toolCallId": "tool-a", "toolName": "shell", "status": "success"}),
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
