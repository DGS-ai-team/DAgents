package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

func createBusyTestAgent(t *testing.T, as *store.AgentStore, id string) {
	t.Helper()
	now := time.Now()
	if err := as.Save(context.Background(), store.AgentRecord{AgentID: id, DisplayName: id, ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), RuntimeRevision: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
}

func createBusyTestGoal(t *testing.T, srv *Server, agentID string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"objective": "observe", "acceptance": "evidence", "agent_id": agentID, "enabled": true, "min_wake_interval_seconds": 60})
	rr := httptest.NewRecorder()
	rr = createGoalViaAutonomy(srv, agentID, body)
	if rr.Code != http.StatusOK {
		t.Fatalf("create goal=%d %s", rr.Code, rr.Body.String())
	}
	var goal struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &goal); err != nil || goal.ID == "" {
		t.Fatalf("invalid goal=%s err=%v", rr.Body.String(), err)
	}
	return goal.ID
}

func wakeBusyTestGoal(srv *Server, id string) int {
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/goals/"+id+"/wake", nil))
	return rr.Code
}

func TestTwoGoalsSameAgentConcurrentWakeOnlyOneRuns(t *testing.T) {
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
	if srv.triggerSched != nil {
		srv.triggerSched.Stop()
	}
	defer func() {
		if srv.feedbackStore != nil {
			_ = srv.feedbackStore.Close()
		}
	}()
	srv.agents = as
	createBusyTestAgent(t, as, cfg.NodeID)
	// The autonomy PUT is an upsert for one Agent. Create two distinct managed
	// Goal fixtures directly so this test continues to exercise busy arbitration.
	firstGoal, err := srv.createManagedGoal(context.Background(), goals.CreateInput{Objective: "observe one", Acceptance: "evidence", AgentID: cfg.NodeID, MinWakeIntervalSeconds: 60}, true)
	if err != nil {
		t.Fatal(err)
	}
	secondGoal, err := srv.createManagedGoal(context.Background(), goals.CreateInput{Objective: "observe two", Acceptance: "evidence", AgentID: cfg.NodeID, MinWakeIntervalSeconds: 60}, true)
	if err != nil {
		t.Fatal(err)
	}
	first, second := firstGoal.ID, secondGoal.ID
	if first == second {
		t.Fatalf("distinct Goal fixtures reused ID %q", first)
	}

	codes := make(chan int, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	for _, id := range []string{first, second} {
		go func(goalID string) { defer wg.Done(); codes <- wakeBusyTestGoal(srv, goalID) }(id)
	}
	wg.Wait()
	close(codes)
	accepted, conflicts := 0, 0
	for code := range codes {
		if code == http.StatusAccepted {
			accepted++
		}
		if code == http.StatusConflict {
			conflicts++
		}
	}
	if accepted != 1 || conflicts != 1 {
		t.Fatalf("concurrent wake codes accepted=%d conflicts=%d", accepted, conflicts)
	}
	select {
	case <-fake.called:
	case <-time.After(5 * time.Second):
		t.Fatal("first goal did not call model")
	}
	if len(srv.goalStore.Runs(first))+len(srv.goalStore.Runs(second)) != 1 {
		t.Fatalf("runs first=%d second=%d", len(srv.goalStore.Runs(first)), len(srv.goalStore.Runs(second)))
	}
	close(fake.release)
}

func TestDifferentAgentsCanWakeConcurrently(t *testing.T) {
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
	if srv.triggerSched != nil {
		srv.triggerSched.Stop()
	}
	defer func() {
		if srv.feedbackStore != nil {
			_ = srv.feedbackStore.Close()
		}
	}()
	srv.agents = as
	createBusyTestAgent(t, as, "agent-a")
	createBusyTestAgent(t, as, "agent-b")
	first, second := createBusyTestGoal(t, srv, "agent-a"), createBusyTestGoal(t, srv, "agent-b")
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	for _, id := range []string{first, second} {
		go func(goalID string) { defer wg.Done(); codes <- wakeBusyTestGoal(srv, goalID) }(id)
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != http.StatusAccepted {
			t.Fatalf("different-agent wake=%d", code)
		}
	}
	for i := 0; i < 2; i++ {
		select {
		case <-fake.called:
		case <-time.After(5 * time.Second):
			t.Fatal("missing concurrent model call")
		}
	}
	close(fake.release)
}
