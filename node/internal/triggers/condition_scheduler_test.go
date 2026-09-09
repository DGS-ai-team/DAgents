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
		{"false", func(context.Context, ConditionRequest) (ConditionResult, error) {
			return ConditionResult{Status: ConditionNotMatched}, nil
		}, FireStatusSkipped},
		{"error", func(context.Context, ConditionRequest) (ConditionResult, error) {
			return ConditionResult{}, errors.New("condition unavailable")
		}, FireStatusError},
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

func TestTypedConditionApprovalPersistsClaimAndCompletesOnce(t *testing.T) {
	store, scheduler, sub, def, _ := newConditionScheduleFixture(t)
	scheduler.SetConditionRunner(func(_ context.Context, req ConditionRequest) (ConditionResult, error) {
		if req.TriggerID != def.TriggerID || req.AgentID != def.TargetAgentID || req.DeliveryID == "" || req.SessionID != "session-a" || req.Command != "check" || req.Occurrence == nil {
			t.Fatalf("bad typed request: %+v", req)
		}
		return ConditionResult{Status: ConditionAwaitingApproval}, nil
	})
	first, err := scheduler.FireTrigger(def.TriggerID, "schedule", nil, false, nil)
	if err != nil || first.Status != FireStatusAwaitingApproval {
		t.Fatalf("awaiting result=%+v err=%v", first, err)
	}
	pending, ok := store.GetTrigger(def.TriggerID)
	if !ok || pending.PendingDeliveryID == nil || pending.PendingOccurrence == nil {
		t.Fatalf("approval claim was not persisted: %+v", pending)
	}
	req := ConditionCompletion{TriggerID: def.TriggerID, DeliveryID: *pending.PendingDeliveryID, SessionID: *pending.PendingSessionID, AgentID: def.TargetAgentID, Revision: pending.Revision, Occurrence: pending.PendingOccurrence, Matched: true}
	if _, err := scheduler.CompleteCondition(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if len(sub.messages) != 1 {
		t.Fatalf("submitted messages=%d want 1", len(sub.messages))
	}
	if _, err := scheduler.CompleteCondition(context.Background(), req); err == nil {
		t.Fatal("duplicate completion succeeded")
	}
	after, _ := store.GetTrigger(def.TriggerID)
	if after.PendingDeliveryID == nil || !after.PendingConditionApproved {
		t.Fatalf("completion did not retain delivery claim: %+v", after)
	}
}

func awaitingCondition(t *testing.T, reason string, payload map[string]any) (*Store, *Scheduler, *fakeSubmitter, Definition, *ConditionCompletion, FireRecord) {
	t.Helper()
	store, scheduler, sub, def, _ := newConditionScheduleFixture(t)
	scheduler.SetConditionRunner(func(_ context.Context, req ConditionRequest) (ConditionResult, error) {
		return ConditionResult{Status: ConditionAwaitingApproval}, nil
	})
	record, err := scheduler.FireTrigger(def.TriggerID, reason, payload, false, nil)
	if err != nil || record.Status != FireStatusAwaitingApproval {
		t.Fatalf("awaiting fire=%+v err=%v", record, err)
	}
	pending, ok := store.GetTrigger(def.TriggerID)
	if !ok || pending.PendingDeliveryID == nil || pending.PendingSessionID == nil {
		t.Fatalf("missing pending condition: %+v", pending)
	}
	return store, scheduler, sub, def, &ConditionCompletion{TriggerID: def.TriggerID, DeliveryID: *pending.PendingDeliveryID, SessionID: *pending.PendingSessionID, AgentID: def.TargetAgentID, Revision: pending.Revision, Occurrence: pending.PendingOccurrence, Matched: true}, record
}

func TestConditionCompletionConcurrentSubmitsOnce(t *testing.T) {
	store, scheduler, sub, _, req, _ := awaitingCondition(t, "manual", map[string]any{"value": "original"})
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := scheduler.CompleteCondition(context.Background(), *req)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 || len(sub.messages) != 1 {
		t.Fatalf("successes=%d submitted=%d", successes, len(sub.messages))
	}
	if got, _ := store.GetTrigger(req.TriggerID); got.PendingDeliveryID == nil {
		t.Fatal("completion cleared pending claim before consumer acknowledgement")
	}
}

func TestManualConditionCompletionAllowsNilOccurrenceAndRetainsPayloadContent(t *testing.T) {
	store, scheduler, sub, _, req, awaiting := awaitingCondition(t, "manual", map[string]any{"value": "original"})
	if req.Occurrence != nil {
		t.Fatalf("manual occurrence=%v want nil", req.Occurrence)
	}
	completed, err := scheduler.CompleteCondition(context.Background(), *req)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Content != awaiting.Content || len(sub.messages) != 1 {
		t.Fatalf("content=%q awaiting=%q messages=%d", completed.Content, awaiting.Content, len(sub.messages))
	}
	got, _ := store.GetTrigger(req.TriggerID)
	if got.PendingConditionPayload["value"] != "original" {
		t.Fatalf("payload not persisted: %+v", got.PendingConditionPayload)
	}
}

func TestConditionCompletionAfterReopenRequiresRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "triggers.json")
	store, err := OpenStore(path, 20)
	if err != nil {
		t.Fatal(err)
	}
	def, err := NewDefinitionFromCreate(CreateInput{Name: "condition", TaskTemplate: "run", TargetAgentID: "agent-a", TargetSessionID: conditionStringPtr("session-a"), Condition: map[string]any{"interval_seconds": 60, "cmd": "check"}}, "agent-a", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateTrigger(def); err != nil {
		t.Fatal(err)
	}
	scheduler := NewScheduler(store, &fakeSubmitter{}, 5)
	scheduler.SetConditionRunner(func(context.Context, ConditionRequest) (ConditionResult, error) {
		return ConditionResult{Status: ConditionAwaitingApproval}, nil
	})
	awaiting, err := scheduler.FireTrigger(def.TriggerID, "manual", map[string]any{"x": "y"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	pending, _ := store.GetTrigger(def.TriggerID)
	req := ConditionCompletion{TriggerID: def.TriggerID, DeliveryID: *pending.PendingDeliveryID, SessionID: *pending.PendingSessionID, AgentID: def.TargetAgentID, Revision: pending.Revision, Occurrence: pending.PendingOccurrence, Matched: true}
	reopened, err := OpenStore(path, 20)
	if err != nil {
		t.Fatal(err)
	}
	reopenedScheduler := NewScheduler(reopened, &fakeSubmitter{}, 5)
	if _, err := reopenedScheduler.CompleteCondition(context.Background(), req); err == nil || !strings.Contains(err.Error(), "identity conflict") {
		t.Fatalf("reopen completion err=%v awaiting=%+v", err, awaiting)
	}
	if got, _ := reopened.GetTrigger(def.TriggerID); got.PendingConditionContent == "" || got.PendingDeliveryID == nil {
		t.Fatalf("reopened content/claim lost: %+v", got)
	}
}

func TestRecoveryClearsConditionApprovalBeforeNextDelivery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "triggers.json")
	store, err := OpenStore(path, 20)
	if err != nil {
		t.Fatal(err)
	}
	def, err := NewDefinitionFromCreate(CreateInput{Name: "condition", TaskTemplate: "run", TargetAgentID: "agent-a", TargetSessionID: conditionStringPtr("session-a"), Condition: map[string]any{"interval_seconds": 60, "cmd": "check"}}, "agent-a", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateTrigger(def); err != nil {
		t.Fatal(err)
	}
	firstScheduler := NewScheduler(store, &fakeSubmitter{}, 5)
	firstScheduler.SetConditionRunner(func(context.Context, ConditionRequest) (ConditionResult, error) {
		return ConditionResult{Status: ConditionAwaitingApproval}, nil
	})
	if _, err = firstScheduler.FireTrigger(def.TriggerID, "manual", map[string]any{"old": true}, false, nil); err != nil {
		t.Fatal(err)
	}
	first, _ := store.GetTrigger(def.TriggerID)
	reopened, err := OpenStore(path, 20)
	if err != nil {
		t.Fatal(err)
	}
	if err = reopened.RecoverPendingDelivery(def.TriggerID, *first.PendingDeliveryID); err != nil {
		t.Fatal(err)
	}
	enabled := true
	recovered, _ := reopened.GetTrigger(def.TriggerID)
	if _, err = reopened.UpdateTrigger(def.TriggerID, UpdatePatch{Enabled: &enabled}, time.Now()); err != nil {
		t.Fatal(err)
	}
	nextScheduler := NewScheduler(reopened, &fakeSubmitter{}, 5)
	nextScheduler.SetConditionRunner(func(context.Context, ConditionRequest) (ConditionResult, error) {
		return ConditionResult{Status: ConditionAwaitingApproval}, nil
	})
	if _, err = nextScheduler.FireTrigger(def.TriggerID, "manual", nil, false, nil); err != nil {
		t.Fatal(err)
	}
	next, _ := reopened.GetTrigger(def.TriggerID)
	if next.PendingConditionApproved || len(next.PendingConditionPayload) != 0 || next.PendingConditionContent == "" || recovered.PendingConditionApproved {
		t.Fatalf("old approval metadata leaked: recovered=%+v next=%+v", recovered, next)
	}
}

func TestFalseConditionCompletionReleasesClaimForNextFire(t *testing.T) {
	store, scheduler, _, def, req, _ := awaitingCondition(t, "manual", nil)
	if req.Matched {
		req.Matched = false
	}
	if _, err := scheduler.CompleteCondition(context.Background(), *req); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.GetTrigger(def.TriggerID); got.PendingDeliveryID != nil || store.HasPendingDelivery(def.TriggerID) {
		t.Fatal("false completion retained claim")
	}
	calls := 0
	scheduler.SetConditionRunner(func(context.Context, ConditionRequest) (ConditionResult, error) {
		calls++
		return ConditionResult{Status: ConditionNotMatched}, nil
	})
	if _, err := scheduler.FireTrigger(def.TriggerID, "manual", nil, false, nil); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("next fire did not execute condition runner: %d", calls)
	}
}

