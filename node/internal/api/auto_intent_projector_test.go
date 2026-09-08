package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
)

func TestStartupRestoresPendingAutoIntent(t *testing.T) {
	cfg := testConfig(t)
	cfg.Onboarding.NodeProfileCompleted = true
	cfg.LLM.Mock = true
	now := time.Now().UTC().Truncate(time.Second)
	as, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := as.Save(context.Background(), store.AgentRecord{AgentID: "startup-pending-auto", DisplayName: "Auto", ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	_ = as.Close()
	gs, err := goals.OpenStore(filepath.Join(cfg.RuntimeDir(), "goals.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gs.SaveProfile(goals.AutoProfile{AgentID: "startup-pending-auto", Enabled: true, Revision: 1, UpdatedAt: now}, 0, now); err != nil {
		t.Fatal(err)
	}
	g, err := gs.CreateManagedCycle(goals.CreateInput{Title: "startup", Objective: "objective", Acceptance: "proof", AgentID: "startup-pending-auto", Managed: true}, "startup-pending", 1, now, true)
	if err != nil {
		t.Fatal(err)
	}
	if g, err = gs.BindSession(g.ID, "startup-session", now); err != nil {
		t.Fatal(err)
	}
	p, _ := gs.GetProfile(g.AgentID)
	p.CurrentGoalID = g.ID
	p.Enabled = true
	if _, err := gs.SaveProfile(p, p.Revision, now); err != nil {
		t.Fatal(err)
	}
	at := now.Add(time.Hour)
	d := goals.FinalDecision{Outcome: goals.OutcomeProgress, Summary: "progress", Reason: "next", ExpectedProgress: "continue", NextAction: goals.NextAt, NextWakeAt: &at}
	if _, err := gs.UpsertScheduleIntent(goals.ScheduleIntent{ID: "startup-intent", AgentID: g.AgentID, GoalID: g.ID, Purpose: "goal", Generation: 1, Decision: d, DueAt: &at, State: goals.IntentPending, ProfileRevision: p.Revision + 1, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}))
	defer srv.Close()
	if srv.startupErr != nil {
		t.Fatal(srv.startupErr)
	}
	got, ok := srv.triggerStore.GetTrigger(managedIntentTriggerID(g.ID, "goal"))
	if !ok || !got.Enabled || got.ManagedGeneration != 1 {
		pp, _ := srv.goalStore.GetProfile(g.AgentID)
		ii, _ := srv.goalStore.GetScheduleIntent(g.ID, "goal")
		t.Fatalf("restored trigger=%+v ok=%v startup=%v profile=%+v intent=%+v", got, ok, srv.startupErr, pp, ii)
	}
	bound, _ := srv.goalStore.Get(g.ID)
	if bound.TriggerID != got.TriggerID {
		t.Fatalf("goal binding=%q trigger=%q", bound.TriggerID, got.TriggerID)
	}
	intent, _ := srv.goalStore.GetScheduleIntent(g.ID, "goal")
	if intent.State != goals.IntentProjected {
		t.Fatalf("intent=%+v", intent)
	}
}

func TestAutoIntentProjectorRejectsStaleProfileRevision(t *testing.T) {
	p, g, now, _ := projectorFixture(t)
	profile, _ := p.Goals.GetProfile(g.AgentID)
	profile.Revision++
	if _, err := p.Goals.SaveProfile(profile, profile.Revision-1, now); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Project(g.ID, "goal"); err == nil {
		t.Fatal("stale profile intent projected")
	}
}

func TestAutoIntentProjectorRejectsMissingSessionWithoutConsumingIntent(t *testing.T) {
	p, g, now, _ := projectorFixture(t)
	if _, err := p.Goals.BindSession(g.ID, "", now); err != nil {
		t.Fatal(err)
	}
	before, err := p.Goals.GetScheduleIntent(g.ID, "goal")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Project(g.ID, "goal"); err == nil {
		t.Fatal("empty session was projected")
	}
	after, err := p.Goals.GetScheduleIntent(g.ID, "goal")
	if err != nil || after.State != goals.IntentPending || after.ID != before.ID || after.Generation != before.Generation {
		t.Fatalf("pending intent changed: before=%+v after=%+v err=%v", before, after, err)
	}
}

func TestAutoIntentProjectorDiskFailureRetry(t *testing.T) {
	p, g, _, triggerPath := projectorFixture(t)
	if err := os.Mkdir(triggerPath+".tmp", 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Project(g.ID, "goal"); err == nil {
		t.Fatal("expected projection write failure")
	}
	i, _ := p.Goals.GetScheduleIntent(g.ID, "goal")
	if i.State != goals.IntentPending {
		t.Fatalf("intent changed: %+v", i)
	}
	if err := os.Remove(triggerPath + ".tmp"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Project(g.ID, "goal"); err != nil {
		t.Fatal(err)
	}
	if got := len(p.Triggers.ListTriggers()); got != 1 {
		t.Fatalf("trigger count=%d", got)
	}
}

func projectorFixture(t *testing.T) (*AutoIntentProjector, goals.Goal, time.Time, string) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	gs, err := goals.OpenStore(filepath.Join(t.TempDir(), "goals.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gs.SaveProfile(goals.AutoProfile{AgentID: "a", Enabled: true, Revision: 1, UpdatedAt: now}, 0, now); err != nil {
		t.Fatal(err)
	}
	g, err := gs.CreateManagedCycle(goals.CreateInput{Title: "g", Objective: "do work", Acceptance: "done", AgentID: "a", Managed: true}, "cycle", 1, now, true)
	if err != nil {
		t.Fatal(err)
	}
	if g, err = gs.BindSession(g.ID, "session-a", now); err != nil {
		t.Fatal(err)
	}
	profile, _ := gs.GetProfile("a")
	profile.Enabled, profile.CurrentGoalID, profile.UpdatedAt = true, g.ID, now
	if _, err := gs.SaveProfile(profile, profile.Revision, now); err != nil {
		t.Fatal(err)
	}
	g, _ = gs.Get(g.ID)
	at := now.Add(time.Hour)
	d := goals.FinalDecision{Outcome: goals.OutcomeProgress, Summary: "progress", Reason: "next", ExpectedProgress: "continue", NextAction: goals.NextAt, NextWakeAt: &at}
	profile, _ = gs.GetProfile("a")
	i := goals.ScheduleIntent{ID: "intent", AgentID: "a", GoalID: g.ID, Purpose: "goal", Generation: 1, Decision: d, DueAt: &at, State: goals.IntentPending, ProfileRevision: profile.Revision, UpdatedAt: now}
	if _, err := gs.UpsertScheduleIntent(i); err != nil {
		t.Fatal(err)
	}
	triggerPath := filepath.Join(t.TempDir(), "triggers.json")
	ts, err := triggers.OpenStore(triggerPath, 20)
	if err != nil {
		t.Fatal(err)
	}
	return &AutoIntentProjector{Goals: gs, Triggers: ts}, g, now, triggerPath
}

func TestAutoIntentProjectorReplayAndFireAt(t *testing.T) {
	p, g, _, _ := projectorFixture(t)
	first, err := p.Project(g.ID, "goal")
	if err != nil {
		t.Fatal(err)
	}
	if !first.Enabled || first.Condition["fire_at"] == nil || first.ManagedGeneration != 1 {
		t.Fatalf("projection=%+v", first)
	}
	second, err := p.Project(g.ID, "goal")
	if err != nil {
		t.Fatal(err)
	}
	if !second.Enabled || second.TriggerID != first.TriggerID {
		t.Fatalf("replay=%+v", second)
	}
	if second.Condition["interval_seconds"] != nil {
		t.Fatal("projection became recurring interval")
	}
}

func TestAutoIntentProjectorPauseFenceAndStaleRevoke(t *testing.T) {
	p, g, now, _ := projectorFixture(t)
	if _, err := p.Project(g.ID, "goal"); err != nil {
		t.Fatal(err)
	}
	newer, err := p.Goals.GetScheduleIntent(g.ID, "goal")
	if err != nil {
		t.Fatal(err)
	}
	newer.Generation = 2
	newer.Fingerprint = ""
	if _, err := p.Goals.UpsertScheduleIntent(newer); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Project(g.ID, "goal"); err != nil {
		t.Fatal(err)
	}
	if err := p.Goals.RevokeScheduleIntent(g.ID, "goal", 1); err == nil {
		t.Fatal("stale generation revoke unexpectedly succeeded")
	}
	profile, _ := p.Goals.GetProfile(g.AgentID)
	profile.Enabled = false
	if _, err := p.Goals.SaveProfile(profile, profile.Revision, now); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Project(g.ID, "goal"); err == nil {
		t.Fatal("paused profile projection unexpectedly succeeded")
	}
	if err := p.Goals.RevokeScheduleIntent(g.ID, "goal", 2); err != nil {
		t.Fatal(err)
	}
	revoked, err := p.Project(g.ID, "goal")
	if err != nil {
		t.Fatal(err)
	}
	if revoked.Enabled {
		t.Fatal("revoked trigger remained enabled")
	}
	_ = now
}

func TestAutoIntentProjectorConsumedGenerationDoesNotBlockNext(t *testing.T) {
	p, g, now, _ := projectorFixture(t)
	first, err := p.Project(g.ID, "goal")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Triggers.MarkFired(first.TriggerID, now); err != nil {
		t.Fatal(err)
	}
	i, _ := p.Goals.GetScheduleIntent(g.ID, "goal")
	i.Generation = 2
	i.Fingerprint = ""
	i.Decision.NextWakeAt = func() *time.Time { x := now.Add(2 * time.Hour); return &x }()
	if _, err := p.Goals.UpsertScheduleIntent(i); err != nil {
		t.Fatal(err)
	}
	second, err := p.Project(g.ID, "goal")
	if err != nil {
		t.Fatal(err)
	}
	if !second.Enabled || second.ManagedGeneration != 2 || second.LastFiredAt != nil {
		t.Fatalf("next generation not rearmed: %+v", second)
	}
}

func TestAutoIntentProjectorConsumedAndRevokedReconcileAreIdempotent(t *testing.T) {
	p, g, now, _ := projectorFixture(t)
	first, err := p.Project(g.ID, "goal")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Triggers.MarkFired(first.TriggerID, now); err != nil {
		t.Fatal(err)
	}
	consumed, err := p.Project(g.ID, "goal")
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := p.Project(g.ID, "goal")
	if err != nil {
		t.Fatal(err)
	}
	if consumed.Revision != replayed.Revision || replayed.Enabled {
		t.Fatalf("consumed reconcile mutated state: first=%+v second=%+v", consumed, replayed)
	}
	if err := p.Goals.RevokeScheduleIntent(g.ID, "goal", 1); err != nil {
		t.Fatal(err)
	}
	revoked, err := p.Project(g.ID, "goal")
	if err != nil {
		t.Fatal(err)
	}
	revokedAgain, err := p.Project(g.ID, "goal")
	if err != nil {
		t.Fatal(err)
	}
	if revokedAgain.Revision != revoked.Revision || revokedAgain.Enabled {
		t.Fatalf("revoked reconcile mutated state: first=%+v second=%+v", revoked, revokedAgain)
	}
}
