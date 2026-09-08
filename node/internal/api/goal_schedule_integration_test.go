package api

import (
	"context"
	"encoding/json"
	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGoalScheduledWakeRunsTwoTurns(t *testing.T) {
	cfg := testConfig(t)
	cfg.Onboarding.NodeProfileCompleted = true
	as, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer as.Close()
	// The scheduled lifecycle must be driven by a validated checkpoint
	// decision; a bare next_wake field is not an authorization to loop.
	fake := &goalWakeLLM{called: make(chan struct{}, 8), release: make(chan struct{}), checkpoint: true}
	srv := NewServer(cfg, nil, WithLLM(fake), WithSkipStore())
	defer srv.sessions.Stop()
	defer func() {
		if srv.feedbackStore != nil {
			_ = srv.feedbackStore.Close()
		}
	}()
	srv.agents = as
	rec := store.AgentRecord{AgentID: "agent-scheduled", DisplayName: "scheduled", ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), RuntimeRevision: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := as.Save(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	if err := as.SaveAgentPolicy(context.Background(), store.AgentPolicyRecord{AgentID: rec.AgentID, Tools: map[string]string{"goal_checkpoint": "never"}}); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"objective": "observe", "acceptance": "evidence", "agent_id": rec.AgentID, "enabled": true})
	rr := httptest.NewRecorder()
	rr = createGoalViaAutonomy(srv, rec.AgentID, b)
	if rr.Code != http.StatusOK {
		t.Fatalf("create=%d %s", rr.Code, rr.Body.String())
	}
	var g struct {
		ID         string    `json:"id"`
		TriggerID  string    `json:"trigger_id"`
		NextWakeAt time.Time `json:"next_wake_at"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &g); err != nil {
		t.Fatal(err)
	}
	srv.triggerSched.RunOnceForTest(context.Background(), g.NextWakeAt.Add(time.Second))
	select {
	case <-fake.called:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduled LLM call missing")
	}
	close(fake.release)
	deadline := time.Now().Add(5 * time.Second)
	for {
		rs := srv.goalStore.Runs(g.ID)
		if len(rs) == 1 && rs[0].FinishedAt != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("first run did not finish: %+v", rs)
		}
		time.Sleep(20 * time.Millisecond)
	}
	second := httptest.NewRecorder()
	srv.triggerSched.RunOnceForTest(context.Background(), time.Now().Add(time.Second))
	if len(srv.goalStore.Runs(g.ID)) != 1 {
		t.Fatalf("early tick runs=%d", len(srv.goalStore.Runs(g.ID)))
	}
	_ = second
	// Move the occurrence to the Goal-owned next wake and run the second tick.
	goal, _ := srv.goalStore.Get(g.ID)
	if goal.NextWakeAt == nil {
		t.Fatal("missing next wake")
	}
	// The scheduler tick performs the durable pending-intent projection. This
	// is also the retry path used after a transient trigger-store write error.
	srv.triggerSched.RunOnceForTest(context.Background(), time.Now().UTC())
	// The intent projector atomically moves the Goal to its stable hash trigger.
	goal, _ = srv.goalStore.Get(g.ID)
	g.TriggerID = goal.TriggerID
	nd, _ := srv.triggerStore.GetTrigger(g.TriggerID)
	if nd == nil {
		t.Fatalf("projected trigger missing goal=%+v intents=%+v triggers=%+v", goal, srv.goalStore.ListScheduleIntents(g.ID), srv.triggerStore.ListTriggers())
	}
	if nd.NextFireAt == nil {
		t.Fatal("missing trigger next fire")
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		pending, active, _, _ := srv.sessions.RuntimeInfo(goal.SessionID)
		if pending == 0 && !active {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("goal session remained busy: pending=%d active=%v", pending, active)
		}
		time.Sleep(20 * time.Millisecond)
	}
	dispatchAt := time.Unix(int64(*nd.NextFireAt), 0).Add(time.Second)
	if goal.NextWakeAt.After(dispatchAt) {
		dispatchAt = goal.NextWakeAt.Add(time.Second)
	}
	srv.triggerSched.RunOnceForTest(context.Background(), dispatchAt)
	select {
	case <-fake.called:
	case <-time.After(5 * time.Second):
		t.Fatal("second scheduled LLM call missing")
	}
	deadline = time.Now().Add(5 * time.Second)
	var completedRuns []goals.Run
	for {
		runs := srv.goalStore.Runs(g.ID)
		if len(runs) == 2 && runs[1].FinishedAt != nil {
			completedRuns = runs
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("second run did not finish: %+v", runs)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if completedRuns[0].TurnID == completedRuns[1].TurnID || completedRuns[1].Generation <= completedRuns[0].Generation {
		t.Fatalf("second run was not a new generation: %+v", completedRuns)
	}
	intent, err := srv.goalStore.GetScheduleIntent(g.ID, "goal")
	if err != nil || intent.Generation != completedRuns[1].Generation {
		t.Fatalf("second decision did not create matching intent: intent=%+v err=%v runs=%+v", intent, err, completedRuns)
	}
}
