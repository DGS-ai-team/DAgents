package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func TestCompletedSnapshotAtomicFailureAndCommandIdempotency(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.db.Exec(`CREATE TRIGGER fail_snapshot BEFORE INSERT ON completed_turn_snapshots BEGIN SELECT RAISE(ABORT, 'snapshot failure'); END`); err != nil {
		t.Fatal(err)
	}
	e := turn.NewTurnEventEnvelope("session-1", turn.EventTurnCompleted, time.Now().UTC())
	e.AgentID = "agent-1"
	e.TurnID = "turn-1"
	e.CommandID = "complete-1"
	if _, err := st.AppendTurnEventWithSnapshot(context.Background(), e, []llm.Message{{Role: "user", Content: "x"}}); err == nil {
		t.Fatal("expected snapshot failure")
	}
	var n int
	if err := st.db.QueryRow(`SELECT count(*) FROM turn_events`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("events=%d err=%v", n, err)
	}
	if _, err := st.db.Exec(`DROP TRIGGER fail_snapshot`); err != nil {
		t.Fatal(err)
	}
	stored, err := st.AppendTurnEventWithSnapshot(context.Background(), e, []llm.Message{{Role: "user", Content: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := st.AppendTurnEventWithSnapshot(context.Background(), e, []llm.Message{{Role: "user", Content: "different"}})
	if err != nil {
		t.Fatal(err)
	}
	if repeated.ID != stored.ID {
		t.Fatalf("idempotent IDs %d/%d", repeated.ID, stored.ID)
	}
	items, err := st.ListCompletedTurnSnapshots(context.Background(), "agent-1", 0, 10)
	if err != nil || len(items) != 1 || items[0].Messages[0].Content != "x" {
		t.Fatalf("snapshots=%+v err=%v", items, err)
	}
}

func TestCompletedTurnSnapshotSurvivesReopenWithToolCallTranscript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	start := turn.NewTurnEventEnvelope("session-real", turn.EventTurnStarted, now)
	start.AgentID = "agent-real"
	start.TurnID = "turn-real"
	start.CommandID = "start-real"
	start.Payload = []byte(`{"input_message":{"role":"user","content":"check status"}}`)
	if _, err := st.AppendTurnEvent(context.Background(), start); err != nil {
		t.Fatal(err)
	}
	assistant := llm.Message{Role: "assistant", Content: "checking", ToolCalls: []llm.ToolCall{{ID: "call-1", Type: "function", Function: llm.ToolCallFunction{Name: "status", Arguments: "{}"}}}}
	ap, _ := json.Marshal(map[string]any{"assistant_message": assistant})
	a := turn.NewTurnEventEnvelope("session-real", turn.EventAssistantMessageRecorded, now.Add(time.Millisecond))
	a.AgentID = "agent-real"
	a.TurnID = "turn-real"
	a.CommandID = "assistant-real"
	a.Payload = ap
	if _, err := st.AppendTurnEvent(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	toolPayload := []byte(`{"result_content":"ok","tool_name":"status"}`)
	tr := turn.NewTurnEventEnvelope("session-real", turn.EventToolResultRecorded, now.Add(2*time.Millisecond))
	tr.AgentID = "agent-real"
	tr.TurnID = "turn-real"
	tr.ToolCallID = "call-1"
	tr.CommandID = "tool-real"
	tr.Payload = toolPayload
	if _, err := st.AppendTurnEvent(context.Background(), tr); err != nil {
		t.Fatal(err)
	}
	tr2 := tr
	tr2.CommandID = "tool-real-duplicate"
	if _, err := st.AppendTurnEvent(context.Background(), tr2); err != nil {
		t.Fatal(err)
	}
	completed := turn.NewTurnEventEnvelope("session-real", turn.EventTurnCompleted, now.Add(3*time.Millisecond))
	completed.AgentID = "agent-real"
	completed.TurnID = "turn-real"
	completed.CommandID = "complete-real"
	messages := []llm.Message{{Role: "user", Content: "check status"}, assistant, {Role: "tool", ToolCallID: "call-1", Name: "status", Content: "ok"}, {Role: "assistant", Content: "done"}}
	if _, err := st.AppendTurnEventWithSnapshot(context.Background(), completed, messages); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := st.RebuildTurnMessages(context.Background(), "session-real", "turn-real")
	if err != nil || len(rebuilt) != 3 || rebuilt[0].Role != "user" || rebuilt[1].ToolCalls[0].ID != "call-1" || rebuilt[2].ToolCallID != "call-1" {
		t.Fatalf("rebuilt=%+v err=%v", rebuilt, err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	items, err := reopened.ListCompletedTurnSnapshots(context.Background(), "agent-real", 0, 10)
	if err != nil || len(items) != 1 || len(items[0].Messages) != 4 || items[0].Messages[3].Content != "done" {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}

func TestTurnEventStoreSequencesAndIdempotency(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	now := time.Now().UTC()
	first := turn.NewTurnEventEnvelope("session-1", turn.EventTurnStarted, now)
	first.AgentID = "agent-1"
	first.TurnID = "turn-1"
	first.CommandID = "cmd-1"
	first.Payload = []byte(`{"status":"running"}`)
	stored, err := st.AppendTurnEvent(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ID == 0 || stored.SessionSeq != 1 || stored.TurnSeq != 1 {
		t.Fatalf("first event = %#v", stored)
	}

	second := turn.NewTurnEventEnvelope("session-1", turn.EventStepStarted, now.Add(time.Second))
	second.AgentID = "agent-1"
	second.TurnID = "turn-1"
	second.StepID = "step-1"
	second.CommandID = "cmd-2"
	storedSecond, err := st.AppendTurnEvent(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	if storedSecond.SessionSeq != 2 || storedSecond.TurnSeq != 2 {
		t.Fatalf("second event = %#v", storedSecond)
	}

	replayed, err := st.AppendTurnEvent(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ID != storedSecond.ID || replayed.SessionSeq != storedSecond.SessionSeq {
		t.Fatalf("idempotent replay = %#v want %#v", replayed, storedSecond)
	}
	completedReplay := turn.NewTurnEventEnvelope("session-2", turn.EventTurnCompleted, now.Add(10*time.Second))
	completedReplay.AgentID = "agent-1"
	completedReplay.TurnID = "turn-1"
	completedReplay.CommandID = "complete-replay"
	completedStored, err := st.AppendTurnEventWithSnapshot(context.Background(), completedReplay, []llm.Message{{Role: "user", Content: "original"}})
	if err != nil {
		t.Fatal(err)
	}
	completedReplay.CreatedAt = now.Add(20 * time.Second)
	completedReplay.SessionSeq = 99
	completedReplay.TurnSeq = 99
	completedReplayed, err := st.AppendTurnEventWithSnapshot(context.Background(), completedReplay, []llm.Message{{Role: "user", Content: "changed"}})
	if err != nil {
		t.Fatal(err)
	}
	if completedReplayed.ID != completedStored.ID || completedReplayed.SessionSeq != completedStored.SessionSeq || !completedReplayed.CreatedAt.Equal(completedStored.CreatedAt) {
		t.Fatalf("completed replay = %#v want %#v", completedReplayed, completedStored)
	}
	completedItems, err := st.ListCompletedTurnSnapshots(context.Background(), "agent-1", 0, 10)
	if err != nil || len(completedItems) != 1 || completedItems[0].Messages[0].Content != "original" {
		t.Fatalf("replayed snapshot = %+v err=%v", completedItems, err)
	}
	conflict := second
	conflict.EventType = turn.EventStepCompleted
	if _, err := st.AppendTurnEvent(context.Background(), conflict); err == nil {
		t.Fatal("expected command id conflict to be rejected")
	}

	third := turn.NewTurnEventEnvelope("session-1", turn.EventTurnStarted, now.Add(2*time.Second))
	third.AgentID = "agent-1"
	third.TurnID = "turn-2"
	third.CommandID = "cmd-3"
	storedThird, err := st.AppendTurnEvent(context.Background(), third)
	if err != nil {
		t.Fatal(err)
	}
	if storedThird.SessionSeq != 3 || storedThird.TurnSeq != 1 {
		t.Fatalf("new turn event = %#v", storedThird)
	}

	events, err := st.ListTurnEvents(context.Background(), "session-1", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].CommandID != "cmd-2" || events[1].TurnID != "turn-2" {
		t.Fatalf("listed events = %#v", events)
	}
}

func TestAppendTurnEventCommandConflictIncludesStepToolAndPayload(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	base := turn.NewTurnEventEnvelope("session-1", turn.EventToolResultRecorded, time.Now().UTC())
	base.AgentID = "agent-1"
	base.TurnID = "turn-1"
	base.StepID = "step-1"
	base.ToolCallID = "call-1"
	base.ToolExecutionID = "exec-1"
	base.CommandID = "tool-command"
	base.Payload = json.RawMessage(`{"result_content":"ok"}`)
	if _, err := st.AppendTurnEvent(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	conflicts := []turn.TurnEventEnvelope{base, base, base}
	conflicts[0].StepID = "step-2"
	conflicts[1].ToolCallID = "call-2"
	conflicts[2].Payload = json.RawMessage(`{"result_content":"different"}`)
	for i, conflict := range conflicts {
		if _, err := st.AppendTurnEvent(context.Background(), conflict); err == nil {
			t.Fatalf("conflict %d was accepted", i)
		}
	}
}

func TestAppendTurnEventWithSnapshotRejectsSequenceGap(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	event := turn.NewTurnEventEnvelope("session-1", turn.EventTurnCompleted, time.Now().UTC())
	event.AgentID = "agent-1"
	event.TurnID = "turn-1"
	event.CommandID = "complete-gap"
	event.SessionSeq = 2
	event.TurnSeq = 2
	if _, err := st.AppendTurnEventWithSnapshot(context.Background(), event, nil); err == nil {
		t.Fatal("expected sequence validation error")
	}
}

func TestTurnEventStoreRejectsNonContiguousExplicitSequence(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	event := turn.NewTurnEventEnvelope("session-1", turn.EventTurnStarted, time.Now().UTC())
	event.TurnID = "turn-1"
	event.CommandID = "cmd-1"
	event.SessionSeq = 4
	event.TurnSeq = 4
	if _, err := st.AppendTurnEvent(context.Background(), event); err == nil {
		t.Fatal("expected sequence validation error")
	}
}
