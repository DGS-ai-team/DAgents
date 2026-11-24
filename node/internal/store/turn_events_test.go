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

func terminalBoundaryFixture() TerminalBoundary {
	now := time.Now().UTC()
	step := turn.NewTurnEventEnvelope("terminal-session", turn.EventStepCompleted, now)
	step.AgentID, step.TurnID, step.StepID, step.CommandID = "terminal-agent", "turn-1", "step-1", "step-done"
	step.Payload = json.RawMessage(`{"command_type":"complete_step"}`)
	completed := turn.NewTurnEventEnvelope("terminal-session", turn.EventTurnCompleted, now.Add(time.Millisecond))
	completed.AgentID, completed.TurnID, completed.StepID, completed.CommandID = "terminal-agent", "turn-1", "step-1", "turn-done"
	completed.Payload = json.RawMessage(`{"command_type":"complete_turn"}`)
	message := llm.Message{Role: "assistant", Content: "final", RecordID: "step-1/assistant/content"}
	return TerminalBoundary{
		Record:           Record{AgentID: "terminal-session", NodeID: "terminal-agent", RuntimeState: RuntimeState{AckSeq: 4}},
		Delta:            &ConversationDelta{ConversationID: "terminal-session", NodeID: "terminal-agent", ExpectedRevision: 0, HistoryRevision: 2, Upserts: []llm.Message{message}, Checkpoint: &CheckpointCommit{Messages: []llm.Message{message}, Lineage: "live_append"}},
		Events:           []turn.TurnEventEnvelope{step, completed},
		SnapshotMessages: []llm.Message{{Role: "user", Content: "question"}, {Role: "assistant", Content: "final", RecordID: "step-1/assistant/content"}},
		SnapshotStatus:   "readable",
	}
}