func TestConditionCompletionRejectsStaleRevisionAndOrdinaryPending(t *testing.T) {
	store, scheduler, _, def, req, _ := awaitingCondition(t, "manual", nil)
	updated, err := store.UpdateTrigger(def.TriggerID, UpdatePatch{Name: conditionStringPtr("updated")}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	req.Revision = updated.Revision - 1
	if _, err := scheduler.CompleteCondition(context.Background(), *req); err == nil {
		t.Fatal("stale revision completion succeeded")
	}
	plain, err := NewDefinitionFromCreate(CreateInput{Name: "plain", TaskTemplate: "run", TargetAgentID: "agent-a", Condition: map[string]any{"interval_seconds": 60}}, "agent-a", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateTrigger(plain); err != nil {
		t.Fatal(err)
	}
	if err = store.ClaimDelivery(plain.TriggerID, "delivery", "session-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := scheduler.CompleteCondition(context.Background(), ConditionCompletion{TriggerID: plain.TriggerID, DeliveryID: "delivery", SessionID: "session-a", AgentID: "agent-a", Revision: plain.Revision, Matched: true}); err == nil {
		t.Fatal("ordinary pending accepted as condition")
	}
}

func TestConditionRunnerIsFencedByRevisionAndOwner(t *testing.T) {
	store, scheduler, _, def, now := newConditionScheduleFixture(t)
	called := 0
	scheduler.SetConditionRunner(func(context.Context, ConditionRequest) (ConditionResult, error) {
		called++
		return ConditionResult{Status: ConditionMatched}, nil
	})
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
	scheduler.SetConditionRunner(func(context.Context, ConditionRequest) (ConditionResult, error) {
		called++
		return ConditionResult{Status: ConditionMatched}, nil
	})
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
	scheduler.SetConditionRunner(func(context.Context, ConditionRequest) (ConditionResult, error) {
		// The durable claim has already succeeded. Make only cleanup fail.
		store.path = t.TempDir()
		return ConditionResult{Status: ConditionNotMatched}, nil
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
	s.SetConditionRunner(func(context.Context, ConditionRequest) (ConditionResult, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			close(entered)
			<-release
		}
		return ConditionResult{Status: ConditionMatched}, nil
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
