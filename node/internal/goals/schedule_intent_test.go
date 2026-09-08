package goals

import (
	"path/filepath"
	"testing"
	"time"
)

func intentFixture(t *testing.T) (*Store, Goal, Run, time.Time) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	s, err := OpenStore(filepath.Join(t.TempDir(), "goals.json"))
	if err != nil {
		t.Fatal(err)
	}
	p := AutoProfile{AgentID: "agent-a", Revision: 3, Enabled: true, CurrentGoalID: "g", UpdatedAt: now}
	g := Goal{ID: "g", Title: "g", Objective: "o", AgentID: "agent-a", Managed: true, Status: StatusActive, Revision: 1, MaxRuns: 3, TokenBudget: 100, CreatedAt: now, UpdatedAt: now}
	s.mu.Lock()
	// Keep the fixture's profile binding explicit and create an unfinished run.
	s.data.Goals["g"] = g
	s.data.Profiles["agent-a"] = p
	r := Run{ID: "run-1", GoalID: "g", Status: "running", StartedAt: now}
	s.data.Runs["g"] = []Run{r}
	_ = s.saveLocked()
	s.mu.Unlock()
	return s, g, r, now
}

func TestScheduleIntentGenerationRestartAndRevocation(t *testing.T) {
	s, _, _, now := intentFixture(t)
	d := validDecision(now)
	i := ScheduleIntent{ID: "i-1", AgentID: "agent-a", GoalID: "g", Purpose: "goal", Generation: 1, Decision: d, State: IntentPending, ProfileRevision: 3, UpdatedAt: now}
	if _, err := s.UpsertScheduleIntent(i); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertScheduleIntent(i); err != nil {
		t.Fatal(err)
	} // idempotent retry
	i.Generation = 0
	if _, err := s.UpsertScheduleIntent(i); err == nil {
		t.Fatal("stale generation accepted")
	}
	if err := s.RevokeScheduleIntent("g", "goal", 1); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetScheduleIntent("g", "goal")
	if err != nil || got.State != IntentRevoked {
		t.Fatalf("intent=%+v err=%v", got, err)
	}
	reopened, err := OpenStore(s.path)
	if err != nil {
		t.Fatal(err)
	}
	got, err = reopened.GetScheduleIntent("g", "goal")
	if err != nil || got.State != IntentRevoked {
		t.Fatalf("restarted intent=%+v err=%v", got, err)
	}
}

func TestFinalizeRunCommitsAndIsIdempotent(t *testing.T) {
	s, g, r, now := intentFixture(t)
	progress := validDecision(now)
	g.Revision = 1
	in := FinalizeInput{RunID: r.ID, ExpectedGoalRevision: g.Revision, ExpectedProfileRevision: 3, Decision: progress, ActualTokens: 4, Purpose: "goal", Generation: 1, Now: now}
	if err := s.FinalizeRun(in); err != nil {
		t.Fatal(err)
	}
	if err := s.FinalizeRun(in); err != nil {
		t.Fatal(err)
	}
	stored, _ := s.Get("g")
	if stored.Status != StatusActive {
		t.Fatalf("goal=%+v", stored)
	}
	if got, _ := s.GetScheduleIntent("g", "goal"); got.State != IntentPending {
		t.Fatalf("intent=%+v", got)
	}
}

func TestFinalizeRejectsPausedProfileAndStaleRevision(t *testing.T) {
	s, g, r, now := intentFixture(t)
	s.mu.Lock()
	p := s.data.Profiles[g.AgentID]
	p.Enabled = false
	s.data.Profiles[g.AgentID] = p
	s.mu.Unlock()
	if err := s.FinalizeRun(FinalizeInput{RunID: r.ID, ExpectedGoalRevision: g.Revision, ExpectedProfileRevision: 3, Decision: validDecision(now), ActualTokens: 1, Purpose: "goal", Generation: 1, Now: now}); err != nil {
		t.Fatalf("err=%v", err)
	}
	if _, err := s.GetScheduleIntent(g.ID, "goal"); err == nil {
		t.Fatal("disabled profile rebuilt intent")
	}
}

func TestFinalizeWriteFailureRollsBack(t *testing.T) {
	s, g, r, now := intentFixture(t)
	oldGoal, _ := s.Get(g.ID)
	s.path = filepath.Dir(s.path) // rename over an existing directory must fail
	if err := s.FinalizeRun(FinalizeInput{RunID: r.ID, ExpectedGoalRevision: g.Revision, ExpectedProfileRevision: 3, Decision: validDecision(now), ActualTokens: 1, Purpose: "goal", Generation: 1, Now: now}); err == nil {
		t.Fatal("expected persistence failure")
	}
	got, _ := s.Get(g.ID)
	if got.Status != oldGoal.Status || got.Revision != oldGoal.Revision {
		t.Fatalf("goal changed after rollback: before=%+v after=%+v", oldGoal, got)
	}
	if _, err := s.GetScheduleIntent(g.ID, "goal"); err == nil {
		t.Fatal("intent survived failed finalize")
	}
}

