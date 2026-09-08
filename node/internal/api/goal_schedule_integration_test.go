package api

import (
	"context"
	"encoding/json"
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
	fake := &goalWakeLLM{called: make(chan struct{}, 4), release: make(chan struct{})}
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
	nd, _ := srv.triggerStore.GetTrigger(g.TriggerID)
	if nd.NextFireAt == nil {
		t.Fatal("missing trigger next fire")
	}
	srv.triggerSched.RunOnceForTest(context.Background(), time.Unix(int64(*nd.NextFireAt), 0).Add(time.Second))
	select {
	case <-fake.called:
	case <-time.After(5 * time.Second):
		t.Fatal("second scheduled LLM call missing")
	}
	if len(srv.goalStore.Runs(g.ID)) != 2 {
		t.Fatalf("runs=%d", len(srv.goalStore.Runs(g.ID)))
	}
}
