package goals

import "testing"
import "time"

func TestCheckpointFinalizeCreatesRegisteredEventIntent(t *testing.T) {
	now := time.Now().UTC()
	s, _ := OpenStore("")
	p, err := s.SaveProfile(AutoProfile{AgentID: "auto", Enabled: true, Revision: 1, UpdatedAt: now}, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.CreateManagedCycle(CreateInput{AgentID: "auto", Managed: true, Objective: "o", Acceptance: "a"}, "cycle", p.Revision, now, true)
	if err != nil {
		t.Fatal(err)
	}
	s.SetRegisteredSources(map[string]map[string]bool{"auto": {"files": true}})
	var ok bool
	p, ok = s.GetProfile("auto")
	if !ok {
		t.Fatal("profile missing")
	}
	r, err := s.StartRun(g.ID, "schedule", now)
	if err != nil {
		t.Fatal(err)
	}
	d := FinalDecision{Outcome: OutcomeProgress, Summary: "wait", Reason: "watch", ExpectedProgress: "change", NextAction: NextEvent, Event: &EventSpec{SourceID: "files", Filter: map[string]any{"equals": map[string]any{"files": 1}}}}
	if _, err = s.Checkpoint(g.ID, r.ID, Checkpoint{Summary: "observed", Done: true, Decision: &d}, now); err != nil {
		t.Fatal(err)
	}
	r.Status = "completed"
	if err = s.FinalizeRun(FinalizeInput{RunID: r.ID, ExpectedGoalRevision: g.Revision, ExpectedProfileRevision: p.Revision, ExpectedConfigRevision: g.ConfigRevision, Decision: d, ActualTokens: 1, Purpose: "goal", Generation: r.Generation, TerminalStatus: "completed", Now: now, RegisteredSource: map[string]bool{"files": true}}); err != nil {
		t.Fatal(err)
	}
	i, err := s.GetScheduleIntent(g.ID, "goal")
	if err != nil || i.Decision.NextAction != NextEvent {
		t.Fatalf("goal=%q run=%q profile=%+v intent=%+v err=%v", g.ID, r.ID, p, i, err)
	}
}