func TestCommitTerminalBoundaryRollsBackEveryWriteOnFailure(t *testing.T) {
	for _, table := range []string{"agent_runtimes", "conversation_records", "context_checkpoints", "turn_events", "completed_turn_snapshots"} {
		t.Run(table, func(t *testing.T) {
			st, err := Open(filepath.Join(t.TempDir(), "terminal.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			trigger := `CREATE TRIGGER fail_terminal BEFORE INSERT ON ` + table + ` BEGIN SELECT RAISE(ABORT,'injected terminal failure'); END`
			if _, err := st.db.Exec(trigger); err != nil {
				t.Fatal(err)
			}
			_, _, err = st.CommitTerminalBoundary(context.Background(), terminalBoundaryFixture())
			if err == nil {
				t.Fatal("expected injected failure")
			}
			for _, check := range []struct {
				table string
				want  int
			}{{"agent_runtimes", 0}, {"conversation_records", 0}, {"context_checkpoints", 0}, {"turn_events", 0}, {"completed_turn_snapshots", 0}} {
				var n int
				if e := st.db.QueryRow(`SELECT count(*) FROM ` + check.table).Scan(&n); e != nil || n != check.want {
					t.Fatalf("%s rows=%d err=%v", check.table, n, e)
				}
			}
		})
	}
}

func TestCommitTerminalBoundaryIdempotentAndRestartReadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "terminal.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	boundary := terminalBoundaryFixture()
	first, rev1, err := st.CommitTerminalBoundary(context.Background(), boundary)
	if err != nil {
		t.Fatal(err)
	}
	second, rev2, err := st.CommitTerminalBoundary(context.Background(), boundary)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || len(second) != 2 || first[1].ID != second[1].ID ||
		rev1.ConversationRevision != rev2.ConversationRevision || rev1.CheckpointRevision != rev2.CheckpointRevision ||
		len(rev1.ChangedItems) != 1 || len(rev2.ChangedItems) != 0 {
		t.Fatalf("first=%+v second=%+v rev1=%+v rev2=%+v", first, second, rev1, rev2)
	}
	if rev1.ConversationRevision != 1 || rev1.CheckpointRevision != 1 {
		t.Fatalf("unexpected revisions: %+v", rev1)
	}
	shortened := terminalBoundaryFixture()
	shortened.Events = shortened.Events[1:]
	if _, _, err := st.CommitTerminalBoundary(context.Background(), shortened); err == nil {
		t.Fatal("expected retry with omitted step fact to be rejected")
	}
	reordered := terminalBoundaryFixture()
	reordered.Events[0], reordered.Events[1] = reordered.Events[1], reordered.Events[0]
	if _, _, err := st.CommitTerminalBoundary(context.Background(), reordered); err == nil {
		t.Fatal("expected retry with reordered event batch to be rejected")
	}
	changed := terminalBoundaryFixture()
	changed.Events[0].Payload = json.RawMessage(`{"command_type":"changed_step"}`)
	if _, _, err := st.CommitTerminalBoundary(context.Background(), changed); err == nil {
		t.Fatal("expected retry with changed event contents to be rejected")
	}
	changedRuntime := terminalBoundaryFixture()
	changedRuntime.Record.RuntimeState.NotifySeq++
	if _, _, err := st.CommitTerminalBoundary(context.Background(), changedRuntime); err == nil {
		t.Fatal("expected retry with changed runtime state to be rejected")
	}
	changedDelta := terminalBoundaryFixture()
	changedDelta.Delta.Upserts[0].Content = "different final"
	if _, _, err := st.CommitTerminalBoundary(context.Background(), changedDelta); err == nil {
		t.Fatal("expected retry with changed conversation delta to be rejected")
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	events, err := reopened.ListTurnEventsForTurn(context.Background(), "terminal-session", "turn-1")
	if err != nil || len(events) != 2 || events[1].EventType != turn.EventTurnCompleted {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	snapshots, err := reopened.ListCompletedTurnSnapshots(context.Background(), "terminal-agent", 0, 10)
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("snapshots=%+v err=%v", snapshots, err)
	}
	if snapshots[0].Status != "readable" || snapshots[0].ConversationRevision != rev1.ConversationRevision || snapshots[0].CheckpointRevision != rev1.CheckpointRevision {
		t.Fatalf("snapshot not revision-bound: %+v", snapshots[0])
	}
	var rowRevision int
	if err := reopened.db.QueryRow(`SELECT revision FROM conversation_records WHERE conversation_id=? AND item_id=?`, "terminal-session", "step-1/assistant/content").Scan(&rowRevision); err != nil || rowRevision != 1 {
		t.Fatalf("record revision=%d err=%v", rowRevision, err)
	}
}

func TestCommitTerminalBoundaryDoesNotSnapshotNonCompletedTerminalStates(t *testing.T) {
	terminalTypes := []turn.EventType{
		turn.EventTurnFailed,
		turn.EventTurnCancelled,
		turn.EventTurnInterrupted,
		turn.EventTurnBudgetExhausted,
	}
	for _, eventType := range terminalTypes {
		t.Run(string(eventType), func(t *testing.T) {
			st, err := Open(filepath.Join(t.TempDir(), "terminal.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			boundary := terminalBoundaryFixture()
			last := &boundary.Events[len(boundary.Events)-1]
			last.EventType = eventType
			last.CommandID = "turn-" + string(eventType)
			if _, _, err := st.CommitTerminalBoundary(context.Background(), boundary); err != nil {
				t.Fatal(err)
			}
			snapshots, err := st.ListCompletedTurnSnapshots(context.Background(), "terminal-agent", 0, 10)
			if err != nil || len(snapshots) != 0 {
				t.Fatalf("non-completed terminal state created completed snapshot: snapshots=%+v err=%v", snapshots, err)
			}
		})
	}
}

func TestCommitTerminalBoundaryClosesRoundRevisionForEveryTerminalState(t *testing.T) {
	tests := []struct {
		name       string
		terminal   turn.EventType
		step       turn.EventType
		snapshot   string
		wantStatus string
		wantFinish string
	}{
		{name: "completed", terminal: turn.EventTurnCompleted, step: turn.EventStepCompleted, snapshot: "readable", wantStatus: "completed", wantFinish: string(turn.EventTurnCompleted)},
		{name: "no_work", terminal: turn.EventTurnCompleted, step: turn.EventStepCompleted, snapshot: "no_work", wantStatus: "completed", wantFinish: "no_work"},
		{name: "failed", terminal: turn.EventTurnFailed, step: turn.EventStepFailed, snapshot: "unreadable", wantStatus: "failed", wantFinish: string(turn.EventTurnFailed)},
		{name: "cancelled", terminal: turn.EventTurnCancelled, step: turn.EventStepCancelled, snapshot: "unreadable", wantStatus: "cancelled", wantFinish: string(turn.EventTurnCancelled)},
		{name: "interrupted", terminal: turn.EventTurnInterrupted, step: turn.EventStepInterrupted, snapshot: "unreadable", wantStatus: "interrupted", wantFinish: string(turn.EventTurnInterrupted)},
		{name: "budget_exhausted", terminal: turn.EventTurnBudgetExhausted, step: turn.EventStepCompleted, snapshot: "unreadable", wantStatus: "budget_exhausted", wantFinish: string(turn.EventTurnBudgetExhausted)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st, err := Open(filepath.Join(t.TempDir(), "round-terminal.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()

			now := time.Now().UTC()
			start := turn.NewTurnEventEnvelope("terminal-session", turn.EventTurnStarted, now)
			start.AgentID, start.TurnID, start.Source, start.CommandID = "terminal-agent", "turn-1", string(turn.TurnSourceHuman), "start-turn"
			start.Payload = json.RawMessage(`{"command_type":"start_turn","generation":1}`)
			stepStart := turn.NewTurnEventEnvelope("terminal-session", turn.EventStepStarted, now.Add(time.Millisecond))
			stepStart.AgentID, stepStart.TurnID, stepStart.StepID, stepStart.Source, stepStart.CommandID = "terminal-agent", "turn-1", "step-1", string(turn.TurnSourceHuman), "start-step"
			stepStart.Payload = json.RawMessage(`{"command_type":"start_step","generation":1}`)
			round := RoundRevision{ConversationID: "terminal-session", RoundID: "round-1", RevisionID: "revision-1", TurnID: "turn-1", Source: string(turn.TurnSourceHuman), Generation: 1, Status: "active"}
			if _, err := st.AppendTurnStartBoundary(context.Background(), []turn.TurnEventEnvelope{start, stepStart}, round, RuntimeState{}); err != nil {
				t.Fatalf("create active round: %v", err)
			}

			boundary := terminalBoundaryFixture()
			boundary.Events[0].EventType = tt.step
			boundary.Events[0].CommandID = "terminal-step"
			boundary.Events[0].Source = string(turn.TurnSourceHuman)
			boundary.Events[0].Payload = json.RawMessage(`{"command_type":"terminal_step"}`)
			boundary.Events[1].EventType = tt.terminal
			boundary.Events[1].CommandID = "terminal-turn"
			boundary.Events[1].Source = string(turn.TurnSourceHuman)
			boundary.Events[1].Payload = json.RawMessage(`{"command_type":"terminal_turn"}`)
			boundary.RoundRevision = &round
			boundary.SnapshotStatus = tt.snapshot
			if _, _, err := st.CommitTerminalBoundary(context.Background(), boundary); err != nil {
				t.Fatalf("commit terminal boundary: %v", err)
			}
			var status, finishReason string
			var recordRevision, checkpointRevision uint64
			if err := st.db.QueryRow(`SELECT status,finish_reason,record_revision,checkpoint_revision FROM conversation_round_revisions WHERE conversation_id=? AND round_id=? AND revision_id=?`, round.ConversationID, round.RoundID, round.RevisionID).Scan(&status, &finishReason, &recordRevision, &checkpointRevision); err != nil {
				t.Fatal(err)
			}
			if status != tt.wantStatus || finishReason != tt.wantFinish {
				t.Fatalf("round revision status=(%q,%q), want (%q,%q)", status, finishReason, tt.wantStatus, tt.wantFinish)
			}
			if recordRevision != 1 || checkpointRevision != 1 {
				t.Fatalf("round revision points at conversation/checkpoint=(%d,%d), want (1,1)", recordRevision, checkpointRevision)
			}
		})
	}
}

func TestCommitTerminalBoundaryRejectsMismatchedRoundRevisionAtomically(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "round-terminal-mismatch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now().UTC()
	start := turn.NewTurnEventEnvelope("terminal-session", turn.EventTurnStarted, now)
	start.AgentID, start.TurnID, start.Source, start.CommandID = "terminal-agent", "turn-1", string(turn.TurnSourceHuman), "start-turn"
	start.Payload = json.RawMessage(`{"command_type":"start_turn","generation":1}`)
	stepStart := turn.NewTurnEventEnvelope("terminal-session", turn.EventStepStarted, now.Add(time.Millisecond))
	stepStart.AgentID, stepStart.TurnID, stepStart.StepID, stepStart.Source, stepStart.CommandID = "terminal-agent", "turn-1", "step-1", string(turn.TurnSourceHuman), "start-step"
	stepStart.Payload = json.RawMessage(`{"command_type":"start_step","generation":1}`)
	round := RoundRevision{ConversationID: "terminal-session", RoundID: "round-1", RevisionID: "revision-1", TurnID: "turn-1", Source: string(turn.TurnSourceHuman), Generation: 1, Status: "active"}
	if _, err := st.AppendTurnStartBoundary(context.Background(), []turn.TurnEventEnvelope{start, stepStart}, round, RuntimeState{}); err != nil {
		t.Fatal(err)
	}
	boundary := terminalBoundaryFixture()
	boundary.Events[0].Source = string(turn.TurnSourceHuman)
	boundary.Events[1].Source = string(turn.TurnSourceHuman)
	wrongRound := round
	wrongRound.Generation++
	boundary.RoundRevision = &wrongRound
	if _, _, err := st.CommitTerminalBoundary(context.Background(), boundary); err == nil {
		t.Fatal("expected mismatched round generation to be rejected")
	}
	var status string
	if err := st.db.QueryRow(`SELECT status FROM conversation_round_revisions WHERE conversation_id=? AND round_id=? AND revision_id=?`, round.ConversationID, round.RoundID, round.RevisionID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "active" {
		t.Fatalf("failed terminal transaction changed round status to %q", status)
	}
	var terminalFacts, records, checkpoints int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM turn_events WHERE session_id=? AND event_type IN ('turn.completed','turn.failed','turn.cancelled','turn.interrupted','turn.budget_exhausted')`, round.ConversationID).Scan(&terminalFacts); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM conversation_records WHERE conversation_id=?`, round.ConversationID).Scan(&records); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM context_checkpoints WHERE conversation_id=?`, round.ConversationID).Scan(&checkpoints); err != nil {
		t.Fatal(err)
	}
	// The successful start boundary now persists the explicit empty revision-0
	// pre-round checkpoint. A rejected terminal boundary must not add any more.
	if terminalFacts != 0 || records != 0 || checkpoints != 1 {
		t.Fatalf("mismatched terminal boundary changed its start base: facts=%d records=%d checkpoints=%d", terminalFacts, records, checkpoints)
	}
}

func TestTurnEventLogVersionTracksDeletionAndRebuild(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "event-version.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	append := func(commandID string) {
		t.Helper()
		event := turn.NewTurnEventEnvelope("session-1", turn.EventTurnStarted, time.Now().UTC())
		event.TurnID = commandID
		event.CommandID = commandID
		if _, err := st.AppendTurnEvent(context.Background(), event); err != nil {
			t.Fatalf("append event: %v", err)
		}
	}
	append("turn-1")
	append("turn-2")
	oldID, oldCount, err := st.TurnEventLogVersion(context.Background(), "session-1")
	if err != nil || oldID == 0 || oldCount != 2 {
		t.Fatalf("initial version = (%d,%d), err=%v", oldID, oldCount, err)
	}
	if _, err := st.db.Exec(`DELETE FROM turn_events WHERE session_id = ?`, "session-1"); err != nil {
		t.Fatal(err)
	}
	deletedID, deletedCount, err := st.TurnEventLogVersion(context.Background(), "session-1")
	if err != nil || deletedID != 0 || deletedCount != 0 {
		t.Fatalf("deleted version = (%d,%d), err=%v", deletedID, deletedCount, err)
	}
	append("turn-rebuilt")
	rebuiltID, rebuiltCount, err := st.TurnEventLogVersion(context.Background(), "session-1")
	if err != nil || rebuiltID == 0 || rebuiltCount != 1 || rebuiltID == oldID {
		t.Fatalf("rebuilt version = (%d,%d), prior=%d, err=%v", rebuiltID, rebuiltCount, oldID, err)
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
