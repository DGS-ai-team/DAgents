package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

// TestGoalBusyAgentDefersScheduledWake proves the busy gate is applied before
// StartRun for both manual and managed scheduler deliveries.
func TestGoalBusyAgentDefersScheduledWake(t *testing.T) {
	cfg := testConfig(t)
	cfg.Onboarding.NodeProfileCompleted = true
	as, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer as.Close()
	fake := &goalWakeLLM{called: make(chan struct{}, 4), release: make(chan struct{})}
	srv := NewServer(cfg, nil, WithLLM(fake), WithSkipStore())
	defer srv.sessions.Stop()
	defer func() {
		if srv.feedbackStore != nil {
			_ = srv.feedbackStore.Close()
		}
	}()
	srv.agents = as
	rec := store.AgentRecord{AgentID: cfg.NodeID, DisplayName: "busy-agent", ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), RuntimeRevision: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := as.Save(context.Background(), rec); err != nil {
		t.Fatal(err)
	}

	mainID := createTestRuntime(t, srv)
	if _, err := srv.sessions.EnqueueMessage(context.Background(), mainID, "message", "human work", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fake.called:
	case <-time.After(5 * time.Second):
		t.Fatal("main turn did not start")
	}

	body, _ := json.Marshal(map[string]any{"objective": "observe", "acceptance": "evidence", "agent_id": rec.AgentID, "enabled": true, "min_wake_interval_seconds": 60})
	create := httptest.NewRecorder()
	create = createGoalViaAutonomy(srv, rec.AgentID, body)
	if create.Code != http.StatusOK {
		t.Fatalf("create=%d %s", create.Code, create.Body.String())
	}
	var goal struct {
		ID         string    `json:"id"`
		NextWakeAt time.Time `json:"next_wake_at"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &goal); err != nil || goal.ID == "" || goal.NextWakeAt.IsZero() {
		t.Fatalf("goal=%s err=%v", create.Body.String(), err)
	}

	manual := httptest.NewRecorder()
	srv.Handler().ServeHTTP(manual, httptest.NewRequest(http.MethodPost, "/v1/goals/"+goal.ID+"/wake", nil))
	if manual.Code != http.StatusConflict {
		t.Fatalf("busy manual wake=%d %s", manual.Code, manual.Body.String())
	}
	if runs := srv.goalStore.Runs(goal.ID); len(runs) != 0 {
		t.Fatalf("busy manual created runs=%d", len(runs))
	}

	srv.triggerSched.RunOnceForTest(context.Background(), goal.NextWakeAt.Add(time.Second))
	if runs := srv.goalStore.Runs(goal.ID); len(runs) != 0 {
		t.Fatalf("busy scheduled created runs=%d", len(runs))
	}

	close(fake.release)
	waitSessionIdle(t, srv, mainID)
	current, ok := srv.goalStore.Get(goal.ID)
	if !ok {
		t.Fatal("goal disappeared")
	}
	trigger, found := srv.triggerStore.GetTrigger(current.TriggerID)
	if !found || trigger.NextFireAt == nil {
		t.Fatalf("managed trigger=%v found=%v", trigger, found)
	}
	srv.triggerSched.RunOnceForTest(context.Background(), time.Unix(int64(*trigger.NextFireAt), 0).Add(time.Second))
	select {
	case <-fake.called:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduled goal turn did not start after Agent became idle")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if runs := srv.goalStore.Runs(goal.ID); len(runs) == 1 && runs[0].FinishedAt != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("goal did not finish: %+v", srv.goalStore.Runs(goal.ID))
}
