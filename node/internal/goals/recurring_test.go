package goals

import (
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func recurringFixture(t *testing.T, path string) (*Store, Goal, Run, RecurringCycleInput) {
	t.Helper()
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 9, 8, 0, 0, 0, time.UTC)
	expires := now.Add(24 * time.Hour)
	p := AutoProfile{AgentID: "auto", Revision: 3, Enabled: true, PlanMode: "recurring", Timezone: "UTC", WorkSchedule: "daily 09:00", CurrentGoalID: "old", AuthorizationRef: "auth", AuthorizationRevision: 7, BusinessTokenBudget: 100}
	g := Goal{ID: "old", AgentID: "auto", Managed: true, Status: StatusCompleted, SessionID: "session", Title: "title", Objective: "objective", Acceptance: "accept", Revision: 4, CycleSequence: 4, ExpiresAt: &expires, CreatedAt: now.Add(-time.Hour), UpdatedAt: now}
	finished := now.Add(-time.Minute)
	r := Run{ID: "run", GoalID: g.ID, SessionID: g.SessionID, Status: "completed", TokensUsed: 5, StartedAt: now.Add(-time.Hour), FinishedAt: &finished}
	s.mu.Lock()
	s.data.Profiles[p.AgentID], s.data.Goals[g.ID], s.data.Runs[g.ID] = p, g, []Run{r}
	if err := s.saveLocked(); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.mu.Unlock()
	in := RecurringCycleInput{AgentID: p.AgentID, PreviousGoalID: g.ID, CompletedRunID: r.ID, Occurrence: now.Add(time.Hour), CycleDuration: 2 * time.Hour, SessionID: g.SessionID, AuthorizationRef: p.AuthorizationRef, AuthorizationRevision: p.AuthorizationRevision, ExpectedProfileRevision: p.Revision, IdempotencyKey: "cycle-1", Now: now}
	return s, g, r, in
}

func TestRecurringCycleCommitProjectionRevisionAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "goals.json")
	s, old, _, in := recurringFixture(t, path)
	g, err := s.CreateNextRecurringCycle(in)
	if err != nil {
		t.Fatal(err)
	}
	if g.PreviousGoalID != old.ID || g.Origin != "recurring" || g.IdempotencyKey != in.IdempotencyKey || !g.ExpiresAt.After(*g.NextWakeAt) {
		t.Fatalf("new cycle=%+v", g)
	}
	p, _ := s.GetProfile(in.AgentID)
	intent, err := s.GetScheduleIntent(g.ID, "goal")
	if err != nil || intent.ProfileRevision != p.Revision || p.CurrentGoalID != g.ID {
		t.Fatalf("profile=%+v intent=%+v err=%v", p, intent, err)
	}
	if err := s.ConfirmProjected(g.ID, "goal", intent.Generation, intent.Fingerprint); err != nil {
		t.Fatalf("projection fence: %v", err)
	}
	reloaded, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.Get(g.ID)
	if !ok || got.IdempotencyKey != in.IdempotencyKey {
		t.Fatalf("restart goal=%+v", got)
	}
	history, _ := reloaded.Get(old.ID)
	if !reflect.DeepEqual(history, old) {
		t.Fatalf("old history changed: before=%+v after=%+v", old, history)
	}
}

func TestRecurringCycleRetryPrecedesBindingFence(t *testing.T) {
	s, _, _, in := recurringFixture(t, "")
	g, err := s.CreateNextRecurringCycle(in)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.GetProfile(in.AgentID)
	p.Enabled = false
	if _, err = s.SaveProfile(p, p.Revision, in.Now); err != nil {
		t.Fatal(err)
	}
	in.Now = in.Now.Add(3 * time.Hour)
	retry, err := s.CreateNextRecurringCycle(in)
	if err != nil || retry.ID != g.ID {
		t.Fatalf("retry=%+v err=%v", retry, err)
	}
	in.IdempotencyKey = "cycle-2"
	if _, err = s.CreateNextRecurringCycle(in); err == nil {
		t.Fatal("changed idempotency payload accepted")
	}
}

