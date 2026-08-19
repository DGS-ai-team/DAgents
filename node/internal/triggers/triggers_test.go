package triggers

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnsureScheduleConditionRejectsEmpty(t *testing.T) {
	if _, err := EnsureScheduleCondition(map[string]any{}); err == nil {
		t.Fatal("expected error for empty condition")
	}
}

func TestStoreCreateUpdateDuePersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "triggers.json")
	store, err := OpenStore(path, 50)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(100, 0)
	def, err := NewDefinitionFromCreate(CreateInput{
		Name:         "heartbeat",
		TaskTemplate: "check {trigger_name}",
		Condition:    map[string]any{"interval_seconds": 10},
	}, "agent-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTrigger(def); err != nil {
		t.Fatal(err)
	}
	due := store.DueTriggers(now.Add(11 * time.Second))
	if len(due) != 1 {
		t.Fatalf("due = %d", len(due))
	}
	name := "renamed"
	updated, err := store.UpdateTrigger(def.TriggerID, UpdatePatch{Name: &name}, now.Add(20*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "renamed" {
		t.Fatalf("name = %q", updated.Name)
	}
	reloaded, err := OpenStore(path, 50)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.GetTrigger(def.TriggerID)
	if !ok || got.Name != "renamed" {
		t.Fatalf("persisted = %+v ok=%v", got, ok)
	}
}

func TestRenderTaskTemplate(t *testing.T) {
	def := Definition{TriggerID: "t1", Name: "demo"}
	out := RenderTaskTemplate("hi {trigger_name} reason={reason}", def, "manual", map[string]any{"k": 1})
	if out != "hi demo reason=manual" {
		t.Fatalf("out = %q", out)
	}
}

type fakeSubmitter struct {
	sessions []string
	messages []string
}

func (f *fakeSubmitter) EnsureSession(requestedID string) (string, error) {
	id := requestedID
	if id == "" {
		id = "sess-generated"
	}
	f.sessions = append(f.sessions, id)
	return id, nil
}

func (f *fakeSubmitter) SubmitTriggerMessage(sessionID, triggerID, content string) error {
	f.messages = append(f.messages, sessionID+":"+triggerID+":"+content)
	return nil
}

func TestSchedulerFireTriggerQueuesMessage(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "t.json"), 20)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(200, 0)
	def, err := NewDefinitionFromCreate(CreateInput{
		Name:         "job",
		TaskTemplate: "run {trigger_name}",
		Condition:    map[string]any{"interval_seconds": 60},
	}, "agent-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTrigger(def); err != nil {
		t.Fatal(err)
	}
	sub := &fakeSubmitter{}
	sched := NewScheduler(store, sub, 5)
	record, err := sched.FireTrigger(def.TriggerID, "agent_tool", nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != FireStatusQueued {
		t.Fatalf("status = %s", record.Status)
	}
	if len(sub.messages) != 1 || sub.messages[0] != "sess-generated:"+def.TriggerID+":run job" {
		t.Fatalf("messages = %v", sub.messages)
	}
}

func TestSchedulerSkipsWhenPendingDelivery(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "t.json"), 20)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(200, 0)
	def, err := NewDefinitionFromCreate(CreateInput{
		Name:         "job",
		TaskTemplate: "run {trigger_name}",
		Condition:    map[string]any{"interval_seconds": 60},
	}, "agent-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTrigger(def); err != nil {
		t.Fatal(err)
	}
	sub := &fakeSubmitter{}
	sched := NewScheduler(store, sub, 5)
	first, err := sched.FireTrigger(def.TriggerID, "manual", nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != FireStatusQueued {
		t.Fatalf("first status = %s", first.Status)
	}
	second, err := sched.FireTrigger(def.TriggerID, "manual", nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != FireStatusSkipped {
		t.Fatalf("second status = %s", second.Status)
	}
	if len(sub.messages) != 1 {
		t.Fatalf("messages = %v", sub.messages)
	}
	store.ClearPendingDelivery(def.TriggerID)
	third, err := sched.FireTrigger(def.TriggerID, "manual", nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if third.Status != FireStatusQueued {
		t.Fatalf("third status = %s", third.Status)
	}
	if len(sub.messages) != 2 {
		t.Fatalf("messages = %v", sub.messages)
	}
}

func TestSchedulerStopAlwaysHonorsCancellation(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "t.json"), 20)
	if err != nil {
		t.Fatal(err)
	}
	sched := NewScheduler(store, &fakeSubmitter{}, 5)

	for i := 0; i < 100; i++ {
		sched.Start()
		stopped := make(chan struct{})
		go func() {
			sched.Stop()
			close(stopped)
		}()

		select {
		case <-stopped:
		case <-time.After(time.Second):
			t.Fatalf("scheduler stop timed out on iteration %d", i)
		}
	}
}

