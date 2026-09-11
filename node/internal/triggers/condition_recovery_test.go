package triggers

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func conditionBoolPtr(v bool) *bool { return &v }

func conditionRecoveryFixture(t *testing.T) (*Store, Definition, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "triggers.json")
	s, err := OpenStore(path, 20)
	if err != nil {
		t.Fatal(err)
	}
	session := "session-condition"
	d, err := s.CreateAuthorized(Principal{Kind: "agent", ID: "agent-1", AgentID: "agent-1"}, CreateInput{
		Name: "condition", TargetAgentID: "agent-1", TargetSessionID: &session, TaskTemplate: "run",
		Condition: map[string]any{"interval_seconds": 60, "cmd": "touch marker"}, Enabled: conditionBoolPtr(true),
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimDelivery(d.TriggerID, "delivery-condition", session); err != nil {
		t.Fatal(err)
	}
	return s, d, path
}

func TestConditionRecoveryFencePersistsAndDoesNotReplay(t *testing.T) {
	s, d, path := conditionRecoveryFixture(t)
	if err := s.MarkConditionRecovery(d.TriggerID, "delivery-condition", "submit failed"); err != nil {
		t.Fatal(err)
	}
	got, ok := s.GetTrigger(d.TriggerID)
	if !ok || !got.RecoveryRequired || got.Enabled || got.PendingDeliveryID == nil || *got.PendingDeliveryID != "delivery-condition" {
		t.Fatalf("recovery fence = %+v", got)
	}
	reopened, err := OpenStore(path, 20)
	if err != nil {
		t.Fatal(err)
	}
	restored, ok := reopened.GetTrigger(d.TriggerID)
	if !ok || !restored.RecoveryRequired || restored.Enabled || restored.PendingDeliveryID == nil || *restored.PendingDeliveryID != "delivery-condition" {
		t.Fatalf("reopened recovery fence = %+v", restored)
	}
}

func TestMarkConditionRecoverySaveFailureRollsBackMemoryAndDisk(t *testing.T) {
	s, d, path := conditionRecoveryFixture(t)
	original, _ := s.GetTrigger(d.TriggerID)
	diskBefore, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s.path = filepath.Dir(path)
	if err := s.MarkConditionRecovery(d.TriggerID, "delivery-condition", "disk unavailable"); err == nil {
		t.Fatal("expected recovery persistence failure")
	}
	current, _ := s.GetTrigger(d.TriggerID)
	if current.RecoveryRequired != original.RecoveryRequired || current.Enabled != original.Enabled || current.RecoveryReason != original.RecoveryReason {
		t.Fatalf("memory changed after failed save: before=%+v after=%+v", original, current)
	}
	diskAfter, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(diskBefore, diskAfter) {
		t.Fatal("disk changed after failed save")
	}
}
