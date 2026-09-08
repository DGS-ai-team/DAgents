package api

import (
	"context"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func TestAutonomyToolCallbackUpdatesOnlyOwnedIntent(t *testing.T) {
	cfg := testConfig(t)
	cfg.Onboarding.NodeProfileCompleted = true
	srv := NewServer(cfg, nil, WithSkipStore())
	defer srv.Close()
	now := time.Now().UTC()
	g, err := srv.goalStore.CreateManaged(goals.CreateInput{AgentID: "auto-owned", Objective: "old", Acceptance: "old evidence", MaxRuns: 3, TokenBudget: 900, TurnTokenBudget: 100, MinWakeIntervalSeconds: 60}, now, true)
	if err != nil {
		t.Fatal(err)
	}
	before := g
	wake := now.Add(2 * time.Hour)
	obj := "new objective"
	got, err := srv.autonomyToolUpdate(context.Background(), "auto-owned", tools.AutonomyUpdate{Objective: &obj, NextWakeAt: &wake})
	if err != nil {
		t.Fatal(err)
	}
	view, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("view type=%T", got)
	}
	if view["objective"] != obj {
		t.Fatalf("objective=%v", view["objective"])
	}
	after, ok := srv.goalStore.Get(before.ID)
	if !ok {
		t.Fatal("goal disappeared")
	}
	if after.MaxRuns != before.MaxRuns || after.TokenBudget != before.TokenBudget || after.Runs != before.Runs || after.TokensUsed != before.TokensUsed {
		t.Fatalf("limits/usage changed: before=%+v after=%+v", before, after)
	}
	if after.NextWakeAt == nil || !after.NextWakeAt.Equal(wake) {
		t.Fatalf("next wake=%v", after.NextWakeAt)
	}
	if _, err := srv.autonomyToolUpdate(context.Background(), "other-agent", tools.AutonomyUpdate{Objective: &obj}); err == nil {
		t.Fatal("unconfigured agent was allowed to create task")
	}
	if _, err := srv.autonomyToolUpdate(tools.WithGoalRun(context.Background(), "goal", "run"), "auto-owned", tools.AutonomyUpdate{Objective: &obj}); err == nil {
		t.Fatal("dedicated goal context was allowed")
	}
}

func TestAutonomyToolRejectsWaitingRun(t *testing.T) {
	cfg := testConfig(t)
	cfg.Onboarding.NodeProfileCompleted = true
	srv := NewServer(cfg, nil, WithSkipStore())
	defer srv.Close()
	g, err := srv.goalStore.CreateManaged(goals.CreateInput{AgentID: "auto-busy", Objective: "old", Acceptance: "evidence", MaxRuns: 3, TokenBudget: 900, TurnTokenBudget: 100, MinWakeIntervalSeconds: 60}, time.Now().UTC(), true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = srv.goalStore.StartRun(g.ID, "test", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err = srv.autonomyToolUpdate(context.Background(), "auto-busy", tools.AutonomyUpdate{}); err == nil {
		t.Fatal("waiting run was adjustable")
	}
}
