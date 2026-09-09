package goals

import "testing"
import "time"

func TestResumeRegisteredEventWithNoDueAtCreatesNewGeneration(t *testing.T) {
	now := time.Now().UTC()
	s, _ := OpenStore("")
	testProfile(t, s, "auto-event", now)
	g, err := s.CreateManagedCycle(CreateInput{AgentID: "auto-event", Objective: "event", Acceptance: "proof"}, "event", 1, now, true)
	if err != nil {
		t.Fatal(err)
	}
	g, err = s.SetStatus(g.ID, StatusPaused, now)
	if err != nil {
		t.Fatal(err)
	}
	s.SetRegisteredSources(map[string]map[string]bool{"auto-event": {"files": true}})
	p, _ := s.GetProfile("auto-event")
	p.CurrentGoalID = g.ID
	p.Revision++
	if _, err = s.SaveProfile(p, p.Revision-1, now); err != nil {
		t.Fatal(err)
	}
	i := ScheduleIntent{ID: "event", AgentID: g.AgentID, GoalID: g.ID, Purpose: "goal", Generation: 1, Decision: FinalDecision{Outcome: OutcomeProgress, Summary: "wait", Reason: "event", ExpectedProgress: "change", NextAction: NextEvent, Event: &EventSpec{SourceID: "files", Filter: map[string]any{"equals": map[string]any{"files": 1}}}}, State: IntentRevoked, ProfileRevision: p.Revision, UpdatedAt: now}
	if _, err = s.UpsertScheduleIntent(i); err != nil {
		t.Fatal(err)
	}
	beforeP, _ := s.GetProfile(g.AgentID)
	beforeG, _ := s.Get(g.ID)
	if _, _, err = s.ApplyAutoAction(g.AgentID, g.ID, "resume_goal", beforeP.Revision, beforeG.Revision, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetScheduleIntent(g.ID, "goal")
	if err != nil || got.State != IntentPending || got.Generation != 2 || got.DueAt != nil {
		t.Fatalf("intent=%+v err=%v", got, err)
	}
}
