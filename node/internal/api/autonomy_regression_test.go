package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func autonomyRegressionServer(t *testing.T) (*Server, *store.AgentStore) {
	t.Helper()
	cfg := testConfig(t)
	cfg.Onboarding.NodeProfileCompleted = true
	as, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil, WithSkipStore())
	if srv.triggerSched != nil {
		srv.triggerSched.Stop()
	}
	srv.agents = as
	t.Cleanup(func() { _ = as.Close(); srv.Close() })
	now := time.Now().UTC()
	if err := as.Save(context.Background(), store.AgentRecord{AgentID: "auto-reg", DisplayName: "auto", ConfigSnapshot: json.RawMessage(`{"agent_type":"auto","defaults":{}}`), RuntimeRevision: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	return srv, as
}

func putAutonomy(t *testing.T, srv *Server, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(payload)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/v1/agents/auto-reg/autonomy", bytes.NewReader(b)))
	return w
}

func TestAutonomyRegressionConcurrentFirstConfigurationSingleManagedGoal(t *testing.T) {
	srv, _ := autonomyRegressionServer(t)
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- putAutonomy(t, srv, map[string]any{"objective": "one", "acceptance": "done", "enabled": false}).Code
		}()
	}
	wg.Wait()
	close(results)
	for code := range results {
		if code >= 500 {
			t.Fatalf("concurrent PUT status=%d", code)
		}
	}
	managed := 0
	for _, g := range srv.goalStore.List() {
		if g.Managed && g.AgentID == "auto-reg" {
			managed++
		}
	}
	if managed != 1 {
		t.Fatalf("managed goals=%d", managed)
	}
}

func TestAutonomyRegressionDoesNotBindOrdinaryGoal(t *testing.T) {
	srv, _ := autonomyRegressionServer(t)
	ordinary, err := srv.goalStore.Create(goals.CreateInput{AgentID: "auto-reg", Objective: "ordinary", Acceptance: "ordinary"}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if w := putAutonomy(t, srv, map[string]any{"objective": "managed", "acceptance": "evidence", "enabled": false}); w.Code >= 500 {
		t.Fatalf("PUT=%d %s", w.Code, w.Body.String())
	}
	if g, ok := srv.goalStore.Get(ordinary.ID); !ok || g.Objective != "ordinary" || g.Managed {
		t.Fatalf("ordinary goal was rebound: %+v", g)
	}
}

func TestAutonomyRegressionRunningGoalRejectsWithoutMutation(t *testing.T) {
	srv, _ := autonomyRegressionServer(t)
	g, err := srv.goalStore.CreateManaged(goals.CreateInput{AgentID: "auto-reg", Objective: "old", Acceptance: "old"}, time.Now().UTC(), true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = srv.goalStore.StartRun(g.ID, "test", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	w := putAutonomy(t, srv, map[string]any{"objective": "new", "acceptance": "new", "enabled": false})
	if w.Code < 400 || w.Code >= 500 {
		t.Fatalf("running PUT=%d", w.Code)
	}
	after, _ := srv.goalStore.Get(g.ID)
	if after.Objective != "old" || after.Acceptance != "old" {
		t.Fatalf("running goal mutated: %+v", after)
	}
}

func TestAutonomyRegressionIntervalUpdatesTrigger(t *testing.T) {
	srv, _ := autonomyRegressionServer(t)
	if w := putAutonomy(t, srv, map[string]any{"objective": "x", "acceptance": "y", "enabled": false, "min_wake_interval_seconds": 120}); w.Code >= 400 {
		t.Fatalf("create=%d %s", w.Code, w.Body.String())
	}
	w := putAutonomy(t, srv, map[string]any{"min_wake_interval_seconds": 600, "enabled": false})
	if w.Code != http.StatusOK {
		t.Fatalf("update=%d %s", w.Code, w.Body.String())
	}
	g, _ := srv.goalStore.Get(jsonGoalID(w.Body.Bytes()))
	if g.TriggerID == "" {
		t.Fatal("missing trigger")
	}
	tr, ok := srv.triggerStore.GetTrigger(g.TriggerID)
	if !ok {
		t.Fatal("trigger missing")
	}
	switch got := tr.Condition["interval_seconds"].(type) {
	case int:
		if got != 600 {
			t.Fatalf("trigger interval=%v", got)
		}
	case float64:
		if got != 600 {
			t.Fatalf("trigger interval=%v", got)
		}
	default:
		t.Fatalf("trigger interval type=%T value=%v", got, got)
	}
}

func TestAutonomyRegressionTriggerFailureDoesNotClaimSuccess(t *testing.T) {
	srv, _ := autonomyRegressionServer(t)
	now := time.Now().UTC()
	g, err := srv.goalStore.CreateManaged(goals.CreateInput{AgentID: "auto-reg", Objective: "old", Acceptance: "old"}, now, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = srv.goalStore.BindTrigger(g.ID, "missing-trigger", now); err != nil {
		t.Fatal(err)
	}
	if _, err = srv.goalStore.UpdateAutonomyIntent(g.ID, g.Objective, g.Acceptance, g.NextWakeAt, now); err != nil {
		t.Fatal(err)
	}
	// The store has no public trigger-id mutator; this callback path still
	// exercises the failure contract when a persisted managed goal is orphaned.
	got, err := srv.autonomyToolUpdate(context.Background(), "auto-reg", tools.AutonomyUpdate{Objective: autonomyStrPtr("new")})
	if err == nil || got != nil {
		t.Fatalf("orphan trigger update got=%v err=%v", got, err)
	}
	after, _ := srv.goalStore.Get(g.ID)
	if after.Objective != "old" {
		t.Fatalf("goal changed after trigger failure: %q", after.Objective)
	}
}

func autonomyStrPtr(s string) *string { return &s }
func jsonGoalID(raw []byte) string {
	var v struct {
		GoalID string `json:"goal_id"`
	}
	_ = json.Unmarshal(raw, &v)
	return v.GoalID
}