func TestFinalizeFenceDoesNotApplyStaleBusinessDecision(t *testing.T) {
	s, g, r, now := intentFixture(t)
	s.mu.Lock()
	p := s.data.Profiles[g.AgentID]
	p.Revision++
	s.data.Profiles[g.AgentID] = p
	s.mu.Unlock()
	d := validDecision(now)
	d.Outcome = OutcomeCompleted
	d.NextAction = NextNone
	d.ExpectedProgress = ""
	if err := s.FinalizeRun(FinalizeInput{RunID: r.ID, ExpectedGoalRevision: g.Revision, ExpectedProfileRevision: 3, Decision: d, ActualTokens: 2, Purpose: "goal", Generation: 1, Now: now}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(g.ID)
	if got.Status == StatusCompleted || got.TokensUsed != 2 {
		t.Fatalf("stale decision changed goal: %+v", got)
	}
}

func TestFinalizeInvalidDecisionStillAccountsAndRevokesIntent(t *testing.T) {
	s, g, r, now := intentFixture(t)
	old := ScheduleIntent{ID: "old", AgentID: g.AgentID, GoalID: g.ID, Purpose: "goal", Generation: 1, Decision: validDecision(now), State: IntentPending, ProfileRevision: 3}
	if _, err := s.UpsertScheduleIntent(old); err != nil {
		t.Fatal(err)
	}
	if err := s.FinalizeRun(FinalizeInput{RunID: r.ID, ExpectedGoalRevision: g.Revision, ExpectedProfileRevision: 3, Decision: FinalDecision{}, ActualTokens: 5, Purpose: "goal", Generation: 2, Now: now}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(g.ID)
	if got.TokensUsed != 5 {
		t.Fatalf("usage not accounted: %+v", got)
	}
	intent, _ := s.GetScheduleIntent(g.ID, "goal")
	if intent.State != IntentRevoked {
		t.Fatalf("intent not revoked: %+v", intent)
	}
	if len(s.Runs(g.ID)) != 1 || s.Runs(g.ID)[0].FinishedAt == nil {
		t.Fatal("run not finalized")
	}
}

func TestLateRunKeepsNewerScheduleIntent(t *testing.T) {
	s, g, r, now := intentFixture(t)
	newer := ScheduleIntent{ID: "new", AgentID: g.AgentID, GoalID: g.ID, Purpose: "goal", Generation: 2, Decision: validDecision(now), State: IntentPending, ProfileRevision: 3}
	if _, err := s.UpsertScheduleIntent(newer); err != nil {
		t.Fatal(err)
	}
	if err := s.FinalizeRun(FinalizeInput{RunID: r.ID, ExpectedGoalRevision: g.Revision, ExpectedProfileRevision: 3, Decision: validDecision(now), ActualTokens: 3, Purpose: "goal", Generation: 1, Now: now}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetScheduleIntent(g.ID, "goal")
	if got.Generation != 2 || got.State != IntentPending {
		t.Fatalf("late run replaced newer intent: %+v", got)
	}
}

func TestFencedInvalidDecisionDoesNotPauseNewGoal(t *testing.T) {
	s, g, r, now := intentFixture(t)
	s.mu.Lock()
	p := s.data.Profiles[g.AgentID]
	p.Revision++
	s.data.Profiles[g.AgentID] = p
	s.mu.Unlock()
	if err := s.FinalizeRun(FinalizeInput{RunID: r.ID, ExpectedGoalRevision: g.Revision, ExpectedProfileRevision: 3, Decision: FinalDecision{}, ActualTokens: 1, Purpose: "goal", Generation: 1, Now: now}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(g.ID)
	if got.Status != StatusActive {
		t.Fatalf("fenced callback changed goal: %+v", got)
	}
}

func TestOlderNoneDecisionDoesNotRevokeNewerIntent(t *testing.T) {
	s, g, r, now := intentFixture(t)
	newer := ScheduleIntent{ID: "new", AgentID: g.AgentID, GoalID: g.ID, Purpose: "goal", Generation: 2, Decision: validDecision(now), State: IntentPending, ProfileRevision: 3}
	if _, err := s.UpsertScheduleIntent(newer); err != nil {
		t.Fatal(err)
	}
	d := validDecision(now)
	d.NextAction = NextNone
	d.NextWakeAt = nil
	d.ExpectedProgress = ""
	if err := s.FinalizeRun(FinalizeInput{RunID: r.ID, ExpectedGoalRevision: g.Revision, ExpectedProfileRevision: 3, Decision: d, ActualTokens: 1, Purpose: "goal", Generation: 1, Now: now}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetScheduleIntent(g.ID, "goal")
	if got.Generation != 2 || got.State != IntentPending {
		t.Fatalf("newer intent revoked: %+v", got)
	}
}
