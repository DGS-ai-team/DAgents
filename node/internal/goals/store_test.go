package goals

import (
	"testing"
	"time"
)

func TestGoalLifecycleAndCheckpoint(t *testing.T) {
	s, err := OpenStore(t.TempDir() + "/goals.json")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	g, err := s.Create(CreateInput{Objective: "observe", Acceptance: "evidence", AgentID: "a"}, now)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.StartRun(g.ID, "manual", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Checkpoint(g.ID, r.ID, Checkpoint{Summary: "x", NextSteps: []string{"y"}}, now); err != nil {
		t.Fatal(err)
	}
	r.Status = "completed"
	r.TokensUsed = 10
	if _, err := s.FinishRun(g.ID, r, now); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(g.ID)
	if got.Status != StatusWaiting || got.TokensUsed != 10 || got.LastCheckpoint == nil {
		t.Fatalf("goal=%+v", got)
	}
	if _, err := s.FinishRun(g.ID, r, now); err != nil {
		t.Fatal("idempotent finish: ", err)
	}
}

func TestRestartFailsClosed(t *testing.T) {
	p := t.TempDir() + "/goals.json"
	s, _ := OpenStore(p)
	g, _ := s.Create(CreateInput{Objective: "x", Acceptance: "y", AgentID: "a"}, time.Now())
	_, _ = s.StartRun(g.ID, "wake", time.Now())
	s2, err := OpenStore(p)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s2.Get(g.ID)
	if got.Status != StatusPaused {
		t.Fatalf("status=%s", got.Status)
	}
	if rs := s2.Runs(g.ID); len(rs) != 1 || rs[0].Status != "unknown" {
		t.Fatalf("runs=%+v", rs)
	}
}

func TestCreateRejectsOversizedBudgets(t *testing.T) {
	s, _ := OpenStore("")
	if _, err := s.Create(CreateInput{Objective: "x", Acceptance: "y", AgentID: "a", MaxRuns: 1001}, time.Now()); err == nil {
		t.Fatal("expected max_runs rejection")
	}
}

func TestCreateRejectsNegativeLimits(t *testing.T) {
	s, _ := OpenStore("")
	for _, in := range []CreateInput{
		{Objective: "x", Acceptance: "y", AgentID: "a", MaxRuns: -1},
		{Objective: "x", Acceptance: "y", AgentID: "a", TokenBudget: -1},
		{Objective: "x", Acceptance: "y", AgentID: "a", TurnTokenBudget: -1},
	} {
		if _, err := s.Create(in, time.Now()); err == nil {
			t.Fatal("expected negative limit rejection")
		}
	}
}

func TestCreateManagedDisabledPersistsPaused(t *testing.T) {
	s, _ := OpenStore("")
	g, err := s.CreateManaged(CreateInput{Objective: "x", Acceptance: "y", AgentID: "a"}, time.Now(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !g.Managed || g.Status != StatusPaused {
		t.Fatalf("goal=%+v", g)
	}
}

func TestUpdateConfigurationAndStatusIsAtomicOnInvalidStatus(t *testing.T) {
	s, _ := OpenStore("")
	now := time.Now()
	g, _ := s.CreateManaged(CreateInput{Objective: "x", Acceptance: "y", AgentID: "a", MaxRuns: 1}, now, false)
	if _, err := s.UpdateConfigurationAndStatus(g.ID, CreateInput{Objective: "changed", Acceptance: "z"}, func() *Status { v := StatusActive; return &v }(), now); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(g.ID)
	if got.Objective != "changed" || got.Status != StatusActive || got.Runs != 0 {
		t.Fatalf("goal=%+v", got)
	}
}

func TestCheckpointRejectsTooSoonWake(t *testing.T) {
	s, _ := OpenStore("")
	g, _ := s.Create(CreateInput{Objective: "x", Acceptance: "y", AgentID: "a"}, time.Now())
	r, _ := s.StartRun(g.ID, "x", time.Now())
	n := time.Now().Add(time.Minute)
	cp := Checkpoint{Summary: "progress", NextWakeAt: &n}
	if _, err := s.Checkpoint(g.ID, r.ID, cp, time.Now()); err == nil {
		t.Fatal("expected min wake rejection")
	}
}
func TestStoppedGoalCannotBeRevivedByLateTerminal(t *testing.T) {
	s, _ := OpenStore("")
	now := time.Now()
	g, _ := s.Create(CreateInput{Objective: "x", Acceptance: "y", AgentID: "a"}, now)
	r, _ := s.StartRun(g.ID, "x", now)
	_, _ = s.SetStatus(g.ID, StatusStopped, now)
	if err := s.ObserveTurn(g.SessionID, TurnSnapshot{TurnID: "t", TurnStatus: "completed", StepStatus: "completed"}, now); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(g.ID)
	if got.Status != StatusStopped {
		t.Fatalf("status=%s", got.Status)
	}
	_ = r
}

func TestSetStatusRejectsResumeAfterRunLimit(t *testing.T) {
	s, _ := OpenStore("")
	now := time.Now()
	g, err := s.Create(CreateInput{Objective: "x", Acceptance: "y", AgentID: "a", MaxRuns: 1}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.StartRun(g.ID, "x", now); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetStatus(g.ID, StatusPaused, now); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetStatus(g.ID, StatusActive, now); err == nil {
		t.Fatal("expected resume to reject exhausted run limit")
	}
}

func TestStartRunPausesWhenBudgetOrRunLimitExhausted(t *testing.T) {
	for _, in := range []CreateInput{
		{Objective: "x", Acceptance: "y", AgentID: "a", MaxRuns: 1},
		{Objective: "x", Acceptance: "y", AgentID: "a", TokenBudget: 1},
	} {
		s, _ := OpenStore("")
		now := time.Now()
		g, err := s.Create(in, now)
		if err != nil {
			t.Fatal(err)
		}
		if in.MaxRuns == 1 {
			if _, err = s.StartRun(g.ID, "first", now); err != nil {
				t.Fatal(err)
			}
		}
		if in.TokenBudget == 1 {
			g.TokensUsed = 1
			s.mu.Lock()
			s.data.Goals[g.ID] = g
			s.mu.Unlock()
		}
		if _, err = s.StartRun(g.ID, "next", now); err == nil {
			t.Fatal("expected exhausted start rejection")
		}
		got, _ := s.Get(g.ID)
		if got.Status != StatusPaused || got.StatusReason == "" {
			t.Fatalf("goal not paused: %+v", got)
		}
	}
}
