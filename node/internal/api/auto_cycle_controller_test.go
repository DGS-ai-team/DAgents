package api

import (
	"context"
	"encoding/json"
	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"path/filepath"
	"testing"
	"time"
)

func controllerFixture(t *testing.T, now time.Time) (*Server, *store.AgentStore, goals.Goal, string) {
	t.Helper()
	srv := NewServer(testConfig(t), nil, WithSkipStore())
	if srv.triggerSched != nil {
		srv.triggerSched.Stop()
	}
	as, err := store.OpenAgents(filepath.Join(t.TempDir(), "agents.db"))
	if err != nil {
		t.Fatal(err)
	}
	rec := store.AgentRecord{AgentID: "cycle-agent", DisplayName: "cycle", ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), CreatedAt: now, UpdatedAt: now}
	if err = as.Save(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	srv.agents = as
	goalPath := filepath.Join(t.TempDir(), "goals.json")
	gs, err := goals.OpenStore(goalPath)
	if err != nil {
		t.Fatal(err)
	}
	srv.goalStore = gs
	p, err := gs.SaveProfile(goals.AutoProfile{AgentID: rec.AgentID, Enabled: true, PlanMode: "recurring", Timezone: "UTC", WorkSchedule: "daily 09:00", CycleDurationSeconds: 3600}, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	old, err := gs.CreateManagedCycle(goals.CreateInput{AgentID: rec.AgentID, Managed: true, Objective: "old", Acceptance: "ok", SessionID: "sess-old", MinWakeIntervalSeconds: 60}, "old", p.Revision, now, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = gs.SetStatus(old.ID, goals.StatusCompleted, now); err != nil {
		t.Fatal(err)
	}
	p, _ = gs.GetProfile(rec.AgentID)
	cur, err := gs.CreateManagedCycle(goals.CreateInput{AgentID: rec.AgentID, Managed: true, Objective: "current", Acceptance: "ok", SessionID: "sess-current", MinWakeIntervalSeconds: 60}, "current", p.Revision, now, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = gs.BindSession(cur.ID, "sess-current", now); err != nil {
		t.Fatal(err)
	}
	r, err := gs.StartRun(cur.ID, "test", now.Add(-72*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	r.Status = "completed"
	r.TokensUsed = 3
	if _, err = gs.FinishRun(cur.ID, r, now.Add(-71*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = gs.SetStatus(cur.ID, goals.StatusCompleted, now); err != nil {
		t.Fatal(err)
	}
	p, _ = gs.GetProfile(rec.AgentID)
	if err = srv.bindRecurringAuthorization(context.Background(), rec.AgentID, &p); err != nil {
		t.Fatal(err)
	}
	if _, err = gs.SaveProfile(p, p.Revision, now); err != nil {
		t.Fatal(err)
	}
	return srv, as, cur, goalPath
}

func TestAutoCycleControllerCurrentHistoryAndOfflineCoalesce(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	srv, as, cur, goalPath := controllerFixture(t, now)
	defer as.Close()
	defer srv.Close()
	cur, _ = srv.goalStore.Get(cur.ID)
	if err := srv.reconcileAutoCycles(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	p, _ := srv.goalStore.GetProfile(cur.AgentID)
	child, ok := srv.goalStore.Get(p.CurrentGoalID)
	if !ok || child.PreviousGoalID != cur.ID || !child.Coalesced {
		t.Fatalf("profile=%+v child=%+v", p, child)
	}
	if err := srv.reconcileAutoCycles(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if len(srv.goalStore.List()) != 3 {
		t.Fatalf("goals=%d", len(srv.goalStore.List()))
	}
	reloaded, err := goals.OpenStore(goalPath)
	if err != nil {
		t.Fatal(err)
	}
	srv.goalStore = reloaded
	if err := srv.reconcileAutoCycles(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if len(reloaded.List()) != 3 {
		t.Fatalf("reloaded goals=%d", len(reloaded.List()))
	}
}

func TestAutoCycleControllerAuthorizationAndConfigurationSummary(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	srv, as, cur, _ := controllerFixture(t, now)
	defer as.Close()
	defer srv.Close()
	cur, _ = srv.goalStore.Get(cur.ID)
	p, _ := srv.goalStore.GetProfile(cur.AgentID)
	if err := as.SaveAgentPolicy(context.Background(), store.AgentPolicyRecord{AgentID: cur.AgentID, Tools: map[string]string{"goal_checkpoint": "never"}}); err != nil {
		t.Fatal(err)
	}
	if got := srv.projectAutoSummary(cur.AgentID, p, &cur)["state_reason"]; got != "authorization_changed" {
		t.Fatalf("reason=%v", got)
	}
	p.CycleDurationSeconds = 0
	if got := srv.projectAutoSummary(cur.AgentID, p, &cur)["state_reason"]; got != "configuration_required" {
		t.Fatalf("reason=%v", got)
	}
}
