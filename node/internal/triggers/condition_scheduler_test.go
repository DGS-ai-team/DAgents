package triggers

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func conditionStringPtr(v string) *string { return &v }

func newConditionScheduleFixture(t *testing.T) (*Store, *Scheduler, *fakeSubmitter, Definition, time.Time) {
	t.Helper()
	now := time.Date(2024, time.May, 1, 10, 1, 0, 0, time.Local)
	store, err := OpenStore(filepath.Join(t.TempDir(), "triggers.json"), 20)
	if err != nil {
		t.Fatal(err)
	}
	def, err := NewDefinitionFromCreate(CreateInput{
		Name: "condition", TaskTemplate: "run", TargetAgentID: "agent-a",
		TargetSessionID: conditionStringPtr("session-a"),
		Condition:       map[string]any{"schedule": map[string]any{"kind": "daily", "hour": 10, "minute": 0}, "cmd": "check"},
	}, "agent-a", now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	v := timeToUnixFloat(now.Add(-time.Minute))
	def.NextFireAt = &v
	if _, err = store.CreateTrigger(def); err != nil {
		t.Fatal(err)
	}
	sub := &fakeSubmitter{}
	return store, NewScheduler(store, sub, 5), sub, def, now
}

func TestConditionRunnerOutcomesClearClaimAndAdvance(t *testing.T) {
	cases := []struct {
		name       string
		runner     ConditionRunner
		wantStatus FireStatus
	}{
		{"false", func(context.Context, string, string) (bool, error) { return false, nil }, FireStatusSkipped},
		{"error", func(context.Context, string, string) (bool, error) { return false, errors.New("condition unavailable") }, FireStatusError},
		{"nil", nil, FireStatusError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, scheduler, sub, def, now := newConditionScheduleFixture(t)
			scheduler.SetConditionRunner(tc.runner)
			record, err := scheduler.FireTrigger(def.TriggerID, "schedule", nil, false, nil)
			if err != nil {
				t.Fatal(err)
			}
			if record.Status != tc.wantStatus {
				t.Fatalf("status=%s want %s (%s)", record.Status, tc.wantStatus, record.Message)
			}
			if len(sub.messages) != 0 {
				t.Fatalf("condition outcome submitted %d messages", len(sub.messages))
			}
			got, ok := store.GetTrigger(def.TriggerID)
			if !ok || got.PendingDeliveryID != nil {
				t.Fatalf("pending claim leaked: %+v", got)
			}
			if got.NextFireAt == nil || *got.NextFireAt == *def.NextFireAt || unixFloatToTime(*got.NextFireAt).Before(now) {
				t.Fatalf("scheduled occurrence was not advanced: %+v", got.NextFireAt)
			}
		})
	}
}

func TestConditionRunnerIsFencedByRevisionAndOwner(t *testing.T) {
	store, scheduler, _, def, now := newConditionScheduleFixture(t)
	called := 0
	scheduler.SetConditionRunner(func(context.Context, string, string) (bool, error) { called++; return true, nil })
	// Updating the condition changes the durable revision and occurrence. A
	// previously captured definition must fail the durable claim before runner.
	updated, err := store.UpdateTrigger(def.TriggerID, UpdatePatch{TaskTemplate: conditionStringPtr("new")}, now)
	if err != nil {
		t.Fatal(err)
	}
	updated.NextFireAt = def.NextFireAt
	if err := store.ReplaceTrigger(updated); err != nil {
		t.Fatal(err)
	}
	old := scheduler.fire(context.Background(), def, "schedule", nil, false, nil)
	if old.Status != FireStatusError || called != 0 {
		t.Fatalf("stale fire=%+v runner calls=%d", old, called)
	}
	// An owner-mismatched authorized fire is rejected before the condition seam.
	record, err := scheduler.FireAuthorized(Principal{Kind: "agent", ID: "other", AgentID: "other"}, updated.TriggerID, updated.Revision, "manual", nil, false, &FireOptions{FixedSessionID: "session-a"})
	if err == nil || record.Status != "" || called != 0 {
		t.Fatalf("cross-owner fire record=%+v err=%v calls=%d", record, err, called)
	}
}

func TestConditionRunnerDoesNotRunExpiredOccurrence(t *testing.T) {
	store, scheduler, _, def, now := newConditionScheduleFixture(t)
	called := 0
	scheduler.SetConditionRunner(func(context.Context, string, string) (bool, error) { called++; return true, nil })
	// Fire with a stale scheduled occurrence directly; ClaimDeliveryForOccurrence
	// must fence it before any condition command executes.
	stale := def
	v := timeToUnixFloat(now.Add(-2 * time.Hour))
	stale.NextFireAt = &v
	record := scheduler.fire(context.Background(), stale, "schedule", nil, false, nil)
	if record.Status != FireStatusError || called != 0 {
		t.Fatalf("expired occurrence fire=%+v calls=%d", record, called)
	}
	if got, _ := store.GetTrigger(def.TriggerID); got.PendingDeliveryID != nil {
		t.Fatal("expired claim left pending delivery")
	}
}

func TestConditionCleanupPersistenceFailureIsReturned(t *testing.T) {
	store, scheduler, _, def, _ := newConditionScheduleFixture(t)
	scheduler.SetConditionRunner(func(context.Context, string, string) (bool, error) {
		// The durable claim has already succeeded. Make only cleanup fail.
		store.path = t.TempDir()
		return false, nil
	})
	record, err := scheduler.FireTrigger(def.TriggerID, "schedule", nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != FireStatusError || !strings.Contains(record.Message, "condition cleanup failed") {
		t.Fatalf("record=%+v", record)
	}
	got, _ := store.GetTrigger(def.TriggerID)
	if got.PendingDeliveryID == nil {
		t.Fatal("failed cleanup must retain durable pending claim")
	}
}

func TestConditionGateClaimsOccurrenceBeforeConcurrentFire(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "triggers.json"), 20)
	if err != nil {
		t.Fatal(err)
	}
	def, err := NewDefinitionFromCreate(CreateInput{Name: "gated", TaskTemplate: "run", TargetAgentID: "agent-a", Condition: map[string]any{"interval_seconds": 60, "cmd": "gate"}}, "agent-a", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTrigger(def); err != nil {
		t.Fatal(err)
	}
	s := NewScheduler(store, &fakeSubmitter{}, 5)
	var mu sync.Mutex
	calls := 0
	entered := make(chan struct{})
	release := make(chan struct{})
	s.SetConditionRunner(func(context.Context, string, string) (bool, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			close(entered)
			<-release
		}
		return true, nil
	})
	done := make(chan FireRecord, 1)
	go func() { r, _ := s.FireTrigger(def.TriggerID, "manual", nil, false, nil); done <- r }()
	<-entered
	second, _ := s.FireTrigger(def.TriggerID, "manual", nil, false, nil)
	if second.Status != FireStatusSkipped {
		t.Fatalf("second status=%s", second.Status)
	}
	close(release)
	<-done
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("condition calls=%d, want 1", calls)
	}
}
