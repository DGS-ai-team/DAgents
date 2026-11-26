package session

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func TestInputTurnStartBoundaryRollsBackCoordinatorAndKeepsClaimUncompleted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "round-start.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	r := newLifecycleTestRuntime()
	r.store = st
	r.inputBox = NewInputBox()
	seq, err := r.inputBox.Append(InputKindChildAgent, queue.Envelope{Content: "child task"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.inputBox.Pop(); !ok {
		t.Fatal("failed to claim input")
	}
	if err := r.persist(context.Background()); err != nil {
		t.Fatalf("persist claimed input: %v", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER fail_step_start BEFORE INSERT ON turn_events WHEN NEW.event_type='step.started' BEGIN SELECT RAISE(ABORT,'injected step start failure'); END`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if consumed := r.handleInputMessage(context.Background(), InputRecord{Seq: seq, Kind: InputKindChildAgent, Env: queue.Envelope{Content: "child task"}}, turn.TurnSourceChildAgent); consumed {
		t.Fatal("failed atomic start boundary reported input consumed")
	}
	if state := r.turnCoordinator.Snapshot(); state.HasActiveTurn {
		t.Fatalf("coordinator kept uncommitted turn active: %+v", state)
	}
	r.deltaMu.Lock()
	roundID, revisionID := r.activeRoundID, r.activeRevisionID
	r.deltaMu.Unlock()
	if roundID != "" || revisionID != "" {
		t.Fatalf("uncommitted round identity leaked into runtime: round=%q revision=%q", roundID, revisionID)
	}
	if input, ok := r.inputBox.InFlight(); !ok || input.Seq != seq || input.Completed {
		t.Fatalf("failed start acknowledged or lost input: %+v present=%v", input, ok)
	}

	loaded, err := st.Load(context.Background(), r.session.ID)
	if err != nil || loaded == nil {
		t.Fatalf("load durable claim: record=%+v err=%v", loaded, err)
	}
	if loaded.RuntimeState.ActiveRoundID != "" || loaded.RuntimeState.ActiveRevisionID != "" {
		t.Fatalf("failed start transaction leaked round state: %+v", loaded.RuntimeState)
	}
	box := NewInputBox()
	if err := restoreMailboxForTest(t, box, st, r.session.ID, loaded.RuntimeState.InputBoxState); err != nil {
		t.Fatal(err)
	}
	if input, ok := box.InFlight(); !ok || input.Seq != seq || input.Completed {
		t.Fatalf("failed start transaction lost durable claim: %+v present=%v", input, ok)
	}
	var facts, rounds int
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.QueryRow(`SELECT COUNT(*) FROM turn_events WHERE session_id=?`, r.session.ID).Scan(&facts); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM conversation_rounds WHERE conversation_id=?`, r.session.ID).Scan(&rounds); err != nil {
		t.Fatal(err)
	}
	if facts != 0 || rounds != 0 {
		t.Fatalf("partial start boundary escaped rollback: events=%d rounds=%d", facts, rounds)
	}
}

func TestInputTurnStartBoundaryCommitsTurnStepAndMailboxTogether(t *testing.T) {
	path := filepath.Join(t.TempDir(), "round-start-success.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	r := newLifecycleTestRuntime()
	r.store = st
	r.inputBox = NewInputBox()
	seq, err := r.inputBox.Append(InputKindUser, queue.Envelope{Content: "claimed input"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.inputBox.Pop(); !ok {
		t.Fatal("failed to claim input")
	}
	if err := r.persist(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.lifecycleBeginInputTurnIdentity(turn.TurnSourceHuman, "round-logical-1", "revision-1"); err != nil {
		t.Fatalf("start boundary: %v", err)
	}
	state := r.turnCoordinator.Snapshot()
	if !state.HasActiveTurn || state.TurnID == "round-logical-1" {
		t.Fatalf("execution TurnID was conflated with logical round: %+v", state)
	}
	loaded, err := st.Load(context.Background(), r.session.ID)
	if err != nil || loaded == nil {
		t.Fatalf("load committed start snapshot: record=%+v err=%v", loaded, err)
	}
	if loaded.RuntimeState.ActiveRoundID != "round-logical-1" || loaded.RuntimeState.ActiveRevisionID != "revision-1" {
		t.Fatalf("durable active identity mismatch: %+v", loaded.RuntimeState)
	}
	box := NewInputBox()
	if err := restoreMailboxForTest(t, box, st, r.session.ID, loaded.RuntimeState.InputBoxState); err != nil {
		t.Fatal(err)
	}
	if input, ok := box.InFlight(); !ok || input.Seq != seq || input.Completed {
		t.Fatalf("start boundary did not preserve uncompleted claim: %+v present=%v", input, ok)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT event_type,turn_id FROM turn_events WHERE session_id=? ORDER BY session_seq`, r.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var facts []string
	for rows.Next() {
		var kind, turnID string
		if err := rows.Scan(&kind, &turnID); err != nil {
			t.Fatal(err)
		}
		if turnID != state.TurnID {
			t.Fatalf("start fact TurnID=%q, coordinator TurnID=%q", turnID, state.TurnID)
		}
		facts = append(facts, kind)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(facts) != 2 || facts[0] != string(turn.EventTurnStarted) || facts[1] != string(turn.EventStepStarted) {
		t.Fatalf("start facts=%v; want turn.started then step.started", facts)
	}
	var roundID, revisionID, status string
	if err := db.QueryRow(`SELECT r.round_id,rr.revision_id,rr.status FROM conversation_rounds r JOIN conversation_round_revisions rr USING(conversation_id,round_id) WHERE r.conversation_id=?`, r.session.ID).Scan(&roundID, &revisionID, &status); err != nil {
		t.Fatal(err)
	}
	if roundID != "round-logical-1" || revisionID != "revision-1" || status != "active" {
		t.Fatalf("round mapping=(%q,%q,%q)", roundID, revisionID, status)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(path)
	if err != nil {
		t.Fatalf("reopen start boundary store: %v", err)
	}
	defer reopened.Close()
	restored, err := reopened.Load(context.Background(), r.session.ID)
	if err != nil || restored == nil || restored.RuntimeState.ActiveRoundID != roundID || restored.RuntimeState.ActiveRevisionID != revisionID {
		t.Fatalf("restart lost active round mapping: record=%+v err=%v", restored, err)
	}
	loadedRevision, err := reopened.LoadRoundRevisionForTurn(context.Background(), r.session.ID, state.TurnID)
	if err != nil || loadedRevision == nil || loadedRevision.RoundID != roundID || loadedRevision.RevisionID != revisionID || loadedRevision.Status != "active" {
		t.Fatalf("restart lost round revision: revision=%+v err=%v", loadedRevision, err)
	}
	base, err := reopened.LoadConversationCheckpointRevision(context.Background(), r.session.ID, loadedRevision.PreCheckpointRevision)
	if err != nil || base == nil || base.Revision != 0 || len(base.Messages) != 0 || base.Lineage != "initial" {
		t.Fatalf("first round lost explicit empty pre-round checkpoint: checkpoint=%+v err=%v", base, err)
	}
	restoredEvents, err := reopened.ListTurnEventsForTurn(context.Background(), r.session.ID, state.TurnID)
	if err != nil || len(restoredEvents) != 2 || restoredEvents[0].EventType != turn.EventTurnStarted || restoredEvents[1].EventType != turn.EventStepStarted {
		t.Fatalf("restart lifecycle facts=%+v err=%v", restoredEvents, err)
	}
}
