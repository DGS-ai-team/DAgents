package session

import (
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func TestP0ReconcileRecoveredDeliveryAfterExplicitRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "triggers.json")
	store, err := triggers.OpenStore(path, 20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateTrigger(triggers.Definition{TriggerID: "t-recover", TargetAgentID: "agent-1", Enabled: true, PendingDeliveryID: strPtr("delivery-old"), NextFireAt: floatPtr(100)}); err != nil {
		t.Fatal(err)
	}
	restarted, err := triggers.OpenStore(path, 20)
	if err != nil {
		t.Fatal(err)
	}
	def, _ := restarted.GetTrigger("t-recover")
	if !def.RecoveryRequired || def.Enabled {
		t.Fatalf("restart did not fence delivery: %+v", def)
	}
	if err := restarted.RecoverPendingDelivery("t-recover", "delivery-old"); err != nil {
		t.Fatal(err)
	}

	r := newLifecycleTestRuntime()
	r.logger = logx.Discard()
	r.inputBox = NewInputBox()
	r.triggerDelivery = restarted
	if err := r.lifecycleBeginHumanTurn(); err != nil {
		t.Fatal(err)
	}
	state := r.turnCoordinator.Snapshot()
	continuation := queue.Envelope{
		RequestType: queue.RequestTypeTurnContinuation,
		TurnID:      state.TurnID,
		Generation:  state.Generation,
	}
	if !r.acceptEnvelope(continuation) {
		t.Fatal("continuation matching the active turn should be accepted before recovery reconciliation")
	}
	if _, err := r.inputBox.Append(InputKindTrigger, queue.Envelope{TriggerID: "t-recover", DeliveryID: "delivery-old", Content: "old"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.inputBox.Pop(); !ok {
		t.Fatal("expected in-flight restored input")
	}
	r.reconcileRestoredInputBox()
	if _, ok := r.inputBox.InFlight(); ok {
		t.Fatal("reconciled old delivery remained in flight")
	}
	if r.acceptEnvelope(continuation) {
		t.Fatal("stale continuation was accepted after recovery reconciliation")
	}
	state = r.turnCoordinator.Snapshot()
	if state.TurnStatus == turn.TurnStatusRunning {
		t.Fatal("recovery did not cancel active turn")
	}
}

func floatPtr(v float64) *float64 { return &v }
