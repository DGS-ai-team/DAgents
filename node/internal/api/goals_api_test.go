package api

import (
	"bytes"
	"encoding/json"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/shared/config"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGoalCreateRejectsUnknownAgentWithoutOrphan(t *testing.T) {
	cfg := testConfig(t)
	as, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer as.Close()
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	if srv.triggerSched != nil {
		srv.triggerSched.Stop()
	}
	srv.agents = as
	defer srv.sessions.Stop()
	b, _ := json.Marshal(map[string]any{"objective": "x", "acceptance": "y", "agent_id": "missing"})
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/goals", bytes.NewReader(b)))
	if rr.Code != 400 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if len(srv.goalStore.List()) != 0 {
		t.Fatal("orphan goal persisted")
	}
}

func TestGoalCreateRejectsArchivedAgentWithoutOrphan(t *testing.T) {
	cfg := testConfig(t)
	cfg.Onboarding.NodeProfileCompleted = true
	as, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer as.Close()
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	if srv.triggerSched != nil {
		srv.triggerSched.Stop()
	}
	defer srv.sessions.Stop()
	srv.agents = as
	_ = as.Save(t.Context(), store.AgentRecord{AgentID: "archived", Archived: true, ConfigSnapshot: json.RawMessage(`{}`)})
	b, _ := json.Marshal(map[string]any{"objective": "x", "acceptance": "y", "agent_id": "archived"})
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/goals", bytes.NewReader(b)))
	if rr.Code != 400 {
		t.Fatalf("status=%d", rr.Code)
	}
	if len(srv.goalStore.List()) != 0 {
		t.Fatal("orphan goal persisted")
	}
}

func TestGoalCreateRejectsNormalAgent(t *testing.T) {
	cfg := testConfig(t)
	as, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer as.Close()
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	if srv.triggerSched != nil {
		srv.triggerSched.Stop()
	}
	srv.agents = as
	defer srv.sessions.Stop()
	_ = as.Save(t.Context(), store.AgentRecord{AgentID: "normal", ConfigSnapshot: json.RawMessage(`{"agent_type":"normal"}`)})
	b, _ := json.Marshal(map[string]any{"objective": "x", "acceptance": "y", "agent_id": "normal"})
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/goals", bytes.NewReader(b)))
	if rr.Code != http.StatusConflict || len(srv.goalStore.List()) != 0 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestGoalPostAlwaysRequiresAutonomyEndpoint(t *testing.T) {
	cfg := testConfig(t)
	as, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer as.Close()
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	if srv.triggerSched != nil {
		srv.triggerSched.Stop()
	}
	srv.agents = as
	defer srv.sessions.Stop()
	now := time.Now()
	for id, typ := range map[string]string{"auto-post": "auto", "empty-post": ""} {
		_ = as.Save(t.Context(), store.AgentRecord{AgentID: id, ConfigSnapshot: json.RawMessage(`{"agent_type":"` + typ + `"}`), CreatedAt: now, UpdatedAt: now})
		b, _ := json.Marshal(map[string]any{"objective": "x", "acceptance": "y", "agent_id": id})
		rr := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/goals", bytes.NewReader(b)))
		if rr.Code != http.StatusConflict {
			t.Fatalf("%s status=%d", id, rr.Code)
		}
	}
	if len(srv.goalStore.List()) != 0 {
		t.Fatal("POST created a Goal")
	}
}

var _ = config.Config{}
