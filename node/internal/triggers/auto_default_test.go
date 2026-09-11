package triggers

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnsureAutoDefaultIsStableAndPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "triggers.json")
	now := time.Unix(100, 0)
	s, err := OpenStore(path, 20)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.EnsureAutoDefault("agent-a", 1800, now)
	if err != nil {
		t.Fatal(err)
	}
	if first.TriggerID != AutoDefaultTriggerID("agent-a") || first.Controller != "auto" || first.ControllerID != "agent-a" || first.OwnerAgentID != "agent-a" || first.TargetAgentID != "agent-a" || first.TargetSessionID == nil || *first.TargetSessionID != "agent-a" || first.SessionTargetMode != SessionTargetFixed {
		t.Fatalf("identity = %+v", first)
	}
	second, err := s.EnsureAutoDefault("agent-a", 1800, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision != first.Revision || second.NextFireAt == nil || *second.NextFireAt != *first.NextFireAt {
		t.Fatalf("same sync changed durable schedule: first=%+v second=%+v", first, second)
	}
	changed, err := s.EnsureAutoDefault("agent-a", 3600, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if changed.Revision != first.Revision+1 || intFromAny(changed.Condition["interval_seconds"]) != 3600 || changed.NextFireAt == nil {
		t.Fatalf("changed sync = %+v", changed)
	}
	reopened, err := OpenStore(path, 20)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reopened.GetTrigger(AutoDefaultTriggerID("agent-a"))
	if !ok || got.Controller != "auto" || intFromAny(got.Condition["interval_seconds"]) != 3600 {
		t.Fatalf("reopened = %+v ok=%v", got, ok)
	}
}

func TestEnsureAutoDefaultDisableInvalidatesPendingAndRejectsMutation(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "triggers.json"), 20)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(200, 0)
	d, err := s.EnsureAutoDefault("agent-a", 60, now)
	if err != nil {
		t.Fatal(err)
	}
	d.PendingDeliveryID = stringPtr("delivery-1")
	d.PendingSessionID = stringPtr("agent-a")
	if err := s.ReplaceTrigger(d); err != nil {
		t.Fatal(err)
	}
	s.MarkPendingDelivery(d.TriggerID)
	if !s.HasPendingDelivery(d.TriggerID) {
		t.Fatal("fixture pending delivery was not marked")
	}
	off, err := s.EnsureAutoDefault("agent-a", 0, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if off.Enabled || off.NextFireAt != nil || off.PendingDeliveryID != nil || off.PendingSessionID != nil || s.HasPendingDelivery(d.TriggerID) {
		t.Fatalf("disabled auto trigger retained executable state: %+v pending=%v", off, s.HasPendingDelivery(d.TriggerID))
	}
	if intFromAny(off.Condition["interval_seconds"]) <= 0 {
		t.Fatalf("disabled trigger lost a valid interval condition: %+v", off.Condition)
	}
	if _, err := s.UpdateTrigger(d.TriggerID, UpdatePatch{Name: stringPtr("tampered")}, now); err == nil {
		t.Fatal("direct update must reject system-owned trigger")
	}
	if _, err := s.UpdateAuthorized(Principal{Kind: "agent", AgentID: "agent-a"}, d.TriggerID, off.Revision, UpdatePatch{Name: stringPtr("tampered")}, now); err == nil {
		t.Fatal("authorized update must reject system-owned trigger")
	}
	if err := s.DeleteAuthorized(Principal{Kind: "agent", AgentID: "agent-a"}, d.TriggerID, off.Revision); err == nil {
		t.Fatal("authorized delete must reject system-owned trigger")
	}
	if s.DeleteTrigger(d.TriggerID) {
		t.Fatal("direct delete must reject system-owned trigger")
	}
}

func TestEnsureAutoDefaultRejectsCollisionAndRollsBackPersistenceFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "triggers.json")
	s, err := OpenStore(path, 20)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(300, 0)
	if _, err := s.CreateTrigger(Definition{
		TriggerID: AutoDefaultTriggerID("agent-a"), Name: "user collision", Condition: map[string]any{"interval_seconds": 60}, TargetAgentID: "agent-a", OwnerAgentID: "agent-a", Controller: "user", ControllerID: "agent-a", SessionTargetMode: SessionTargetFixed, Revision: 1, Enabled: true, NextFireAt: func() *float64 { v := float64(now.Add(time.Minute).UnixNano()) / 1e9; return &v }(), CreatedAt: float64(now.UnixNano()) / 1e9,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnsureAutoDefault("agent-a", 60, now); err == nil {
		t.Fatal("expected stable ID collision to be rejected")
	}

	good, err := s.EnsureAutoDefault("agent-b", 60, now)
	if err != nil {
		t.Fatal(err)
	}
	good.FireCount = 3
	last := timeToUnixFloat(now.Add(-time.Minute))
	good.LastFiredAt = &last
	if err := s.ReplaceTrigger(good); err != nil {
		t.Fatal(err)
	}
	s.MarkPendingDelivery(good.TriggerID)
	blocker := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.path = filepath.Join(blocker, "triggers.json")
	if _, err := s.EnsureAutoDefault("agent-b", 120, now.Add(time.Minute)); err == nil {
		t.Fatal("expected persistence failure")
	}
	s.path = path
	after, ok := s.GetTrigger(good.TriggerID)
	if !ok || after.Revision != good.Revision || after.FireCount != good.FireCount || after.LastFiredAt == nil || *after.LastFiredAt != *good.LastFiredAt || intFromAny(after.Condition["interval_seconds"]) != 60 || after.NextFireAt == nil || *after.NextFireAt != *good.NextFireAt || !s.HasPendingDelivery(good.TriggerID) {
		t.Fatalf("failed sync was not rolled back: %+v ok=%v", after, ok)
	}
}

func TestEnsureAutoDefaultDisablePersistenceFailureKeepsPendingAndDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "triggers.json")
	s, err := OpenStore(path, 20)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(400, 0)
	d, err := s.EnsureAutoDefault("agent-a", 60, now)
	if err != nil {
		t.Fatal(err)
	}
	d.PendingDeliveryID = stringPtr("delivery-off")
	d.PendingSessionID = stringPtr("agent-a")
	if err := s.ReplaceTrigger(d); err != nil {
		t.Fatal(err)
	}
	s.MarkPendingDelivery(d.TriggerID)
	blocker := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.path = filepath.Join(blocker, "triggers.json")
	if _, err := s.EnsureAutoDefault("agent-a", 0, now.Add(time.Minute)); err == nil {
		t.Fatal("expected disable persistence failure")
	}
	s.path = path
	after, ok := s.GetTrigger(d.TriggerID)
	if !ok || after.Enabled != d.Enabled || after.PendingDeliveryID == nil || *after.PendingDeliveryID != "delivery-off" || after.PendingSessionID == nil || *after.PendingSessionID != "agent-a" || !s.HasPendingDelivery(d.TriggerID) {
		t.Fatalf("failed disable changed in-memory state: %+v pending=%v ok=%v", after, s.HasPendingDelivery(d.TriggerID), ok)
	}
	reopened, err := OpenStore(path, 20)
	if err != nil {
		t.Fatal(err)
	}
	disk, ok := reopened.GetTrigger(d.TriggerID)
	if !ok || disk.PendingDeliveryID == nil || *disk.PendingDeliveryID != "delivery-off" || disk.PendingSessionID == nil || *disk.PendingSessionID != "agent-a" || disk.Enabled || !disk.RecoveryRequired {
		t.Fatalf("failed disable changed persisted state: %+v ok=%v", disk, ok)
	}
}
