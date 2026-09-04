package turn

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestTurnCoordinatorDispatchesOneTurnAcrossMultipleSteps(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	coordinator := NewTurnCoordinator("session-1", "agent-1")

	snapshot, err := coordinator.Dispatch(TurnCommand{
		Type:       CommandStartTurn,
		SessionID:  "session-1",
		TurnID:     "turn-1",
		Generation: 1,
		Source:     TurnSourceHuman,
		At:         now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TurnStatus != TurnStatusRunning {
		t.Fatalf("turn status = %s", snapshot.TurnStatus)
	}

	snapshot, err = coordinator.Dispatch(TurnCommand{
		Type:       CommandStartStep,
		SessionID:  "session-1",
		TurnID:     "turn-1",
		StepID:     "step-1",
		Generation: 1,
		At:         now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.StepStatus != StepStatusRequesting {
		t.Fatalf("step status = %s", snapshot.StepStatus)
	}

	commands := []TurnCommand{
		{Type: CommandModelRequestStarted, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1, At: now},
		{Type: CommandAssistantReceived, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1, HasTools: true, At: now},
		{Type: CommandInteractionRequested, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1, At: now, Reason: "approval required"},
		{Type: CommandInteractionResolved, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1, At: now},
		{Type: CommandToolBatchSettled, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1, At: now},
		{Type: CommandCompleteStep, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1, At: now},
	}
	for _, command := range commands {
		if _, err := coordinator.Dispatch(command); err != nil {
			t.Fatalf("dispatch %s: %v", command.Type, err)
		}
	}

	snapshot, err = coordinator.Dispatch(TurnCommand{
		Type:       CommandStartStep,
		SessionID:  "session-1",
		TurnID:     "turn-1",
		StepID:     "step-2",
		Generation: 1,
		At:         now.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.StepIndex != 2 || snapshot.StepID != "step-2" {
		t.Fatalf("second step projection = %#v", snapshot)
	}

	if _, err := coordinator.Dispatch(TurnCommand{
		Type:       CommandAssistantReceived,
		SessionID:  "session-1",
		TurnID:     "turn-1",
		StepID:     "step-2",
		Generation: 1,
		At:         now.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Dispatch(TurnCommand{
		Type:       CommandCompleteStep,
		SessionID:  "session-1",
		TurnID:     "turn-1",
		StepID:     "step-2",
		Generation: 1,
		At:         now.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err = coordinator.Dispatch(TurnCommand{
		Type:       CommandCompleteTurn,
		SessionID:  "session-1",
		TurnID:     "turn-1",
		Generation: 1,
		At:         now.Add(2 * time.Second),
		Reason:     "completed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TurnStatus != TurnStatusCompleted || snapshot.HasActiveTurn {
		t.Fatalf("completed projection = %#v", snapshot)
	}
}

func TestTurnCoordinatorRejectsStaleCommands(t *testing.T) {
	coordinator := NewTurnCoordinator("session-1", "agent-1")
	_, err := coordinator.Dispatch(TurnCommand{
		Type:       CommandStartTurn,
		SessionID:  "session-1",
		TurnID:     "turn-1",
		Generation: 4,
		Source:     TurnSourceHuman,
		At:         time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = coordinator.Dispatch(TurnCommand{
		Type:       CommandStartStep,
		SessionID:  "session-1",
		TurnID:     "turn-1",
		StepID:     "step-1",
		Generation: 3,
		At:         time.Now(),
	})
	if err == nil || !strings.Contains(err.Error(), "generation mismatch") {
		t.Fatalf("expected stale generation error, got %v", err)
	}
}

func TestTurnCoordinatorRecordsExternalFactWithoutCreatingToolExecution(t *testing.T) {
	now := time.Now().UTC()
	c := NewTurnCoordinator("session-1", "agent-1")
	for _, command := range []TurnCommand{
		{Type: CommandStartTurn, SessionID: "session-1", TurnID: "turn-1", Generation: 1, Source: TurnSourceHuman, At: now},
		{Type: CommandStartStep, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1, At: now},
	} {
		if _, err := c.Dispatch(command); err != nil {
			t.Fatal(err)
		}
	}
	state, err := c.Dispatch(TurnCommand{
		Type:             CommandExternalFactRecorded,
		CommandID:        "fact-command-1",
		SessionID:        "session-1",
		TurnID:           "turn-1",
		StepID:           "step-1",
		Generation:       1,
		ExternalFactID:   "async:job-1",
		ExternalFactKind: "async",
		ToolCallID:       "call-original",
		ToolName:         "bash_run",
		ResultContent:    "done",
		At:               now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if state.ExternalFacts != 1 || c.HasToolCall("call-original") {
		t.Fatalf("external fact projection = %#v; it must not create a tool call", state)
	}
	state, err = c.Dispatch(TurnCommand{
		Type:           CommandExternalFactRecorded,
		CommandID:      "fact-command-2",
		SessionID:      "session-1",
		TurnID:         "turn-1",
		StepID:         "step-1",
		Generation:     1,
		ExternalFactID: "async:job-1",
		At:             now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if state.ExternalFacts != 1 {
		t.Fatalf("duplicate external fact changed count: %#v", state)
	}
}

func TestTurnCoordinatorReplaysExternalFact(t *testing.T) {
	now := time.Now().UTC()
	payload := func(value string) []byte { return []byte(value) }
	events := []TurnEventEnvelope{
		{SessionID: "session-1", TurnID: "turn-1", SessionSeq: 1, TurnSeq: 1, EventType: EventTurnStarted, EventVersion: 1, Source: string(TurnSourceHuman), CommandID: "start", CreatedAt: now, Payload: payload(`{"generation":1}`)},
		{SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", SessionSeq: 2, TurnSeq: 2, EventType: EventStepStarted, EventVersion: 1, CommandID: "step", CreatedAt: now, Payload: payload(`{"generation":1}`)},
		{SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", ToolCallID: "call-original", SessionSeq: 3, TurnSeq: 3, EventType: EventExternalFactRecorded, EventVersion: 1, CommandID: "fact", CreatedAt: now, Payload: payload(`{"generation":1,"external_fact_id":"async:job-1","external_fact_kind":"async"}`)},
	}
	c := NewTurnCoordinator("session-1", "agent-1")
	if err := c.Restore(events); err != nil {
		t.Fatal(err)
	}
	state := c.Snapshot()
	if state.ExternalFacts != 1 || c.HasToolCall("call-original") {
		t.Fatalf("replayed external fact projection = %#v", state)
	}
}

func TestTurnCoordinatorAllowsNewTurnAfterTerminalTurn(t *testing.T) {
	coordinator := NewTurnCoordinator("session-1", "agent-1")
	start := func(turnID string, generation uint64) {
		t.Helper()
		if _, err := coordinator.Dispatch(TurnCommand{
			Type:       CommandStartTurn,
			SessionID:  "session-1",
			TurnID:     turnID,
			Generation: generation,
			Source:     TurnSourceHuman,
			At:         time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}

	start("turn-1", 1)
	if _, err := coordinator.Dispatch(TurnCommand{
		Type:       CommandInterruptTurn,
		SessionID:  "session-1",
		TurnID:     "turn-1",
		Generation: 1,
		At:         time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	start("turn-2", 2)
	snapshot := coordinator.Snapshot()
	if snapshot.TurnID != "turn-2" || snapshot.TurnStatus != TurnStatusRunning {
		t.Fatalf("new turn projection = %#v", snapshot)
	}
}

func TestTurnCoordinatorRejectsStaleStepCommands(t *testing.T) {
	coordinator := NewTurnCoordinator("session-1", "agent-1")
	for _, command := range []TurnCommand{
		{Type: CommandStartTurn, SessionID: "session-1", TurnID: "turn-1", Generation: 1, Source: TurnSourceHuman, At: time.Now()},
		{Type: CommandStartStep, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1, At: time.Now()},
	} {
		if _, err := coordinator.Dispatch(command); err != nil {
			t.Fatal(err)
		}
	}
	_, err := coordinator.Dispatch(TurnCommand{
		Type:       CommandAssistantReceived,
		SessionID:  "session-1",
		TurnID:     "turn-1",
		StepID:     "stale-step",
		Generation: 1,
		At:         time.Now(),
	})
	if err == nil || !strings.Contains(err.Error(), "step mismatch") {
		t.Fatalf("expected stale step error, got %v", err)
	}
}

func TestTurnCoordinatorRollsBackRejectedCommand(t *testing.T) {
	now := time.Now().UTC()
	c := NewTurnCoordinator("session-1", "agent-1")
	for _, command := range []TurnCommand{
		{Type: CommandStartTurn, SessionID: "session-1", TurnID: "turn-1", Generation: 1, Source: TurnSourceHuman, At: now},
		{Type: CommandStartStep, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1, At: now},
		{Type: CommandAssistantReceived, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1, HasTools: true, At: now},
		{Type: CommandInteractionRequested, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1, InteractionID: "interaction-1", InteractionRevision: 1, At: now},
	} {
		if _, err := c.Dispatch(command); err != nil {
			t.Fatalf("dispatch %s: %v", command.Type, err)
		}
	}
	before := c.Snapshot()
	_, err := c.Dispatch(TurnCommand{
		Type: CommandInteractionResolved, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1,
		InteractionID: "wrong-interaction", InteractionRevision: 99, At: now,
	})
	if err == nil {
		t.Fatal("expected interaction mismatch")
	}
	after := c.Snapshot()
	if after.TurnStatus != before.TurnStatus || after.StepStatus != before.StepStatus || after.InteractionID != before.InteractionID {
		t.Fatalf("rejected command mutated projection: before=%+v after=%+v", before, after)
	}
}

func TestTurnCoordinatorRejectsOutOfOrderToolFacts(t *testing.T) {
	now := time.Now().UTC()
	c := NewTurnCoordinator("session-1", "agent-1")
	for _, command := range []TurnCommand{
		{Type: CommandStartTurn, SessionID: "session-1", TurnID: "turn-1", Generation: 1, Source: TurnSourceHuman, At: now},
		{Type: CommandStartStep, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1, At: now},
		{Type: CommandAssistantReceived, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1, HasTools: true, At: now},
		{Type: CommandToolCallRecorded, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1, ToolCallID: "call-1", ToolName: "read_file", At: now},
	} {
		if _, err := c.Dispatch(command); err != nil {
			t.Fatalf("dispatch %s: %v", command.Type, err)
		}
	}
	_, err := c.Dispatch(TurnCommand{
		Type: CommandToolExecutionCompleted, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1,
		ToolExecutionID: "call-1-execution", ExecutionStatus: ToolExecutionStatusSucceeded, At: now,
	})
	if err == nil {
		t.Fatal("expected completion before start to be rejected")
	}
	if status, ok := c.ToolExecutionStatusForCall("call-1"); !ok || status != ToolExecutionStatusProposed {
		t.Fatalf("rejected completion changed execution status: %s/%v", status, ok)
	}
	if _, err := c.Dispatch(TurnCommand{
		Type: CommandToolExecutionStarted, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1,
		ToolExecutionID: "call-1-execution", At: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Dispatch(TurnCommand{
		Type: CommandToolExecutionCompleted, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1,
		ToolExecutionID: "call-1-execution", ExecutionStatus: ToolExecutionStatusSucceeded, At: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Dispatch(TurnCommand{
		Type: CommandToolResultRecorded, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1,
		ToolCallID: "call-1", ToolExecutionID: "call-1-execution", At: now,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTurnCoordinatorRestoreRollsBackCorruptReplay(t *testing.T) {
	now := time.Now().UTC()
	c := NewTurnCoordinator("session-1", "agent-1")
	if _, err := c.Dispatch(TurnCommand{
		Type: CommandStartTurn, SessionID: "session-1", TurnID: "turn-live", Generation: 3,
		Source: TurnSourceHuman, At: now,
	}); err != nil {
		t.Fatal(err)
	}
	before := c.Snapshot()
	err := c.Restore([]TurnEventEnvelope{
		{SessionID: "session-1", TurnID: "turn-replay", SessionSeq: 1, TurnSeq: 1, EventType: EventTurnStarted, CreatedAt: now},
		{SessionID: "wrong-session", TurnID: "turn-replay", SessionSeq: 2, TurnSeq: 2, EventType: EventStepStarted, CreatedAt: now},
	})
	if err == nil {
		t.Fatal("expected corrupt replay to fail")
	}
	after := c.Snapshot()
	if after.TurnID != before.TurnID || after.TurnStatus != before.TurnStatus || after.Generation != before.Generation {
		t.Fatalf("corrupt replay mutated projection: before=%+v after=%+v", before, after)
	}
}

func TestTurnCoordinatorRecordsCompactionBeforeContextRebuild(t *testing.T) {
	now := time.Now()
	coordinator := NewTurnCoordinator("session-1", "agent-1")
	for _, command := range []TurnCommand{
		{Type: CommandStartTurn, SessionID: "session-1", TurnID: "turn-1", Generation: 1, Source: TurnSourceHuman, At: now},
		{Type: CommandStartStep, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1, At: now},
	} {
		if _, err := coordinator.Dispatch(command); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := coordinator.Dispatch(TurnCommand{
		Type:       CommandContextCompacted,
		SessionID:  "session-1",
		TurnID:     "turn-1",
		StepID:     "step-1",
		Generation: 1,
		At:         now,
		Reason:     "context overflow",
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ContextEpoch != 0 {
		t.Fatalf("context epoch = %d, want 0 before model context rebuild", snapshot.ContextEpoch)
	}
}

func TestTurnCoordinatorCompactsWhileWaitingForInteraction(t *testing.T) {
	now := time.Now().UTC()
	coordinator := NewTurnCoordinator("session-1", "agent-1")
	commands := []TurnCommand{
		{Type: CommandStartTurn, SessionID: "session-1", TurnID: "turn-1", Generation: 1, Source: TurnSourceHuman, At: now},
		{Type: CommandStartStep, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1, At: now},
		{Type: CommandAssistantReceived, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1, HasTools: true, At: now},
		{Type: CommandInteractionRequested, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1, InteractionKind: "approval", At: now},
	}
	for _, command := range commands {
		if _, err := coordinator.Dispatch(command); err != nil {
			t.Fatalf("dispatch %s: %v", command.Type, err)
		}
	}
	snapshot, err := coordinator.Dispatch(TurnCommand{
		Type:       CommandContextCompacted,
		SessionID:  "session-1",
		TurnID:     "turn-1",
		StepID:     "step-1",
		Generation: 1,
		Reason:     "manual_compression",
		At:         now.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TurnStatus != TurnStatusWaiting || snapshot.StepStatus != StepStatusWaitingInteraction || snapshot.ContextEpoch != 0 || snapshot.StepEndReason != "" {
		t.Fatalf("waiting interaction compaction projection = %#v", snapshot)
	}
}

func TestTurnCoordinatorReplacesModelContextAtControlledBoundary(t *testing.T) {
	now := time.Now().UTC()
	coordinator := NewTurnCoordinator("session-1", "agent-1")
	for _, command := range []TurnCommand{
		{Type: CommandStartTurn, SessionID: "session-1", TurnID: "turn-1", Generation: 1, Source: TurnSourceHuman, At: now},
		{Type: CommandStartStep, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1, At: now},
		{Type: CommandTurnSnapshotCreated, SessionID: "session-1", TurnID: "turn-1", Generation: 1, ContextSnapshot: NewModelContextSnapshot("prompt-v1", nil, 1, "runtime-v1"), At: now},
	} {
		if _, err := coordinator.Dispatch(command); err != nil {
			t.Fatalf("dispatch %s: %v", command.Type, err)
		}
	}

	snapshot, err := coordinator.Dispatch(TurnCommand{
		Type:            CommandModelContextChanged,
		SessionID:       "session-1",
		TurnID:          "turn-1",
		StepID:          "step-1",
		Generation:      1,
		ContextSnapshot: NewModelContextSnapshot("prompt-v2", nil, 1, "runtime-v2"),
		Reason:          "skills_load",
		At:              now.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ContextEpoch != 1 || snapshot.PromptDigest != Digest("prompt-v2") || snapshot.StepStatus != StepStatusRequesting || snapshot.TurnEndReason != "" || snapshot.StepEndReason != "" {
		t.Fatalf("replaced context projection = %#v", snapshot)
	}
}

func TestTurnCoordinatorRejectsChangedContextInjectionOnRepeatedSnapshot(t *testing.T) {
	now := time.Now().UTC()
	coordinator := NewTurnCoordinator("session-1", "agent-1")
	first := NewModelContextSnapshotWithInjections(
		"stable-prompt", nil,
		[]ContextInjection{{Name: "runtime_context", Source: "test", Content: "cwd=/one"}},
		1, "runtime-v1",
	)
	second := NewModelContextSnapshotWithInjections(
		"stable-prompt", nil,
		[]ContextInjection{{Name: "runtime_context", Source: "test", Content: "cwd=/two"}},
		1, "runtime-v1",
	)
	for _, command := range []TurnCommand{
		{Type: CommandStartTurn, SessionID: "session-1", TurnID: "turn-1", Generation: 1, Source: TurnSourceHuman, At: now},
		{Type: CommandStartStep, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1, At: now},
		{Type: CommandTurnSnapshotCreated, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1, ContextSnapshot: first, At: now},
	} {
		if _, err := coordinator.Dispatch(command); err != nil {
			t.Fatalf("dispatch %s: %v", command.Type, err)
		}
	}
	if _, err := coordinator.Dispatch(TurnCommand{
		Type: CommandTurnSnapshotCreated, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1",
		Generation: 1, ContextSnapshot: second, At: now.Add(time.Second),
	}); err == nil {
		t.Fatal("repeated snapshot with changed context injection should be rejected")
	}
}

func TestTurnCoordinatorReplaysModelContextChange(t *testing.T) {
	now := time.Now().UTC()
	first := NewModelContextSnapshot("prompt-v1", nil, 1, "runtime-v1")
	second := NewModelContextSnapshot("prompt-v2", nil, 2, "runtime-v2")
	second.SkillsCatalogRevision = "catalog-v2"
	second.LoadedSkillsDigest = "loaded-v2"
	second.LoadedSkillsContentDigest = "body-v2"
	second.MemorySnapshotID = "memsnap-1"
	second.MemoryStoreRevision = 17
	second.MemoryDigest = "memory-digest-v1"
	second.MemoryCoreCount = 2
	second.MemoryRecallCount = 3
	second.MemoryEstimatedTokens = 480
	payload := func(generation uint64, snapshot *ModelContextSnapshot) []byte {
		raw, err := json.Marshal(map[string]any{
			"generation":       generation,
			"context_snapshot": snapshot,
		})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	events := []TurnEventEnvelope{
		{SessionID: "session-1", TurnID: "turn-1", SessionSeq: 1, TurnSeq: 1, EventType: EventTurnStarted, EventVersion: 1, Source: string(TurnSourceHuman), CommandID: "start", CreatedAt: now, Payload: payload(1, nil)},
		{SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", SessionSeq: 2, TurnSeq: 2, EventType: EventStepStarted, EventVersion: 1, CommandID: "step", CreatedAt: now, Payload: payload(1, nil)},
		{SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", SessionSeq: 3, TurnSeq: 3, EventType: EventTurnSnapshotCreated, EventVersion: 1, CommandID: "snapshot", CreatedAt: now, Payload: payload(1, first)},
		{SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", SessionSeq: 4, TurnSeq: 4, EventType: EventModelContextChanged, EventVersion: 1, CommandID: "context-change", CreatedAt: now.Add(time.Second), Payload: payload(1, second)},
	}
	coordinator := NewTurnCoordinator("session-1", "agent-1")
	if err := coordinator.Restore(events); err != nil {
		t.Fatal(err)
	}
	state := coordinator.Snapshot()
	if state.ContextEpoch != 1 || state.PromptDigest != second.PromptDigest || state.RuntimeRevision != second.RuntimeRevision || state.ContextSnapshot == nil {
		t.Fatalf("replayed context change projection = %#v", state)
	}
	if state.ContextSnapshot.SkillsCatalogRevision != "catalog-v2" ||
		state.ContextSnapshot.LoadedSkillsDigest != "loaded-v2" ||
		state.ContextSnapshot.LoadedSkillsContentDigest != "body-v2" {
		t.Fatalf("replayed skill snapshot diagnostics = %#v", state.ContextSnapshot)
	}
	if state.ContextSnapshot.MemorySnapshotID != "memsnap-1" ||
		state.ContextSnapshot.MemoryStoreRevision != 17 ||
		state.ContextSnapshot.MemoryDigest != "memory-digest-v1" ||
		state.ContextSnapshot.MemoryCoreCount != 2 ||
		state.ContextSnapshot.MemoryRecallCount != 3 ||
		state.ContextSnapshot.MemoryEstimatedTokens != 480 {
		t.Fatalf("replayed memory snapshot diagnostics = %#v", state.ContextSnapshot)
	}
}

func TestTurnCoordinatorTracksSnapshotAttemptsAndToolExecution(t *testing.T) {
	now := time.Now().UTC()
	c := NewTurnCoordinator("session-1", "agent-1")
	for _, command := range []TurnCommand{
		{Type: CommandStartTurn, SessionID: "session-1", TurnID: "turn-1", Generation: 2, Source: TurnSourceHuman, At: now},
		{Type: CommandStartStep, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 2, At: now},
		{Type: CommandTurnSnapshotCreated, SessionID: "session-1", TurnID: "turn-1", Generation: 2, RuntimeRevision: 9, RuntimeDigest: "runtime", PromptDigest: "prompt", ToolDigest: "tools", At: now},
		{Type: CommandModelRequestStarted, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 2, RequestDigest: "request", At: now},
		{Type: CommandModelResponseCompleted, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 2, At: now},
		{Type: CommandAssistantReceived, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 2, HasTools: true, AssistantMessageID: "assistant-1", At: now},
		{Type: CommandToolCallRecorded, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 2, ToolCallID: "call-1", ToolName: "bash", Arguments: []byte(`{"command":"pwd"}`), At: now},
		{Type: CommandToolExecutionStarted, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 2, ToolExecutionID: "call-1-execution", At: now},
		{Type: CommandToolExecutionCompleted, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 2, ToolExecutionID: "call-1-execution", ExecutionStatus: ToolExecutionStatusSucceeded, At: now},
	} {
		if _, err := c.Dispatch(command); err != nil {
			t.Fatalf("dispatch %s: %v", command.Type, err)
		}
	}
	snapshot := c.Snapshot()
	if snapshot.RuntimeRevision != 9 || snapshot.RuntimeDigest != "runtime" || snapshot.ModelAttempt != 1 || snapshot.AssistantMsgID != "assistant-1" {
		t.Fatalf("snapshot metadata = %#v", snapshot)
	}
	if snapshot.StepStatus != StepStatusExecutingTools || snapshot.ToolBatchID == "" {
		t.Fatalf("tool batch projection = %#v", snapshot)
	}
	if c.ToolExecutionID("call-1") != "call-1-execution" {
		t.Fatal("tool execution was not recorded")
	}
}

func TestTurnCoordinatorRestoresWaitingInteraction(t *testing.T) {
	now := time.Now().UTC()
	coordinator := NewTurnCoordinator("session-1", "agent-1")
	events := []TurnEventEnvelope{
		{SessionID: "session-1", TurnID: "turn-1", SessionSeq: 1, TurnSeq: 1, EventType: EventTurnStarted, Source: string(TurnSourceHuman), Payload: []byte(`{"generation":7}`), CreatedAt: now},
		{SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", SessionSeq: 2, TurnSeq: 2, EventType: EventStepStarted, Payload: []byte(`{"generation":7}`), CreatedAt: now},
		{SessionID: "session-1", TurnID: "turn-1", SessionSeq: 3, TurnSeq: 3, EventType: EventTurnSnapshotCreated, Payload: []byte(`{"generation":7,"runtime_revision":3,"runtime_digest":"runtime","prompt_digest":"prompt","tool_digest":"tools"}`), CreatedAt: now},
		{SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", SessionSeq: 4, TurnSeq: 4, EventType: EventModelRequestStarted, Payload: []byte(`{"generation":7,"request_digest":"request"}`), CreatedAt: now},
		{SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", SessionSeq: 5, TurnSeq: 5, EventType: EventModelRequestCompleted, Payload: []byte(`{"generation":7}`), CreatedAt: now},
		{SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", SessionSeq: 6, TurnSeq: 6, EventType: EventAssistantMessageRecorded, Payload: []byte(`{"generation":7,"has_tools":true}`), CreatedAt: now},
		{SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", SessionSeq: 7, TurnSeq: 7, EventType: EventInteractionRequested, InteractionID: "interaction-1", Payload: []byte(`{"generation":7,"interaction_kind":"approval"}`), CreatedAt: now},
	}
	if err := coordinator.Restore(events); err != nil {
		t.Fatal(err)
	}
	snapshot := coordinator.Snapshot()
	if snapshot.TurnID != "turn-1" || snapshot.StepID != "step-1" || snapshot.TurnStatus != TurnStatusWaiting || snapshot.StepStatus != StepStatusWaitingInteraction {
		t.Fatalf("restored projection = %#v", snapshot)
	}
	if snapshot.Generation != 7 || snapshot.InteractionID == "" {
		t.Fatalf("restored metadata = %#v", snapshot)
	}
}

func TestTurnCoordinatorTracksAndPreflightsTurnBudget(t *testing.T) {
	now := time.Now().UTC()
	c := NewTurnCoordinator("session-1", "agent-1")
	budget := TurnBudget{MaxSteps: 1, MaxToolCalls: 1}
	for _, command := range []TurnCommand{
		{Type: CommandStartTurn, SessionID: "session-1", TurnID: "turn-1", Generation: 1, Source: TurnSourceHuman, Budget: budget, At: now},
		{Type: CommandStartStep, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1, At: now},
		{Type: CommandAssistantReceived, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1, HasTools: true, At: now},
		{Type: CommandToolCallRecorded, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1, ToolCallID: "call-1", At: now},
	} {
		if _, err := c.Dispatch(command); err != nil {
			t.Fatalf("dispatch %s: %v", command.Type, err)
		}
	}
	state := c.Snapshot()
	if state.Usage.Steps != 1 || state.Usage.ToolCalls != 1 {
		t.Fatalf("budget usage = %+v", state.Usage)
	}
	if decision := c.BudgetDecisionFor(CommandStartStep); decision.Allowed {
		t.Fatalf("next step should be rejected: %+v", decision)
	}
	if decision := c.BudgetDecisionFor(CommandToolCallRecorded); !decision.Allowed {
		t.Fatalf("exact tool-call budget should be allowed: %+v", decision)
	}
	if _, err := c.Dispatch(TurnCommand{
		Type: CommandToolCallRecorded, SessionID: "session-1", TurnID: "turn-1", StepID: "step-1", Generation: 1,
		ToolCallID: "call-2", At: now,
	}); err != nil {
		t.Fatal(err)
	}
	if decision := c.BudgetDecisionFor(CommandToolCallRecorded); decision.Allowed || decision.Reason != "max_tool_calls" {
		t.Fatalf("over-limit tool batch should be rejected: %+v", decision)
	}
}

func TestTurnCoordinatorAccumulatesModelUsageAcrossAttempts(t *testing.T) {
	now := time.Now().UTC()
	c := NewTurnCoordinator("session-1", "agent-1")
	commands := []TurnCommand{
		{Type: CommandStartTurn, SessionID: "session-1", TurnID: "turn-usage", Generation: 1, Source: TurnSourceHuman, At: now},
		{Type: CommandStartStep, SessionID: "session-1", TurnID: "turn-usage", StepID: "step-1", Generation: 1, At: now},
		{Type: CommandModelRequestStarted, SessionID: "session-1", TurnID: "turn-usage", StepID: "step-1", Generation: 1, RequestDigest: "attempt-1", At: now},
		{Type: CommandModelUsageRecorded, SessionID: "session-1", TurnID: "turn-usage", StepID: "step-1", Generation: 1, Usage: StepUsage{InputTokens: 10, OutputTokens: 4, TotalTokens: 14, PromptCacheHitTokens: 6, PromptCacheMissTokens: 4, PromptCacheMetricsObserved: true, ReasoningTokens: 2}, At: now},
		{Type: CommandModelRequestRetrying, SessionID: "session-1", TurnID: "turn-usage", StepID: "step-1", Generation: 1, ErrorKind: "timeout", At: now},
		{Type: CommandModelRequestStarted, SessionID: "session-1", TurnID: "turn-usage", StepID: "step-1", Generation: 1, RequestDigest: "attempt-2", At: now},
		{Type: CommandModelUsageRecorded, SessionID: "session-1", TurnID: "turn-usage", StepID: "step-1", Generation: 1, Usage: StepUsage{InputTokens: 20, OutputTokens: 6, TotalTokens: 26, PromptCacheHitTokens: 12, PromptCacheMissTokens: 8, PromptCacheMetricsObserved: true, ReasoningTokens: 3}, At: now},
	}
	for _, command := range commands {
		if _, err := c.Dispatch(command); err != nil {
			t.Fatalf("dispatch %s: %v", command.Type, err)
		}
	}
	state := c.Snapshot()
	if state.Usage.InputTokens != 30 || state.Usage.OutputTokens != 10 || state.Usage.TotalTokens != 40 {
		t.Fatalf("turn usage = %+v", state.Usage)
	}
	if state.Usage.PromptCacheHitTokens != 18 || state.Usage.PromptCacheMissTokens != 12 ||
		!state.Usage.PromptCacheMetricsObserved || state.Usage.ReasoningTokens != 5 {
		t.Fatalf("turn cache/reasoning usage = %+v", state.Usage)
	}
	if state.StepStatus != StepStatusRequesting || state.ModelAttempt != 2 {
		t.Fatalf("attempt projection = %+v", state)
	}
}

func TestTurnCoordinatorRequiresReconciliationForUnknownExecution(t *testing.T) {
	now := time.Now().UTC()
	c := NewTurnCoordinator("session-1", "agent-1")
	commands := []TurnCommand{
		{Type: CommandStartTurn, SessionID: "session-1", TurnID: "turn-recovery", Generation: 1, Source: TurnSourceHuman, At: now},
		{Type: CommandStartStep, SessionID: "session-1", TurnID: "turn-recovery", StepID: "step-1", Generation: 1, At: now},
		{Type: CommandAssistantReceived, SessionID: "session-1", TurnID: "turn-recovery", StepID: "step-1", Generation: 1, HasTools: true, At: now},
		{Type: CommandToolCallRecorded, SessionID: "session-1", TurnID: "turn-recovery", StepID: "step-1", Generation: 1, ToolCallID: "call-1", ToolName: "bash", Arguments: []byte(`{"command":"touch marker"}`), At: now},
		{Type: CommandToolExecutionStarted, SessionID: "session-1", TurnID: "turn-recovery", StepID: "step-1", Generation: 1, ToolExecutionID: "call-1-execution", At: now},
		{Type: CommandToolExecutionFailed, SessionID: "session-1", TurnID: "turn-recovery", StepID: "step-1", Generation: 1, ToolExecutionID: "call-1-execution", ExecutionStatus: ToolExecutionStatusUnknown, ErrorKind: "node_restart_unknown", At: now},
	}
	for _, command := range commands {
		if _, err := c.Dispatch(command); err != nil {
			t.Fatalf("dispatch %s: %v", command.Type, err)
		}
	}
	state := c.Snapshot()
	if !state.RecoveryRequired || state.StepStatus != StepStatusExecutingTools {
		t.Fatalf("unknown execution projection = %+v", state)
	}
	if status, ok := c.ToolExecutionStatusForCall("call-1"); !ok || status != ToolExecutionStatusUnknown {
		t.Fatalf("unknown execution status = %s/%v", status, ok)
	}
	if _, err := c.Dispatch(TurnCommand{
		Type: CommandToolExecutionReconciled, SessionID: "session-1", TurnID: "turn-recovery", StepID: "step-1", Generation: 1,
		ToolExecutionID: "call-1-execution", ExecutionStatus: ToolExecutionStatusSucceeded, ResultContent: "marker created", At: now,
	}); err != nil {
		t.Fatal(err)
	}
	state = c.Snapshot()
	if state.RecoveryRequired {
		t.Fatalf("reconciliation did not clear recovery fence: %+v", state)
	}
	if status, ok := c.ToolExecutionStatusForCall("call-1"); !ok || status != ToolExecutionStatusSucceeded {
		t.Fatalf("reconciled execution status = %s/%v", status, ok)
	}
}

func TestTurnCoordinatorEnforcesUsageBudgets(t *testing.T) {
	now := time.Now().UTC()
	c := NewTurnCoordinator("session-1", "agent-1")
	for _, command := range []TurnCommand{
		{Type: CommandStartTurn, SessionID: "session-1", TurnID: "turn-budget-usage", Generation: 1, Source: TurnSourceHuman, Budget: TurnBudget{MaxOutputTokens: 5, MaxCost: 1}, At: now},
		{Type: CommandStartStep, SessionID: "session-1", TurnID: "turn-budget-usage", StepID: "step-1", Generation: 1, At: now},
		{Type: CommandModelRequestStarted, SessionID: "session-1", TurnID: "turn-budget-usage", StepID: "step-1", Generation: 1, At: now},
		{Type: CommandModelUsageRecorded, SessionID: "session-1", TurnID: "turn-budget-usage", StepID: "step-1", Generation: 1, Usage: StepUsage{OutputTokens: 5, TotalTokens: 5, Cost: 1}, At: now},
	} {
		if _, err := c.Dispatch(command); err != nil {
			t.Fatalf("dispatch %s: %v", command.Type, err)
		}
	}
	if decision := c.BudgetDecisionFor(CommandStartStep); decision.Allowed || decision.Reason != "max_output_tokens" {
		t.Fatalf("output budget decision = %+v", decision)
	}
}

func TestTurnCoordinatorDurableDispatchRollsBackWhenPersistenceFails(t *testing.T) {
	c := NewTurnCoordinator("session-1", "agent-1")
	_, err := c.DispatchDurable(TurnCommand{
		Type: CommandStartTurn, SessionID: "session-1", TurnID: "turn-durable", Generation: 1,
		Source: TurnSourceHuman, At: time.Now().UTC(),
	}, func(TurnCommand, CoordinatorSnapshot) error {
		return fmt.Errorf("event store unavailable")
	})
	if err == nil {
		t.Fatal("expected durable dispatch error")
	}
	if state := c.Snapshot(); state.HasActiveTurn {
		t.Fatalf("failed durable dispatch mutated projection = %+v", state)
	}
	if _, err := c.Dispatch(TurnCommand{
		Type: CommandStartTurn, SessionID: "session-1", TurnID: "turn-durable", Generation: 1,
		Source: TurnSourceHuman, At: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("coordinator did not recover after rollback: %v", err)
	}
}

func TestTurnCoordinatorAllowsOnlyReservedFinalSummaryStep(t *testing.T) {
	now := time.Now().UTC()
	c := NewTurnCoordinator("session-1", "agent-1")
	budget := TurnBudget{MaxSteps: 1, ReserveFinalSummary: true}
	for _, command := range []TurnCommand{
		{Type: CommandStartTurn, SessionID: "session-1", TurnID: "turn-summary", Generation: 1, Source: TurnSourceHuman, Budget: budget, At: now},
		{Type: CommandStartStep, SessionID: "session-1", TurnID: "turn-summary", StepID: "step-1", Generation: 1, At: now},
	} {
		if _, err := c.Dispatch(command); err != nil {
			t.Fatalf("dispatch %s: %v", command.Type, err)
		}
	}
	if decision := c.BudgetDecisionFor(CommandStartStep); decision.Allowed {
		t.Fatalf("ordinary next step should be rejected: %+v", decision)
	}
	for _, command := range []TurnCommand{
		{Type: CommandAssistantReceived, SessionID: "session-1", TurnID: "turn-summary", StepID: "step-1", Generation: 1, At: now},
		{Type: CommandCompleteStep, SessionID: "session-1", TurnID: "turn-summary", StepID: "step-1", Generation: 1, At: now},
	} {
		if _, err := c.Dispatch(command); err != nil {
			t.Fatalf("finish first step: %v", err)
		}
	}
	summary := TurnCommand{Type: CommandStartStep, SessionID: "session-1", TurnID: "turn-summary", StepID: "step-2", Generation: 1, FinalSummary: true, At: now}
	if decision := c.BudgetDecisionForCommand(summary); !decision.Allowed {
		t.Fatalf("reserved summary should be allowed: %+v", decision)
	}
	state, err := c.Dispatch(summary)
	if err != nil {
		t.Fatal(err)
	}
	if !state.FinalSummary || state.Usage.SummarySteps != 1 {
		t.Fatalf("summary projection = %+v", state)
	}
	if decision := c.BudgetDecisionForCommand(TurnCommand{Type: CommandStartStep, FinalSummary: true}); decision.Allowed {
		t.Fatalf("summary reserve must be single-use: %+v", decision)
	}
}
