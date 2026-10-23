package triggers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyMigrationBacksUpAndPersists(t *testing.T) {
	p := filepath.Join(t.TempDir(), "triggers.json")
	legacy := `{"triggers":[{"trigger_id":"t","target_agent_id":"a","condition":{"interval_seconds":60},"enabled":false}],"history":[]}`
	if err := os.WriteFile(p, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(p, 20); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p + ".v1.bak")
	if string(b) != legacy {
		t.Fatal("legacy backup mismatch")
	}
	data, _ := os.ReadFile(p)
	if !strings.Contains(string(data), `"schema_version": 2`) {
		t.Fatal("migration was not persisted")
	}
	if err := os.WriteFile(p+".v1.bak", []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(p, 20); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(p + ".v1.bak")
	if string(b) != "keep" {
		t.Fatal("backup was overwritten")
	}
}

func TestV011Schema2TriggerRestorePreservesAutoTargetAndPendingDelivery(t *testing.T) {
	p := filepath.Join(t.TempDir(), "triggers.json")
	fixture := `{"schema_version":2,"triggers":[{"trigger_id":"trigger-v011","name":"fixture auto wake","condition":{"interval_seconds":3600},"target_agent_id":"agent-v011","target_session_id":"agent-v011","session_target_mode":"fixed","enabled":true,"fire_count":3,"last_fired_at":1000,"next_fire_at":4600,"created_at":900,"updated_at":1000,"pending_delivery_id":"delivery-v011","pending_session_id":"agent-v011","pending_occurrence":4600,"pending_condition_reason":"condition true","pending_condition_content":"fixture work","pending_condition_payload":{"source":"fixture"},"pending_condition_approved":false,"recovery_required":false,"owner_agent_id":"agent-v011","controller":"auto","controller_id":"agent-v011","revision":2,"created_by":"agent-v011"}],"history":[{"fire_id":"fire-v011","trigger_id":"trigger-v011","delivery_id":"delivery-v011","status":"queued","reason":"pending delivery","session_id":"agent-v011","content":"fixture work","fired_at":1000}]}`
	if err := os.WriteFile(p, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := OpenStore(p, 20)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := st.GetTrigger("trigger-v011")
	if !ok || got.TargetAgentID != "agent-v011" || got.TargetSessionID == nil || *got.TargetSessionID != "agent-v011" ||
		got.PendingDeliveryID == nil || *got.PendingDeliveryID != "delivery-v011" || got.Controller != "auto" || got.Revision != 2 {
		t.Fatalf("v0.11 trigger target/delivery was not restored: trigger=%+v ok=%v", got, ok)
	}
	if got.Enabled || !got.RecoveryRequired || got.RecoveryReason != "pending delivery requires recovery after restart" {
		t.Fatalf("pending delivery was not fenced for explicit recovery: %+v", got)
	}
	history := st.ListHistory("trigger-v011")
	if len(history) != 1 || history[0].FireID != "fire-v011" || history[0].DeliveryID != "delivery-v011" || history[0].Status != FireStatusQueued {
		t.Fatalf("v0.11 trigger history was not restored: %+v", history)
	}
	current, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(current), `"schema_version": 2`) || !strings.Contains(string(current), `"delivery-v011"`) || !strings.Contains(string(current), `"recovery_required": true`) {
		t.Fatalf("schema 2 trigger restore lost its delivery/recovery state: %s", current)
	}
	if _, err := os.Stat(p + ".v1.bak"); !os.IsNotExist(err) {
		t.Fatalf("current schema unexpectedly created migration backup: err=%v", err)
	}
	reopened, err := OpenStore(p, 20)
	if err != nil {
		t.Fatal(err)
	}
	got, ok = reopened.GetTrigger("trigger-v011")
	if !ok || got.Enabled || !got.RecoveryRequired || got.PendingDeliveryID == nil || *got.PendingDeliveryID != "delivery-v011" {
		t.Fatalf("trigger restart lost its explicit recovery fence: trigger=%+v ok=%v", got, ok)
	}
}

func TestFutureSchemaRejected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "triggers.json")
	if err := os.WriteFile(p, []byte(`{"schema_version":99,"triggers":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(p, 20); err == nil {
		t.Fatal("future schema accepted")
	}
}

func TestValidateOwnersDisablesInvalidAndDoesNotGuessV2(t *testing.T) {
	st, _ := OpenStore(filepath.Join(t.TempDir(), "triggers.json"), 20)
	d := Definition{TriggerID: "bad", OwnerAgentID: "", TargetAgentID: "a", Controller: "retired", ControllerID: "wrong"}
	if _, err := st.CreateTrigger(d); err != nil {
		t.Fatal(err)
	}
	if err := st.ValidateOwners(map[string]bool{"a": true}); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetTrigger("bad")
	if got.Enabled || !got.RecoveryRequired || got.OwnerAgentID != "" {
		t.Fatalf("got=%+v", got)
	}
}
