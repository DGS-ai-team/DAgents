package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/goals"
)

func TestAutonomyResumeCreatesFreshIntentGeneration(t *testing.T) {
	srv, _ := autonomyRegressionServer(t)
	created := putAutonomy(t, srv, map[string]any{"objective": "resume", "acceptance": "proof", "enabled": true})
	if created.Code != http.StatusOK {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	p, _ := srv.goalStore.GetProfile("auto-reg")
	g, _ := srv.goalStore.Get(p.CurrentGoalID)
	now := time.Now().UTC()
	due := now.Add(-time.Minute)
	d := goals.FinalDecision{Outcome: goals.OutcomeProgress, Summary: "continue", Reason: "resume", ExpectedProgress: "next", NextAction: goals.NextAt, NextWakeAt: &due}
	intent, err := srv.goalStore.UpsertScheduleIntent(goals.ScheduleIntent{ID: "seed:goal", AgentID: g.AgentID, GoalID: g.ID, Purpose: "goal", Generation: 1, Decision: d, DueAt: &due, State: goals.IntentPending, ProfileRevision: p.Revision, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	p, _ = srv.goalStore.GetProfile("auto-reg")
	g, _ = srv.goalStore.Get(g.ID)
	postAction := func(action string, p goals.AutoProfile, g goals.Goal) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"action": action, "expected_profile_revision": p.Revision, "expected_goal_revision": g.Revision})
		r := httptest.NewRequest(http.MethodPost, "/v1/agents/auto-reg/autonomy/actions", bytes.NewReader(body))
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, r)
		return w
	}
	if w := postAction("pause_goal", p, g); w.Code != http.StatusOK {
		t.Fatalf("pause=%d %s", w.Code, w.Body.String())
	}
	p, _ = srv.goalStore.GetProfile("auto-reg")
	g, _ = srv.goalStore.Get(g.ID)
	if w := postAction("resume_goal", p, g); w.Code != http.StatusOK {
		t.Fatalf("resume=%d %s", w.Code, w.Body.String())
	}
	resumedP, _ := srv.goalStore.GetProfile("auto-reg")
	resumedG, _ := srv.goalStore.Get(g.ID)
	resumed, err := srv.goalStore.GetScheduleIntent(g.ID, "goal")
	if err != nil {
		t.Fatal(err)
	}
	if resumed.ID == intent.ID || resumed.Generation != intent.Generation+1 || resumed.ProfileRevision != resumedP.Revision || resumed.State != goals.IntentPending || !resumed.DueAt.After(now) {
		t.Fatalf("resume did not create fresh future intent: old=%+v new=%+v profile=%+v", intent, resumed, resumedP)
	}
	if resumedG.Status != goals.StatusActive {
		t.Fatalf("goal not active after resume: %+v", resumedG)
	}
	projected, err := (&AutoIntentProjector{Goals: srv.goalStore, Triggers: srv.triggerStore}).Project(g.ID, "goal")
	if err != nil || !projected.Enabled || projected.ManagedGeneration != resumed.Generation {
		t.Fatalf("resume projection=%+v err=%v", projected, err)
	}
}
