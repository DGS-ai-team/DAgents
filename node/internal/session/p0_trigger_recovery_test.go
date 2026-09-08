package session

import (
	"context"
	"encoding/json"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
	"os"
	"path/filepath"
	"testing"
)

func TestP0RestartRequiresRecoveryAndFencesOldDelivery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "triggers.json")
	raw := map[string]any{"triggers": []any{map[string]any{
		"trigger_id": "t1", "name": "job", "target_agent_id": "agent-1", "condition": map[string]any{"interval_seconds": 60},
		"task_template": "run", "enabled": true, "pending_delivery_id": "delivery-1", "next_fire_at": 100, "created_at": 1, "updated_at": 1,
	}}, "history": []any{}}
	b, _ := json.Marshal(raw)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := triggers.OpenStore(path, 20)
	if err != nil {
		t.Fatal(err)
	}
	d, ok := store.GetTrigger("t1")
	if !ok || !d.RecoveryRequired || d.Enabled {
		t.Fatalf("restart state = %+v", d)
	}
	if err := store.RecoverPendingDelivery("t1", "delivery-1"); err != nil {
		t.Fatal(err)
	}
	d, _ = store.GetTrigger("t1")
	if d.RecoveryRequired || d.Enabled || d.PendingDeliveryID != nil {
		t.Fatalf("recovered state = %+v", d)
	}
	mgr := testManager(t)
	defer mgr.Stop()
	mgr.SetTriggerDeliveryTracker(store)
	sess, _, err := mgr.Create("sess-old-delivery")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	before := len(rt.messages)
	if !rt.dispatchInput(context.Background(), InputRecord{Kind: InputKindTrigger, Env: queue.Envelope{TriggerID: "t1", DeliveryID: "delivery-1", Content: "old delivery"}}) {
		t.Fatal("old delivery should be consumed")
	}
	if len(rt.messages) != before {
		t.Fatalf("recovered old delivery executed a turn: before=%d after=%d", before, len(rt.messages))
	}
}

func TestP0RecoveredMailboxItemIsDiscardedWithoutTurnExecution(t *testing.T) {
	store, err := triggers.OpenStore(filepath.Join(t.TempDir(), "triggers.json"), 20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTrigger(triggers.Definition{TriggerID: "t1", TargetAgentID: "agent-1", Enabled: false, RecoveryRequired: true, RecoveryReason: "unknown", PendingDeliveryID: strPtr("delivery-1")}); err != nil {
		t.Fatal(err)
	}
	mgr := testManager(t)
	defer mgr.Stop()
	mgr.SetTriggerDeliveryTracker(store)
	sess, _, err := mgr.Create("sess-recovered")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	before := len(rt.messages)
	record := InputRecord{Kind: InputKindTrigger, Env: queue.Envelope{TriggerID: "t1", DeliveryID: "delivery-1", Content: "old delivery"}}
	if !rt.dispatchInput(context.Background(), record) {
		t.Fatal("recovered mailbox item should be consumed")
	}
	if len(rt.messages) != before {
		t.Fatalf("recovered item executed a turn: before=%d after=%d", before, len(rt.messages))
	}
}

func strPtr(v string) *string { return &v }
