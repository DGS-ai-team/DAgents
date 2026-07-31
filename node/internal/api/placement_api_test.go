package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestPlacementAPI_PeersRouteRemoved(t *testing.T) {
	cfg := &config.Config{NodeID: "node-owner", FSRoot: t.TempDir()}
	cfg.ApplyDefaults()
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())

	req := httptest.NewRequest(http.MethodGet, "/v1/peers/nodes", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s want 404", rr.Code, rr.Body.String())
	}
}

func TestPlacementAPI_InternalRoutesDeprecated(t *testing.T) {
	cfg := &config.Config{
		NodeID: "node-home",
		FSRoot: t.TempDir(),
	}
	cfg.Manage.NodeToken = "tok-home"
	cfg.ApplyDefaults()

	agentsDB, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer agentsDB.Close()

	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	srv.agents = agentsDB

	body, _ := json.Marshal(map[string]any{
		"display_name": "远端实例",
		"defaults":     map[string]any{},
		"placement": map[string]any{
			"role":          "home",
			"owner_node_id": "node-owner",
			"home_node_id":  "node-home",
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/internal/placement/agents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(placementControlHeader, "1")
	req.Header.Set(tokenHeader, "tok-home")
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusGone {
		t.Fatalf("create status=%d body=%s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/v1/internal/placement/agents/agt-123", nil)
	req.Header.Set(placementControlHeader, "1")
	req.Header.Set(tokenHeader, "tok-home")
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusGone {
		t.Fatalf("delete status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPlacementAPI_LocalCreateAttachesHost(t *testing.T) {
	cfg := &config.Config{NodeID: "node-local", FSRoot: t.TempDir()}
	cfg.ApplyDefaults()
	agentsDB, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer agentsDB.Close()
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	srv.agents = agentsDB

	body, _ := json.Marshal(map[string]any{
		"display_name": "本机",
		"defaults":     map[string]any{},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/agents", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var created agentView
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if len(created.Host) == 0 {
		t.Fatal("expected host snapshot")
	}
}
