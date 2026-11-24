package session

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func terminalMailboxRuntime(t *testing.T, failTerminalCommit bool) (*runtime, *store.SQLiteStore, uint64, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mailbox-terminal.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	r := newLifecycleTestRuntime()
	r.store = st
	r.inputBox = NewInputBox()
	seq, err := r.inputBox.Append(InputKindUser, queue.Envelope{Content: "durable input"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.inputBox.Pop(); !ok {
		t.Fatal("failed to claim input")
	}
	if err := r.persist(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.lifecycleBeginInputTurn(turn.TurnSourceHuman); err != nil {
		t.Fatal(err)
	}
	if failTerminalCommit {
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`CREATE TRIGGER fail_terminal_snapshot BEFORE INSERT ON completed_turn_snapshots BEGIN SELECT RAISE(ABORT,'injected terminal failure'); END`); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
	user := llm.Message{Role: "user", Content: "durable input", RecordID: llm.InputRecordID(r.session.ID, seq)}
	history := []llm.Message{user, {Role: "assistant", Content: "durable answer", RecordID: "terminal-step/assistant/content"}}
	if err := r.lifecycleAfterModelStep(turn.StepOutcome{}, history, 0); err != nil && !failTerminalCommit {
		t.Fatalf("commit terminal turn: %v", err)
	} else if err == nil && failTerminalCommit {
		t.Fatal("expected injected terminal commit failure")
	}
	return r, st, seq, path
}

func persistedInFlight(t *testing.T, st *store.SQLiteStore) (InputRecord, bool) {
	t.Helper()
	record, err := st.Load(context.Background(), "session-1")
	if err != nil || record == nil {
		t.Fatalf("load persisted runtime: record=%+v err=%v", record, err)
	}
	box := NewInputBox()
	if err := restoreMailboxForTest(t, box, st, "session-1", record.RuntimeState.InputBoxState); err != nil {
		t.Fatalf("restore persisted input box: %v", err)
	}
	return box.InFlight()
}

func TestFailedTerminalCommitDoesNotLeakCompletedMailboxMarkerIntoOrdinarySave(t *testing.T) {
	r, st, seq, path := terminalMailboxRuntime(t, true)
	defer st.Close()
	if got, ok := r.inputBox.InFlight(); !ok || got.Seq != seq || got.Completed {
		t.Fatalf("failed terminal commit mutated live in-flight input: %+v present=%v", got, ok)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TRIGGER fail_terminal_snapshot`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	// A later ordinary metadata/content save must preserve the uncompleted
	// ownership marker even though the terminal transaction failed.
	if err := r.persist(context.Background()); err != nil {
		t.Fatalf("ordinary save after terminal failure: %v", err)
	}
	record, ok := persistedInFlight(t, st)
	if !ok || record.Seq != seq || record.Completed {
		t.Fatalf("ordinary save persisted false completion: %+v present=%v", record, ok)
	}

	restarted := newLifecycleTestRuntime()
	restarted.store = st
	restarted.inputBox = NewInputBox()
	restarted.restoreInputBoxState(mustRuntimeState(t, st).InputBoxState)
	restarted.reconcileRestoredInputBox()
	if got, ok := restarted.inputBox.Peek(); !ok || got.Seq != seq || got.Env.Content != "durable input" {
		t.Fatalf("restart lost uncompleted input: %+v present=%v", got, ok)
	}
}

func TestSuccessfulTerminalCommitRemovesClaimedQueueRowBeforeRestart(t *testing.T) {
	r, st, seq, _ := terminalMailboxRuntime(t, false)
	defer st.Close()
	if got, ok := r.inputBox.InFlight(); !ok || got.Seq != seq || !got.Completed {
		t.Fatalf("successful terminal commit did not align live mailbox marker: %+v present=%v", got, ok)
	}
	if _, err := r.inputBox.Append(InputKindUser, queue.Envelope{Content: "next input"}); err != nil {
		t.Fatal(err)
	}
	if err := r.persist(context.Background()); err != nil {
		t.Fatalf("ordinary save after appending next input: %v", err)
	}
	rows, err := st.LoadInputQueue(context.Background(), r.session.ID)
	if err != nil || len(rows) != 1 || rows[0].InputSeq == seq {
		t.Fatalf("completed claim remained in the durable queue: rows=%+v err=%v", rows, err)
	}

	restarted := newLifecycleTestRuntime()
	restarted.store = st
	restarted.inputBox = NewInputBox()
	restarted.restoreInputBoxState(mustRuntimeState(t, st).InputBoxState)
	restarted.reconcileRestoredInputBox()
	if restarted.inputBox.HasInFlight() || restarted.inputBox.Len() != 1 {
		t.Fatalf("restart did not restore only the remaining queue: in_flight=%v len=%d", restarted.inputBox.HasInFlight(), restarted.inputBox.Len())
	}
	if next, ok := restarted.inputBox.Peek(); !ok || next.Env.Content != "next input" {
		t.Fatalf("restart lost the next queued input: %+v present=%v", next, ok)
	}
}

func mustRuntimeState(t *testing.T, st *store.SQLiteStore) store.RuntimeState {
	t.Helper()
	record, err := st.Load(context.Background(), "session-1")
	if err != nil || record == nil {
		t.Fatalf("load runtime state: record=%+v err=%v", record, err)
	}
	return record.RuntimeState
}
