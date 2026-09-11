package session

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

// A legacy trigger restored from the mailbox has no delivery fence. It must
// cancel the recovered lifecycle before any continuation can run.
func TestReconcileRestoredLegacyTriggerCancelsActiveTurn(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Stop()
	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	if err := rt.lifecycleBeginHumanTurn(); err != nil {
		t.Fatal(err)
	}
	state := rt.turnCoordinator.Snapshot()
	raw, err := json.Marshal(map[string]any{
		"seq": 1,
		"in_flight": map[string]any{
			"seq": 1, "kind": InputKindTrigger,
			"env": queue.Envelope{TriggerID: "legacy-trigger", Content: "old", RequestType: queue.RequestTypeMessage},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.inputBox.Restore(raw); err != nil {
		t.Fatal(err)
	}
	rt.reconcileRestoredInputBox()
	if got, ok := rt.inputBox.InFlight(); ok {
		t.Fatalf("legacy in-flight record survived recovery: %+v", got)
	}
	final := rt.turnCoordinator.Snapshot()
	if final.TurnID != state.TurnID || final.HasActiveTurn {
		t.Fatalf("legacy recovery left active turn: before=%+v after=%+v", state, final)
	}
	if len(rt.messages) != 0 {
		// No restored legacy user message may be inserted before cancellation.
		for _, msg := range rt.messages {
			if llm.MessageTextSummary(msg) == "old" {
				t.Fatal("legacy trigger message was restored into transcript")
			}
		}
	}
}

func TestInputBoxDoesNotOverwriteInFlightOwnership(t *testing.T) {
	box := NewInputBox()
	if _, err := box.Append(InputKindTrigger, queue.Envelope{TriggerID: "t", DeliveryID: "d1", Content: "one"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := box.Pop(); !ok {
		t.Fatal("first input was not popped")
	}
	if _, err := box.Append(InputKindTrigger, queue.Envelope{TriggerID: "t", DeliveryID: "d2", Content: "two"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := box.Pop(); ok {
		t.Fatal("second input overwrote in-flight ownership")
	}
}

func TestTriggerPopPersistenceFailureDoesNotExecute(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	mgr := NewManager("agent-1", nil, &llm.MockClient{}, nil, nil, st, TurnOptions{}, nil)
	rt := newRuntime("persist-failure", "agent-1", nil, mgr.llm, nil, nil, st, nil, nil, nil, nil, false, 0, 0, mgr.turn, nil)
	rt.session = Session{ID: "persist-failure", AgentID: "agent-1"}
	if _, err := rt.inputBox.Append(InputKindTrigger, queue.Envelope{TriggerID: "t", DeliveryID: "d", Content: "must not run"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	rt.start(context.Background())
	rt.signalInputBox()
	select {
	case <-rt.done:
	case <-time.After(2 * time.Second):
		t.Fatal("consumer did not fail closed")
	}
	if _, ok := rt.inputBox.InFlight(); ok {
		t.Fatal("failed pre-exec persist left ownership in flight")
	}
	if got, ok := rt.inputBox.Peek(); !ok || got.Env.Content != "must not run" {
		t.Fatalf("trigger input was lost: %+v %v", got, ok)
	}
}

type closeStoreDeliveryTracker struct {
	st      *store.SQLiteStore
	closed  bool
	cleared bool
}

func (t *closeStoreDeliveryTracker) HasPendingDelivery(string) bool { return true }
func (t *closeStoreDeliveryTracker) MarkPendingDelivery(string)     {}
func (t *closeStoreDeliveryTracker) ClearPendingDelivery(string)    { t.cleared = true }
func (t *closeStoreDeliveryTracker) IsRecoveryRequired(string) bool { return false }
func (t *closeStoreDeliveryTracker) IsPendingDelivery(string, string) bool {
	if !t.closed {
		_ = t.st.Close()
		t.closed = true
	}
	return true
}
func (t *closeStoreDeliveryTracker) ClearPendingDeliveryIfMatch(string, string) { t.cleared = true }

func TestTriggerPostCompletePersistenceFailureRetainsGuard(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	mgr := NewManager("agent-1", nil, &llm.MockClient{}, nil, nil, st, TurnOptions{}, nil)
	tracker := &closeStoreDeliveryTracker{st: st}
	rt := newRuntime("postcomplete-failure", "agent-1", nil, mgr.llm, nil, nil, st, logx.Discard(), nil, nil, nil, false, 0, 0, mgr.turn, tracker)
	rt.session = Session{ID: "postcomplete-failure", AgentID: "agent-1"}
	if _, err := rt.inputBox.Append(InputKindTrigger, queue.Envelope{TriggerID: "t", DeliveryID: "d", Content: "drop"}); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.inputBox.Append(InputKindUser, queue.Envelope{Content: "must remain"}); err != nil {
		t.Fatal(err)
	}
	rt.start(context.Background())
	rt.signalInputBox()
	select {
	case <-rt.done:
	case <-time.After(2 * time.Second):
		t.Fatal("consumer did not fail closed")
	}
	got, ok := rt.inputBox.InFlight()
	if !ok || !got.Completed || got.Env.DeliveryID != "d" {
		t.Fatalf("completed ownership guard = %+v, ok=%v", got, ok)
	}
	if next, ok := rt.inputBox.Peek(); !ok || next.Env.Content != "must remain" {
		t.Fatalf("next input was consumed or lost: %+v %v", next, ok)
	}
	if tracker.cleared {
		t.Fatal("delivery was cleared after failed completion persistence")
	}
}
