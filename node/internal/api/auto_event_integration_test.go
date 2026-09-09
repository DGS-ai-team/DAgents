package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/events"
	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

// TestAutoEventProbeServerFixture exercises the persisted startup path: the
// source, Goal profile and NextEvent intent all exist before Server starts.
func TestAutoEventProbeServerFixture(t *testing.T) {
	cfg := testConfig(t)
	cfg.LLM.Mock = true
	cfg.Onboarding.NodeProfileCompleted = true
	settings, err := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	if err = settings.Save(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	settings.Close()
	as, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err = as.Save(context.Background(), store.AgentRecord{AgentID: "event-auto", ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	as.Close()
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a"), []byte("one"), 0600)
	es, err := events.OpenStore(filepath.Join(cfg.RuntimeDir(), "events.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = es.RegisterSource(events.SourceRegistration{SourceID: "files", OwnerAgentID: "event-auto", Revision: 1, Root: root, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	gs, err := goals.OpenStore(filepath.Join(cfg.RuntimeDir(), "goals.json"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := gs.SaveProfile(goals.AutoProfile{AgentID: "event-auto", Enabled: true, PlanMode: "recurring", Revision: 1, UpdatedAt: now}, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	g, err := gs.CreateManagedCycle(goals.CreateInput{AgentID: "event-auto", Managed: true, Objective: "event", Acceptance: "proof", SessionID: "event-session"}, "event", p.Revision, now, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = gs.BindSession(g.ID, "event-session", now); err != nil {
		t.Fatal(err)
	}
	p, ok := gs.GetProfile("event-auto")
	if !ok {
		t.Fatal("profile missing")
	}
	intent := goals.ScheduleIntent{ID: "event-intent", AgentID: g.AgentID, GoalID: g.ID, Purpose: "goal", Generation: 1, Decision: goals.FinalDecision{Outcome: goals.OutcomeProgress, Summary: "wait", Reason: "event", ExpectedProgress: "change", NextAction: goals.NextEvent, Event: &goals.EventSpec{SourceID: "files", Filter: map[string]any{"equals": map[string]any{"files": 1}}}}, State: goals.IntentPending, ProfileRevision: p.Revision, UpdatedAt: now}
	if _, err = gs.UpsertScheduleIntent(intent); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}))
	defer srv.Close()
	srv.cfg.Onboarding.NodeProfileCompleted = true
	if srv.startupErr != nil {
		t.Fatal(srv.startupErr)
	}
	srv.triggerSched.RunOnceForTest(context.Background(), now)
	if n := len(srv.goalStore.Runs(g.ID)); n != 0 {
		t.Fatalf("baseline runs=%d", n)
	}
	os.WriteFile(filepath.Join(root, "b"), []byte("two"), 0600)
	srv.triggerSched.RunOnceForTest(context.Background(), now.Add(time.Second))
	if n := len(srv.goalStore.Runs(g.ID)); n != 0 {
		t.Fatalf("nonmatching filter runs=%d", n)
	}
	os.Remove(filepath.Join(root, "b"))
	os.WriteFile(filepath.Join(root, "a"), []byte("changed"), 0600)
	srv.triggerSched.RunOnceForTest(context.Background(), now.Add(2*time.Second))
	deadline := time.Now().Add(3 * time.Second)
	for len(srv.goalStore.Runs(g.ID)) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if n := len(srv.goalStore.Runs(g.ID)); n != 1 {
		t.Fatalf("matching runs=%d", n)
	}
	srv.triggerSched.RunOnceForTest(context.Background(), now.Add(3*time.Second))
	if n := len(srv.goalStore.Runs(g.ID)); n != 1 {
		t.Fatalf("repeat runs=%d", n)
	}
	reg, _ := srv.eventStore.GetRegistration("files")
	reg.Enabled = false
	reg.Revision++
	if err := srv.eventStore.RegisterSource(reg); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "a"), []byte("disabled"), 0600)
	srv.triggerSched.RunOnceForTest(context.Background(), now.Add(4*time.Second))
	if n := len(srv.goalStore.Runs(g.ID)); n != 1 {
		t.Fatalf("disabled source runs=%d", n)
	}
	if err := srv.goalStore.RevokeScheduleIntent(g.ID, "goal", 1); err != nil {
		t.Fatal(err)
	}
	reg.Enabled = true
	reg.Revision++
	if err := srv.eventStore.RegisterSource(reg); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "a"), []byte("revoked"), 0600)
	srv.triggerSched.RunOnceForTest(context.Background(), now.Add(5*time.Second))
	if n := len(srv.goalStore.Runs(g.ID)); n != 1 {
		t.Fatalf("revoked intent runs=%d", n)
	}
}
