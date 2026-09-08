package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func relativeCheckpointFixture(t *testing.T) (*goals.Store, *tools.Registry, string, string) {
	t.Helper()
	now := time.Now().UTC()
	gs, err := goals.OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	expires := now.Add(time.Hour)
	g, err := gs.Create(goals.CreateInput{AgentID: "relative-agent", Managed: true, Objective: "objective", Acceptance: "accept", MinWakeIntervalSeconds: 60, ExpiresAt: &expires}, now)
	if err != nil {
		t.Fatal(err)
	}
	r, err := gs.StartRun(g.ID, "test", now)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(testConfig(t), nil, WithSkipStore())
	srv.goalStore = gs
	reg, err := tools.NewRegistry(t.TempDir(), 10)
	if err != nil {
		t.Fatal(err)
	}
	srv.attachNodeRuntimeDeps(reg, g.AgentID)
	return gs, reg, g.ID, r.ID
}

func TestGoalCheckpointRelativeSecondsUsesServerClockAndPersists(t *testing.T) {
	gs, reg, gid, rid := relativeCheckpointFixture(t)
	ctx := tools.WithGoalRun(context.Background(), gid, rid)
	_, err := reg.Execute(ctx, "goal_checkpoint", `{"call_purpose":"wait","summary":"progress","decision":{"outcome":"progress","summary":"progress","next_action":"at","next_wake_after_seconds":60,"reason":"wait","expected_progress":"next check"}}`)
	if err != nil {
		t.Fatal(err)
	}
	g, _ := gs.Get(gid)
	if g.LastCheckpoint == nil || g.LastCheckpoint.Decision == nil || g.LastCheckpoint.Decision.NextWakeAt == nil {
		t.Fatalf("checkpoint=%+v", g.LastCheckpoint)
	}
	if !g.LastCheckpoint.Decision.NextWakeAt.After(g.LastCheckpoint.At) {
		t.Fatalf("wake=%v at=%v", g.LastCheckpoint.Decision.NextWakeAt, g.LastCheckpoint.At)
	}
	if got := g.LastCheckpoint.Decision.NextWakeAt.Sub(g.LastCheckpoint.At); got != 60*time.Second {
		t.Fatalf("relative wake delta=%v", got)
	}
}

func TestGoalCheckpointRelativeSecondsRejectsAmbiguousAndOverflow(t *testing.T) {
	for name, body := range map[string]string{
		"mutually exclusive": `{"call_purpose":"wait","summary":"progress","decision":{"outcome":"progress","summary":"progress","next_action":"at","next_wake_at":"2099-01-01T00:00:00Z","next_wake_after_seconds":2,"reason":"wait","expected_progress":"next"}}`,
		"overflow":           `{"call_purpose":"wait","summary":"progress","decision":{"outcome":"progress","summary":"progress","next_action":"at","next_wake_after_seconds":2678401,"reason":"wait","expected_progress":"next"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			gs, reg, gid, rid := relativeCheckpointFixture(t)
			_, err := reg.Execute(tools.WithGoalRun(context.Background(), gid, rid), "goal_checkpoint", body)
			if err == nil || !strings.Contains(err.Error(), "wake") && !strings.Contains(err.Error(), "next_wake") {
				t.Fatalf("err=%v", err)
			}
			if g, _ := gs.Get(gid); g.LastCheckpoint != nil {
				t.Fatal("rejected checkpoint persisted")
			}
		})
	}
}

func TestGoalCheckpointCompletedGoalCannotScheduleNext(t *testing.T) {
	gs, reg, gid, rid := relativeCheckpointFixture(t)
	now := time.Now().UTC()
	_, err := reg.Execute(tools.WithGoalRun(context.Background(), gid, rid), "goal_checkpoint", `{"call_purpose":"done","summary":"done","decision":{"outcome":"completed","summary":"done","next_action":"none","reason":"finished"}}`)
	if err != nil {
		t.Fatal(err)
	}
	g, _ := gs.Get(gid)
	if g.LastCheckpoint == nil || g.LastCheckpoint.Decision.NextWakeAt != nil {
		t.Fatalf("checkpoint=%+v", g.LastCheckpoint)
	}
	if err := gs.FinalizeRun(goals.FinalizeInput{RunID: rid, ExpectedGoalRevision: g.Revision, Decision: *g.LastCheckpoint.Decision, ActualTokens: 1, TerminalStatus: "completed", Now: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := gs.GetScheduleIntent(gid, "goal"); err == nil {
		t.Fatal("completed run created a next intent")
	}
}
