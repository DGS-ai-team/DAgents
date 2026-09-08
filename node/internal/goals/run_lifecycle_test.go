package goals

import (
	"testing"
	"time"
)

func TestObserveTurnBindsWaitingAndCompletesWithEvidence(t *testing.T) {
	s, _ := OpenStore("")
	now := time.Now().UTC()
	g, err := s.Create(CreateInput{Title: "goal", Objective: "do", Acceptance: "prove", AgentID: "agent"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.BindSession(g.ID, "goal-session", now); err != nil {
		t.Fatal(err)
	}
	r, err := s.StartRun(g.ID, "wake", now)
	if err != nil {
		t.Fatal(err)
	}
	waiting := TurnSnapshot{TurnID: "turn-1", TurnStatus: "waiting", StepStatus: "waiting_for_interaction"}
	if err = s.ObserveTurn("goal-session", waiting, now); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(g.ID)
	if got.Status != StatusWaiting {
		t.Fatalf("status=%s", got.Status)
	}
	cp := Checkpoint{Done: true, Evidence: []string{"artifact.txt"}, Summary: "done"}
	if _, err = s.Checkpoint(g.ID, r.ID, cp, now); err != nil {
		t.Fatal(err)
	}
	terminal := waiting
	terminal.TurnStatus = "completed"
	terminal.StepStatus = "completed"
	terminal.TotalTokens = 17
	if err = s.ObserveTurn("goal-session", terminal, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(g.ID)
	if got.Status != StatusCompleted || got.TokensUsed != 17 {
		t.Fatalf("goal=%+v", got)
	}
	if got.StatusReason != "" {
		t.Fatalf("stale approval reason remained after completion: %q", got.StatusReason)
	}
	// A repeated terminal snapshot must not charge tokens a second time.
	if err = s.ObserveTurn("goal-session", terminal, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(g.ID)
	if got.TokensUsed != 17 {
		t.Fatalf("repeated terminal charged tokens: %d", got.TokensUsed)
	}
}

func TestObserveTurnApprovalReasonClearedWhenContinuing(t *testing.T) {
	s, _ := OpenStore("")
	now := time.Now().UTC()
	g, err := s.Create(CreateInput{Objective: "do", Acceptance: "prove", AgentID: "agent"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.BindSession(g.ID, "goal-session", now); err != nil {
		t.Fatal(err)
	}
	r, err := s.StartRun(g.ID, "wake", now)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ObserveTurn("goal-session", TurnSnapshot{TurnID: "turn-wait", TurnStatus: "waiting", StepStatus: "waiting_for_interaction"}, now); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Checkpoint(g.ID, r.ID, Checkpoint{Summary: "progress"}, now); err != nil {
		t.Fatal(err)
	}
	if err = s.ObserveTurn("goal-session", TurnSnapshot{TurnID: "turn-wait", TurnStatus: "completed", StepStatus: "completed", TotalTokens: 3}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(g.ID)
	if got.Status != StatusWaiting || got.StatusReason != "" {
		t.Fatalf("approval reason not cleared for next wake: %+v", got)
	}
}

func TestObserveTurnFailureCannotCompleteFromDoneCheckpoint(t *testing.T) {
	s, _ := OpenStore("")
	now := time.Now().UTC()
	g, _ := s.Create(CreateInput{Objective: "do", Acceptance: "prove", AgentID: "agent"}, now)
	_, _ = s.BindSession(g.ID, "goal-session", now)
	r, _ := s.StartRun(g.ID, "wake", now)
	_, _ = s.Checkpoint(g.ID, r.ID, Checkpoint{Done: true, Evidence: []string{"evidence"}}, now)
	snapshot := TurnSnapshot{TurnID: "turn-1", TurnStatus: "failed", StepStatus: "failed", TotalTokens: 9}
	if err := s.ObserveTurn("goal-session", snapshot, now); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(g.ID)
	if got.Status == StatusCompleted {
		t.Fatalf("failed turn completed goal: %+v", got)
	}
}

func TestObserveTurnTerminalPreservesUserStateAndCharges(t *testing.T) {
	for _, tc := range []struct {
		name, initial, terminal, reason string
		usage                           int
		want                            Status
	}{
		{"paused completed", "paused", "completed", "", 7, StatusPaused},
		{"stopped completed", "stopped", "completed", "", 7, StatusStopped},
		{"stopped budget", "stopped", "budget_exhausted", "", 7, StatusStopped},
		{"paused unknown", "paused", "completed", "", 0, StatusPaused},
		{"stopped unknown", "stopped", "completed", "", 0, StatusStopped},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := OpenStore("")
			now := time.Now().UTC()
			g, err := s.Create(CreateInput{Objective: "do", Acceptance: "prove", AgentID: "agent"}, now)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = s.BindSession(g.ID, "non-empty-session", now)
			r, _ := s.StartRun(g.ID, "wake", now)
			_, _ = s.SetStatus(g.ID, Status(tc.initial), now)
			err = s.ObserveTurn("non-empty-session", TurnSnapshot{TurnID: "turn-" + tc.name, TurnStatus: tc.terminal, StepStatus: "completed", TotalTokens: tc.usage}, now.Add(time.Second))
			if err != nil {
				t.Fatal(err)
			}
			got, _ := s.Get(g.ID)
			if got.Status != tc.want || got.TokensUsed != int64(tc.usage) {
				t.Fatalf("goal=%+v run=%+v", got, s.Runs(g.ID)[0])
			}
			_ = r
		})
	}
}

func TestObserveTurnNoProgressPausesAfterTwoRuns(t *testing.T) {
	s, _ := OpenStore("")
	now := time.Now().UTC()
	g, _ := s.Create(CreateInput{Objective: "do", Acceptance: "prove", AgentID: "agent"}, now)
	_, _ = s.BindSession(g.ID, "session", now)
	for i := 0; i < 2; i++ {
		r, err := s.StartRun(g.ID, "wake", now.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		_, _ = s.Checkpoint(g.ID, r.ID, Checkpoint{Summary: "same"}, now)
		if err := s.ObserveTurn("session", TurnSnapshot{TurnID: "turn-np-" + string(rune('a'+i)), TurnStatus: "completed", StepStatus: "completed", TotalTokens: 1}, now); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := s.Get(g.ID)
	if got.Status != StatusPaused || got.StatusReason != "no_progress" {
		t.Fatalf("goal=%+v", got)
	}
}

func TestObserveTurnDifferentCheckpointContinues(t *testing.T) {
	s, _ := OpenStore("")
	now := time.Now().UTC()
	g, _ := s.Create(CreateInput{Objective: "do", Acceptance: "prove", AgentID: "agent"}, now)
	_, _ = s.BindSession(g.ID, "session", now)
	for i, summary := range []string{"one", "two"} {
		r, _ := s.StartRun(g.ID, "wake", now)
		_, _ = s.Checkpoint(g.ID, r.ID, Checkpoint{Summary: summary}, now)
		_ = s.ObserveTurn("session", TurnSnapshot{TurnID: string(rune('x' + i)), TurnStatus: "completed", StepStatus: "completed", TotalTokens: 1}, now)
	}
	got, _ := s.Get(g.ID)
	if got.Status != StatusWaiting {
		t.Fatalf("goal=%+v", got)
	}
}
