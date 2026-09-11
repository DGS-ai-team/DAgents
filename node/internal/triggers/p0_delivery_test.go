package triggers

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type p0DeliverySubmitter struct {
	store    *Store
	mu       sync.Mutex
	messages int
	callback func(string, string)
}

func (s *p0DeliverySubmitter) EnsureSession(id string) (string, error) {
	if id == "" {
		id = "session-1"
	}
	return id, nil
}
func (s *p0DeliverySubmitter) SubmitTriggerMessage(id, trigger, content string) error { return nil }
func (s *p0DeliverySubmitter) SubmitTriggerMessageWithDelivery(id, trigger, delivery, content string) error {
	s.mu.Lock()
	s.messages++
	s.mu.Unlock()
	s.store.ClearPendingDeliveryIfMatch(trigger, delivery)
	if s.callback != nil {
		s.callback(trigger, delivery)
	}
	return nil
}

func TestP0AckedDeliveryAllowsNextFire(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "triggers.json"), 20)
	if err != nil {
		t.Fatal(err)
	}
	def, err := NewDefinitionFromCreate(CreateInput{Name: "job", TargetAgentID: "agent-1", TaskTemplate: "run", Condition: map[string]any{"interval_seconds": 60}}, "agent-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateTrigger(def); err != nil {
		t.Fatal(err)
	}
	sub := &p0DeliverySubmitter{store: store}
	sched := NewScheduler(store, sub, 1)
	record, e := sched.FireTrigger(def.TriggerID, "manual", nil, false, nil)
	if e != nil || record.Status != FireStatusQueued {
		t.Fatalf("first fire = %v, %+v", e, record)
	}
	record, e = sched.FireTrigger(def.TriggerID, "manual", nil, false, nil)
	if e != nil || record.Status != FireStatusQueued {
		t.Fatalf("second fire = %v, %+v", e, record)
	}
	if sub.messages != 2 {
		t.Fatalf("messages = %d, want 2", sub.messages)
	}
}

func TestP0StaleScheduledOccurrenceCannotBeClaimedTwice(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "triggers.json"), 20)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	def, err := NewDefinitionFromCreate(CreateInput{Name: "job", TargetAgentID: "agent-1", TaskTemplate: "run", Condition: map[string]any{"interval_seconds": 60}}, "agent-1", now.Add(-60*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	def.NextFireAt = func() *float64 { v := float64(now.Unix()); return &v }()
	if _, err = store.CreateTrigger(def); err != nil {
		t.Fatal(err)
	}
	var staleErr error
	sub := &p0DeliverySubmitter{store: store, callback: func(trigger, delivery string) {
		staleErr = store.ClaimDeliveryForOccurrence(trigger, "delivery-stale", "session-stale", def.NextFireAt)
	}}
	sched := NewScheduler(store, sub, 1)
	sched.RunOnceForTest(context.Background(), now)
	if staleErr == nil {
		t.Fatal("stale occurrence claim unexpectedly succeeded")
	}
	if sub.messages != 1 {
		t.Fatalf("stale occurrence delivered %d messages, want 1", sub.messages)
	}
}

func TestP0ClearDeliverySaveFailureRetainsOriginalID(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "triggers.json"), 20)
	if err != nil {
		t.Fatal(err)
	}
	def, err := NewDefinitionFromCreate(CreateInput{Name: "job", TargetAgentID: "agent-1", TaskTemplate: "run", Condition: map[string]any{"interval_seconds": 60}}, "agent-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateTrigger(def); err != nil {
		t.Fatal(err)
	}
	if err = store.ClaimDelivery(def.TriggerID, "delivery-old", "session-1"); err != nil {
		t.Fatal(err)
	}
	badParent := filepath.Join(t.TempDir(), "parent-file")
	if err = os.WriteFile(badParent, []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	store.path = filepath.Join(badParent, "triggers.json")
	store.ClearPendingDeliveryIfMatch(def.TriggerID, "delivery-old")
	got, _ := store.GetTrigger(def.TriggerID)
	if got.PendingDeliveryID == nil || *got.PendingDeliveryID != "delivery-old" {
		t.Fatalf("clear failure lost original delivery ID: %+v", got)
	}
}

func TestP0ConcurrentFireClaimsOnlyOneDelivery(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "triggers.json"), 20)
	if err != nil {
		t.Fatal(err)
	}
	def, err := NewDefinitionFromCreate(CreateInput{Name: "job", TargetAgentID: "agent-1", TaskTemplate: "run", Condition: map[string]any{"interval_seconds": 60}}, "agent-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateTrigger(def); err != nil {
		t.Fatal(err)
	}
	sub := &fakeSubmitter{}
	sched := NewScheduler(store, sub, 1)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = sched.FireTrigger(def.TriggerID, "schedule", nil, false, nil) }()
	}
	wg.Wait()
	if len(sub.messages) != 1 {
		t.Fatalf("concurrent fire delivered %d messages, want 1", len(sub.messages))
	}
}

func TestP0ClaimDeliverySaveFailureDoesNotLeaveMemoryGuard(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "triggers.json"), 20)
	if err != nil {
		t.Fatal(err)
	}
	def, err := NewDefinitionFromCreate(CreateInput{Name: "job", TargetAgentID: "agent-1", TaskTemplate: "run", Condition: map[string]any{"interval_seconds": 60}}, "agent-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateTrigger(def); err != nil {
		t.Fatal(err)
	}
	badParent := filepath.Join(t.TempDir(), "parent-file")
	if err := os.WriteFile(badParent, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	store.path = filepath.Join(badParent, "triggers.json")
	if err := store.ClaimDelivery(def.TriggerID, "delivery-1", "session-1"); err == nil {
		t.Fatal("expected durable claim failure")
	}
	got, _ := store.GetTrigger(def.TriggerID)
	if got.PendingDeliveryID != nil {
		t.Fatal("failed claim left a pending in-memory guard")
	}
}
