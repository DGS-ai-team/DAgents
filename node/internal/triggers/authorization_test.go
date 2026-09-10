package triggers

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestAuthorizedAuditContainsPrincipalFieldsOnly(t *testing.T) {
	st, _ := OpenStore(t.TempDir()+"/triggers.json", 20)
	var logs bytes.Buffer
	st.SetLogger(slog.New(slog.NewTextHandler(&logs, nil)))
	p := Principal{Kind: "agent", ID: "actor-1", AgentID: "a"}
	d, err := st.CreateAuthorized(p, CreateInput{Name: "audit", Condition: map[string]any{"interval_seconds": 60}, TargetAgentID: "a", TaskTemplate: "secret task"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	name := "audit-2"
	if _, err := st.UpdateAuthorized(p, d.TriggerID, d.Revision, UpdatePatch{Name: &name}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateAuthorized(Principal{Kind: "agent", AgentID: "other"}, d.TriggerID, d.Revision+1, UpdatePatch{Name: &name}, time.Now()); err == nil {
		t.Fatal("other owner update unexpectedly succeeded")
	}
	if _, err := st.UpdateAuthorized(p, d.TriggerID, d.Revision, UpdatePatch{Name: &name}, time.Now()); err == nil {
		t.Fatal("stale update unexpectedly succeeded")
	}
	if err := st.DeleteAuthorized(p, d.TriggerID, d.Revision+1); err != nil {
		t.Fatal(err)
	}
	text := logs.String()
	for _, op := range []string{"create", "update", "delete"} {
		if !strings.Contains(text, "operation="+op) {
			t.Fatalf("audit missing %s: %s", op, text)
		}
	}
	for _, reason := range []string{"not_owner_or_controller", "revision_conflict"} {
		if !strings.Contains(text, "result=denied") || !strings.Contains(text, "reason="+reason) {
			t.Fatalf("audit missing denied %s: %s", reason, text)
		}
	}
	for _, field := range []string{"actor_kind=agent", "actor_id=actor-1", "owner_agent_id=a", "result=allowed"} {
		if !strings.Contains(text, field) {
			t.Fatalf("audit missing %s: %s", field, text)
		}
	}
	if strings.Contains(text, "secret task") {
		t.Fatal("audit leaked task template")
	}
}

type revisionBumpSubmitter struct {
	store   *Store
	id      string
	submits int
}

func (s *revisionBumpSubmitter) EnsureSession(string) (string, error) {
	name := "bumped"
	_, _ = s.store.UpdateTrigger(s.id, UpdatePatch{Name: &name}, time.Now())
	return "sess", nil
}
func (s *revisionBumpSubmitter) SubmitTriggerMessage(string, string, string) error {
	s.submits++
	return nil
}

func authTrigger(t *testing.T, st *Store, owner, controller string) Definition {
	t.Helper()
	d, err := NewDefinitionFromCreate(CreateInput{Name: "t", Condition: map[string]any{"interval_seconds": 60}, TargetAgentID: owner, TaskTemplate: "x"}, "node", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	d.OwnerAgentID, d.Controller, d.ControllerID = owner, controller, owner
	if _, err = st.CreateTrigger(d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestAuthorizedRecoveryCASAndOwnership(t *testing.T) {
	st, _ := OpenStore(t.TempDir()+"/triggers.json", 20)
	d := authTrigger(t, st, "a", "user")
	if err := st.ClaimDeliveryForOccurrence(d.TriggerID, "del", "sess", nil); err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	cur := st.triggers[d.TriggerID]
	cur.RecoveryRequired = true
	cur.PendingDeliveryID = func() *string { v := "del"; return &v }()
	st.triggers[d.TriggerID] = cur
	st.mu.Unlock()
	if err := st.RecoverAuthorized(Principal{Kind: "agent", AgentID: "b"}, d.TriggerID, d.Revision, "del"); !IsNotFound(err) {
		t.Fatalf("other owner err=%v", err)
	}
	if err := st.RecoverAuthorized(Principal{Kind: "agent", AgentID: "a"}, d.TriggerID, d.Revision+1, "del"); err == nil {
		t.Fatal("stale revision accepted")
	}
	if err := st.RecoverAuthorized(Principal{Kind: "agent", AgentID: "a"}, d.TriggerID, d.Revision, "wrong"); err == nil {
		t.Fatal("wrong delivery accepted")
	}
	got, _ := st.GetTrigger(d.TriggerID)
	if got.PendingDeliveryID == nil || *got.PendingDeliveryID != "del" {
		t.Fatal("pending delivery was cleared")
	}
	if err := st.RecoverAuthorized(Principal{Kind: "agent", AgentID: "a"}, d.TriggerID, d.Revision, "del"); err != nil {
		t.Fatal(err)
	}
}

func TestAuthorizedHistoryIsolation(t *testing.T) {
	st, _ := OpenStore(t.TempDir()+"/t.json", 20)
	d := authTrigger(t, st, "a", "user")
	in := map[string]any{"x": map[string]any{"nested": []any{"y"}}}
	st.AddHistory(FireRecord{TriggerID: d.TriggerID, FiredAt: 1, Payload: in})
	in["x"].(map[string]any)["nested"].([]any)[0] = "mutated"
	if _, err := st.HistoryAuthorized(Principal{Kind: "agent", AgentID: "b"}, d.TriggerID); !IsNotFound(err) {
		t.Fatalf("other owner err=%v", err)
	}
	h, err := st.HistoryAuthorized(Principal{Kind: "agent", AgentID: "a"}, d.TriggerID)
	if err != nil || len(h) != 1 {
		t.Fatalf("history=%v err=%v", h, err)
	}
	h[0].Payload["x"].(map[string]any)["nested"].([]any)[0] = "mutated"
	h2, _ := st.HistoryAuthorized(Principal{Kind: "agent", AgentID: "a"}, d.TriggerID)
	if h2[0].Payload["x"].(map[string]any)["nested"].([]any)[0] != "y" {
		t.Fatal("history payload leaked mutable map")
	}
}

func TestAuthorizedUpdateRejectsOtherAndManaged(t *testing.T) {
	st, _ := OpenStore(t.TempDir()+"/t.json", 20)
	d := authTrigger(t, st, "a", "user")
	name := "changed"
	if _, err := st.UpdateAuthorized(Principal{Kind: "agent", AgentID: "b"}, d.TriggerID, d.Revision, UpdatePatch{Name: &name}, time.Now()); !IsNotFound(err) {
		t.Fatalf("other update err=%v", err)
	}
	d.Controller = "retired"
	_ = st.ReplaceTrigger(d)
	if _, err := st.UpdateAuthorized(Principal{Kind: "agent", AgentID: "a"}, d.TriggerID, d.Revision, UpdatePatch{Name: &name}, time.Now()); !IsNotFound(err) {
		t.Fatalf("managed update err=%v", err)
	}
}

func TestAuthorizedCloneIsolation(t *testing.T) {
	st, _ := OpenStore(t.TempDir()+"/t.json", 20)
	d := authTrigger(t, st, "a", "user")
	d.Condition["interval_seconds"] = 999
	g, _ := st.GetTrigger(d.TriggerID)
	g.Condition["interval_seconds"] = 999
	g2, _ := st.GetTrigger(d.TriggerID)
	if g2.Condition["interval_seconds"] == 999 {
		t.Fatal("trigger condition leaked mutable map")
	}
}

func TestAuthorizedFireStaleRevisionNoDelivery(t *testing.T) {
	st, _ := OpenStore(t.TempDir()+"/t.json", 20)
	d := authTrigger(t, st, "a", "user")
	sub := &revisionBumpSubmitter{store: st, id: d.TriggerID}
	sch := NewScheduler(st, sub, 60)
	if _, err := sch.FireAuthorized(Principal{Kind: "admin", ID: "local"}, d.TriggerID, 0, "manual", nil, false, nil); err != nil {
		t.Fatal(err)
	}
	if sub.submits != 0 {
		t.Fatalf("stale fire delivered=%d", sub.submits)
	}
	if st.HasPendingDelivery(d.TriggerID) {
		t.Fatal("stale fire claimed delivery")
	}
	cur, _ := st.GetTrigger(d.TriggerID)
	if _, err := sch.FireAuthorized(Principal{Kind: "admin", ID: "local"}, d.TriggerID, d.Revision, "manual", nil, false, nil); err == nil {
		t.Fatal("old explicit revision accepted")
	}
	cur.Controller = "maintenance"
	_ = st.ReplaceTrigger(*cur)
	if _, err := sch.FireAuthorized(Principal{Kind: "admin", ID: "local"}, d.TriggerID, 0, "manual", nil, false, nil); err == nil {
		t.Fatal("maintenance controller fired")
	}
}