func TestRecurringCycleRejectsUnknownRunWrongSessionAndBudget(t *testing.T) {
	for name, mutate := range map[string]func(*Store){
		"unknown": func(s *Store) { s.data.Runs["old"][0].Status = "unknown" },
		"budget":  func(s *Store) { u := s.data.Usage["auto"]; u.BusinessTokens = 100; s.data.Usage["auto"] = u },
	} {
		t.Run(name, func(t *testing.T) {
			s, _, _, in := recurringFixture(t, "")
			s.mu.Lock()
			mutate(s)
			s.mu.Unlock()
			if _, err := s.CreateNextRecurringCycle(in); err == nil {
				t.Fatal("ineligible cycle accepted")
			}
		})
	}
	t.Run("wrong session", func(t *testing.T) {
		s, _, _, in := recurringFixture(t, "")
		in.SessionID = "foreign"
		if _, err := s.CreateNextRecurringCycle(in); err == nil {
			t.Fatal("foreign session accepted")
		}
	})
}

func TestRecurringCycleRequiresAuthorizationCASAndFutureExpiry(t *testing.T) {
	s, old, _, in := recurringFixture(t, "")
	in.ExpectedProfileRevision++
	if _, err := s.CreateNextRecurringCycle(in); err == nil {
		t.Fatal("stale profile revision accepted")
	}
	in.ExpectedProfileRevision--
	s.mu.Lock()
	p := s.data.Profiles[in.AgentID]
	p.AuthorizationRevision++
	s.data.Profiles[in.AgentID] = p
	s.mu.Unlock()
	if _, err := s.CreateNextRecurringCycle(in); err == nil {
		t.Fatal("stale authorization accepted")
	}
	s.mu.Lock()
	p = s.data.Profiles[in.AgentID]
	p.AuthorizationRevision = in.AuthorizationRevision
	s.data.Profiles[in.AgentID] = p
	g := s.data.Goals[old.ID]
	expired := in.Now
	g.ExpiresAt = &expired
	s.data.Goals[old.ID] = g
	s.mu.Unlock()
	next, err := s.CreateNextRecurringCycle(in)
	if err != nil || !next.ExpiresAt.After(*next.NextWakeAt) {
		t.Fatalf("cycle expiry=%+v err=%v", next, err)
	}
}

func TestRecurringCycleConcurrentRetryOnlyOneGoal(t *testing.T) {
	s, _, _, in := recurringFixture(t, "")
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.CreateNextRecurringCycle(in); results <- err }()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent retry failed: %v", err)
		}
	}
	if len(s.List()) != 2 {
		t.Fatalf("goals=%d", len(s.List()))
	}
}

func TestRecurringCycleWriteFailureRollsBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "goals.json")
	s, old, _, in := recurringFixture(t, path)
	before, _ := s.GetProfile(in.AgentID)
	s.mu.RLock()
	beforeGoals, beforeRuns, beforeUsage, beforeIntents := cloneGoalsMap(s.data.Goals), cloneRunsMap(s.data.Runs), cloneUsageMap(s.data.Usage), cloneIntentsMap(s.data.ScheduleIntents)
	s.mu.RUnlock()
	// Replace the file path with its containing directory after the fixture save.
	s.path = dir
	if _, err := s.CreateNextRecurringCycle(in); err == nil {
		t.Fatal("write failure accepted")
	}
	after, _ := s.GetProfile(in.AgentID)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("profile changed after rollback: before=%+v after=%+v", before, after)
	}
	s.mu.RLock()
	if !reflect.DeepEqual(beforeGoals, s.data.Goals) || !reflect.DeepEqual(beforeRuns, s.data.Runs) || !reflect.DeepEqual(beforeUsage, s.data.Usage) || !reflect.DeepEqual(beforeIntents, s.data.ScheduleIntents) {
		t.Fatal("failed save changed durable snapshot")
	}
	s.mu.RUnlock()
	if _, ok := s.Get(old.ID + "-missing"); ok {
		t.Fatal("unexpected goal")
	}
	if len(s.List()) != 1 {
		t.Fatal("new goal survived failed save")
	}
}
