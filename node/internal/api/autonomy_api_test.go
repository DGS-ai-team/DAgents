package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

func TestAgentAutonomyTypeAndLifecycle(t *testing.T) {
	cfg := testConfig(t)
	cfg.Onboarding.NodeProfileCompleted = true
	as, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer as.Close()
	fake := &goalWakeLLM{called: make(chan struct{}, 2), release: make(chan struct{})}
	srv := NewServer(cfg, nil, WithLLM(fake), WithSkipStore())
	defer srv.sessions.Stop()
	srv.agents = as
	defer func() {
		if srv.feedbackStore != nil {
			_ = srv.feedbackStore.Close()
		}
	}()
	now := time.Now().UTC()
	for _, rec := range []store.AgentRecord{
		{AgentID: "normal-a", DisplayName: "normal", ConfigSnapshot: json.RawMessage(`{"agent_type":"normal","defaults":{}}`), RuntimeRevision: 1, CreatedAt: now, UpdatedAt: now},
		{AgentID: "auto-a", DisplayName: "auto", ConfigSnapshot: json.RawMessage(`{"agent_type":"auto","defaults":{}}`), RuntimeRevision: 1, CreatedAt: now, UpdatedAt: now},
	} {
		if err := as.Save(context.Background(), rec); err != nil {
			t.Fatal(err)
		}
	}
	get := httptest.NewRecorder()
	srv.Handler().ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/v1/agents/normal-a/autonomy", nil))
	if get.Code != http.StatusConflict {
		t.Fatalf("normal autonomy=%d", get.Code)
	}
	get = httptest.NewRecorder()
	srv.Handler().ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/v1/agents/auto-a/autonomy", nil))
	if get.Code != http.StatusOK || !bytes.Contains(get.Body.Bytes(), []byte(`"status":"disabled"`)) {
		t.Fatalf("auto default=%d %s", get.Code, get.Body.String())
	}
	body, _ := json.Marshal(map[string]any{"objective": "watch", "acceptance": "evidence", "enabled": true, "max_runs": 4, "token_budget": 1000, "turn_token_budget": 100, "min_wake_interval_seconds": 60})
	put := httptest.NewRecorder()
	srv.Handler().ServeHTTP(put, httptest.NewRequest(http.MethodPut, "/v1/agents/auto-a/autonomy", bytes.NewReader(body)))
	if put.Code != http.StatusOK {
		t.Fatalf("put=%d %s", put.Code, put.Body.String())
	}
	var out struct {
		GoalID string `json:"goal_id"`
		Status string `json:"status"`
		Limits struct {
			TriggerID   string `json:"trigger_id"`
			TokenBudget int64  `json:"token_budget"`
		} `json:"limits"`
	}
	if err := json.Unmarshal(put.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.GoalID == "" || out.Status != "active" || out.Limits.TriggerID == "" {
		t.Fatalf("autonomy=%+v", out)
	}
	if out.Limits.TokenBudget != 1000 {
		t.Fatalf("budget=%d", out.Limits.TokenBudget)
	}
}