func TestEvaluateDueIntervalFiresEvenWhenOverdue(t *testing.T) {
	nextAt := time.Now().Add(-2 * time.Minute)
	def := Definition{
		Enabled:    true,
		Condition:  map[string]any{"interval_seconds": 60},
		NextFireAt: ptrFloat(timeToUnixFloat(nextAt)),
	}
	decision, _ := EvaluateDue(def, time.Now())
	if decision != DueFire {
		t.Fatalf("decision = %d want DueFire for overdue interval", decision)
	}
}

func TestConditionCmdIgnoresMissingKey(t *testing.T) {
	if got := ConditionCmd(map[string]any{"interval_seconds": 60}); got != "" {
		t.Fatalf("ConditionCmd = %q, want empty", got)
	}
	if got := ConditionCmd(map[string]any{"cmd": nil}); got != "" {
		t.Fatalf("ConditionCmd nil = %q, want empty", got)
	}
}

func TestSchedulerTickIntervalWhenDue(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "t.json"), 20)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	def, err := NewDefinitionFromCreate(CreateInput{
		Name:         "tick",
		TaskTemplate: "hello",
		Condition:    map[string]any{"interval_seconds": 1},
	}, "agent-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTrigger(def); err != nil {
		t.Fatal(err)
	}
	sub := &fakeSubmitter{}
	sched := NewScheduler(store, sub, 1)
	sched.RunOnceForTest(nil, now.Add(500*time.Millisecond))
	if len(sub.messages) != 0 {
		t.Fatalf("early tick should not fire: %v", sub.messages)
	}
	sched.RunOnceForTest(nil, now.Add(1100*time.Millisecond))
	if len(sub.messages) != 1 {
		t.Fatalf("expected 1 message, got %v", sub.messages)
	}
}

func TestSchedulerTickIntervalOverdueStillFires(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "t.json"), 20)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	def, err := NewDefinitionFromCreate(CreateInput{
		Name:         "overdue",
		TaskTemplate: "catch-up",
		Condition:    map[string]any{"interval_seconds": 30},
	}, "agent-1", now.Add(-5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	past := timeToUnixFloat(now.Add(-2 * time.Minute))
	def.NextFireAt = &past
	if _, err := store.CreateTrigger(def); err != nil {
		t.Fatal(err)
	}
	sub := &fakeSubmitter{}
	sched := NewScheduler(store, sub, 5)
	sched.RunOnceForTest(nil, now)
	if len(sub.messages) != 1 {
		t.Fatalf("overdue interval should still fire once, got %v", sub.messages)
	}
}

func TestStoreLoadReconcilesMissingNextFireAt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "triggers.json")
	raw := `{"triggers":[{"trigger_id":"t1","name":"x","condition":{"interval_seconds":60},"target_agent_id":"local","task_template":"hi","enabled":true,"fire_count":0,"created_at":1,"updated_at":1}],"history":[]}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(path, 20)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := store.GetTrigger("t1")
	if !ok || got.NextFireAt == nil {
		t.Fatalf("expected reconciled next_fire_at, got %+v ok=%v", got, ok)
	}
}
