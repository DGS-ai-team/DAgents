package goals

import (
	"path/filepath"
	"testing"
	"time"
)

func realManagedGoal(t *testing.T, now time.Time) (*Store, Goal) {
	t.Helper()
	s, err := OpenStore(filepath.Join(t.TempDir(), "goals.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveProfile(AutoProfile{AgentID: "a", Enabled: true}, 0, now); err != nil {
		t.Fatal(err)
	}
	g, err := s.CreateManagedCycle(CreateInput{AgentID: "a", Objective: "do", Acceptance: "done", MaxRuns: 4, MinWakeIntervalSeconds: 60}, "cycle-1", 1, now, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.BindSession(g.ID, "session", now); err != nil {
		t.Fatal(err)
	}
	return s, g
}

func TestObserveTurnRealStartCheckpointCompletedNone(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	s, g := realManagedGoal(t, now)
	r, err := s.StartRun(g.ID, "wake", now)
	if err != nil {
		t.Fatal(err)
	}
	d := FinalDecision{Outcome: OutcomeCompleted, Summary: "finished", Reason: "accepted", NextAction: NextNone}
	if _, err = s.Checkpoint(g.ID, r.ID, Checkpoint{Done: false, Summary: "finished", Decision: &d}, now); err != nil {
		t.Fatal(err)
	}
	if err = s.ObserveTurn("session", TurnSnapshot{TurnID: "turn-1", TurnStatus: "completed", StepStatus: "completed", TotalTokens: 11}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(g.ID)
	if got.Status != StatusCompleted || got.TokensUsed != 11 {
		t.Fatalf("goal=%+v", got)
	}
	runs := s.Runs(g.ID)
	if len(runs) != 1 || runs[0].Checkpoint == nil || runs[0].Checkpoint.Decision == nil {
		t.Fatalf("run=%+v", runs)
	}
}

func TestObserveTurnConfigChangeOnlyAccountsOldRun(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	s, g := realManagedGoal(t, now)
	r, err := s.StartRun(g.ID, "wake", now)
	if err != nil {
		t.Fatal(err)
	}
	old, _ := s.Get(g.ID)
	if _, err = s.UpdateAutonomyIntent(g.ID, "changed", "done", nil, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err = s.ObserveTurn("session", TurnSnapshot{TurnID: "turn-old", TurnStatus: "completed", StepStatus: "completed", TotalTokens: 3}, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(g.ID)
	if got.Status == StatusCompleted || got.Objective != "changed" {
		t.Fatalf("old callback changed config/goal: %+v", got)
	}
	if got.TokensUsed != old.TokensUsed+3 {
		t.Fatalf("old callback did not charge goal accounting: %+v", got)
	}
	if s.Runs(g.ID)[0].TokensUsed != 3 {
		t.Fatalf("run not accounted: %+v", s.Runs(g.ID)[0])
	}
	_ = r
}

func TestObserveTurnCompletedZeroTokensPausesUnknown(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	s, g := realManagedGoal(t, now)
	r, err := s.StartRun(g.ID, "wake", now)
	if err != nil {
		t.Fatal(err)
	}
	d := FinalDecision{Outcome: OutcomeCompleted, Summary: "finished", Reason: "accepted", NextAction: NextNone}
	if _, err = s.Checkpoint(g.ID, r.ID, Checkpoint{Decision: &d}, now); err != nil {
		t.Fatal(err)
	}
	if err = s.ObserveTurn("session", TurnSnapshot{TurnID: "turn-zero", TurnStatus: "completed", StepStatus: "completed"}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(g.ID)
	if got.Status != StatusPaused || got.StatusReason != "usage_unknown" {
		t.Fatalf("goal=%+v", got)
	}
	if s.Runs(g.ID)[0].Status != "unknown" {
		t.Fatalf("run=%+v", s.Runs(g.ID)[0])
	}
}

func TestObserveTurnFailedPreservesFailureReason(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	s, g := realManagedGoal(t, now)
	r, err := s.StartRun(g.ID, "wake", now)
	if err != nil {
		t.Fatal(err)
	}
	due := now.Add(5 * time.Minute)
	d := FinalDecision{Outcome: OutcomeProgress, Summary: "attempted", Reason: "retry later", ExpectedProgress: "retry", NextAction: NextAt, NextWakeAt: &due}
	if _, err = s.Checkpoint(g.ID, r.ID, Checkpoint{Decision: &d}, now); err != nil {
		t.Fatal(err)
	}
	if err = s.ObserveTurn("session", TurnSnapshot{TurnID: "turn-failed", TurnStatus: "failed", StepStatus: "failed", TurnEndReason: "tool_failed", TotalTokens: 2}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	runs := s.Runs(g.ID)
	if runs[0].Status != "failed" || runs[0].Reason != "tool_failed" {
		t.Fatalf("run=%+v", runs[0])
	}
	if _, err = s.GetScheduleIntent(g.ID, "goal"); err == nil {
		t.Fatal("failed turn created intent")
	}
}

func TestObserveTurnApprovalCompletionClearsReason(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	s, g := realManagedGoal(t, now)
	r, err := s.StartRun(g.ID, "wake", now)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ObserveTurn("session", TurnSnapshot{TurnID: "turn-approval", TurnStatus: "waiting", StepStatus: "waiting_for_interaction"}, now); err != nil {
		t.Fatal(err)
	}
	due := now.Add(5 * time.Minute)
	d := FinalDecision{Outcome: OutcomeProgress, Summary: "continued", Reason: "approval received", ExpectedProgress: "next check", NextAction: NextAt, NextWakeAt: &due}
	if _, err = s.Checkpoint(g.ID, r.ID, Checkpoint{Decision: &d}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err = s.ObserveTurn("session", TurnSnapshot{TurnID: "turn-approval", TurnStatus: "completed", StepStatus: "completed", TotalTokens: 2}, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(g.ID)
	if got.Status != StatusWaiting || got.StatusReason != "" {
		t.Fatalf("goal=%+v", got)
	}
}

func TestLateOldCycleStillAccountsAfterNewCycle(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	s, oldGoal := realManagedGoal(t, now)
	oldRun, err := s.StartRun(oldGoal.ID, "wake", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetStatus(oldGoal.ID, StatusStopped, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	p, ok := s.GetProfile("a")
	if !ok {
		t.Fatal("profile missing")
	}
	newGoal, err := s.CreateManagedCycle(CreateInput{AgentID: "a", Objective: "new", Acceptance: "new done", MaxRuns: 4, MinWakeIntervalSeconds: 60}, "cycle-2", p.Revision, now.Add(2*time.Second), true)
	if err != nil {
		t.Fatal(err)
	}
	beforeNew, _ := s.Get(newGoal.ID)
	if err = s.ObserveTurn("session", TurnSnapshot{TurnID: "late-old", TurnStatus: "completed", StepStatus: "completed", TotalTokens: 7}, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	gotOld, _ := s.Get(oldGoal.ID)
	gotNew, _ := s.Get(newGoal.ID)
	if gotOld.TokensUsed != 7 || gotNew.TokensUsed != beforeNew.TokensUsed || gotNew.Status != beforeNew.Status {
		t.Fatalf("old=%+v new=%+v", gotOld, gotNew)
	}
	u, _ := s.GetUsage("a")
	if u.BusinessTokens != 7 {
		t.Fatalf("usage=%+v", u)
	}
	if oldRun.ID == "" {
		t.Fatal("run missing")
	}
}
