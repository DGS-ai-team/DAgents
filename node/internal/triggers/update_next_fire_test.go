package triggers

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func strPtr(v string) *string { return &v }

func writeFileForTest(path string) error { return os.WriteFile(path, []byte("file"), 0600) }

func TestUpdateNextFireAtPreservesDeliveryStateAndRollsBackOnSaveFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "triggers.json")
	st, err := OpenStore(path, 20)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	def, err := NewDefinitionFromCreate(CreateInput{
		Name: "goal", TargetAgentID: "agent-1", TargetSessionID: strPtr("session-1"),
		TaskTemplate: "run", Condition: map[string]any{"interval_seconds": 60},
	}, "agent-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateTrigger(def); err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimDelivery(def.TriggerID, "delivery-1", "session-1"); err != nil {
		t.Fatal(err)
	}
	st.MarkPendingDelivery(def.TriggerID)
	before, ok := st.GetTrigger(def.TriggerID)
	if !ok || before.PendingDeliveryID == nil || before.PendingSessionID == nil || !st.HasPendingDelivery(def.TriggerID) {
		t.Fatalf("delivery state not established: %+v pending=%v", before, st.HasPendingDelivery(def.TriggerID))
	}
	wantNext := float64(now.Add(10*time.Minute).UnixNano()) / 1e9
	if err := st.UpdateNextFireAt(def.TriggerID, &wantNext); err != nil {
		t.Fatal(err)
	}
	after, _ := st.GetTrigger(def.TriggerID)
	if after.NextFireAt == nil || *after.NextFireAt != wantNext || after.PendingDeliveryID == nil || *after.PendingDeliveryID != "delivery-1" || after.PendingSessionID == nil || *after.PendingSessionID != "session-1" || !st.HasPendingDelivery(def.TriggerID) {
		t.Fatalf("narrow update changed delivery state: before=%+v after=%+v pending=%v", before, after, st.HasPendingDelivery(def.TriggerID))
	}

	badParent := filepath.Join(t.TempDir(), "parent-file")
	if err := writeFileForTest(badParent); err != nil {
		t.Fatal(err)
	}
	st.path = filepath.Join(badParent, "triggers.json")
	failedNext := float64(now.Add(20*time.Minute).UnixNano()) / 1e9
	if err := st.UpdateNextFireAt(def.TriggerID, &failedNext); err == nil {
		t.Fatal("expected persistence failure")
	}
	st.path = path
	rolled, _ := st.GetTrigger(def.TriggerID)
	if rolled.NextFireAt == nil || *rolled.NextFireAt != wantNext || rolled.PendingDeliveryID == nil || *rolled.PendingDeliveryID != "delivery-1" || !st.HasPendingDelivery(def.TriggerID) {
		t.Fatalf("failed update changed memory: %+v", rolled)
	}
	reloaded, err := OpenStore(path, 20)
	if err != nil {
		t.Fatal(err)
	}
	persisted, _ := reloaded.GetTrigger(def.TriggerID)
	if persisted.NextFireAt == nil || *persisted.NextFireAt != wantNext || persisted.PendingDeliveryID == nil || *persisted.PendingDeliveryID != "delivery-1" || persisted.PendingSessionID == nil || *persisted.PendingSessionID != "session-1" {
		t.Fatalf("failed update changed disk: %+v", persisted)
	}
}
