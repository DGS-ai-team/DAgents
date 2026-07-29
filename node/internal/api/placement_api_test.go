package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestPlacementAPI_PeersEmptyWithoutManage(t *testing.T) {
	cfg := &config.Config{NodeID: "node-owner", FSRoot: t.TempDir()}
	cfg.ApplyDefaults()
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())

	req := httptest.NewRequest(http.MethodGet, "/v1/peers/nodes", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["self_node_id"] != "node-owner" {
		t.Fatalf("self = %#v", body["self_node_id"])
	}
	nodes, _ := body["nodes"].([]any)
	if len(nodes) != 0 {
		t.Fatalf("nodes = %#v", nodes)
	}
}

func TestPlacementAPI_PeersDegradedWhenManageUnreachable(t *testing.T) {
	cfg := &config.Config{NodeID: "node-owner", FSRoot: t.TempDir()}
	cfg.Manage.Enabled = true
	cfg.Manage.URL = "http://127.0.0.1:9"
	cfg.ApplyDefaults()
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())

	req := httptest.NewRequest(http.MethodGet, "/v1/peers/nodes", nil)
	rr := httptest.NewRecorder()
	start := time.Now()
	srv.Handler().ServeHTTP(rr, req)
	if elapsed := time.Since(start); elapsed > 6*time.Second {
		t.Fatalf("peers took too long: %s", elapsed)
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["peers_degraded"] != true {
		t.Fatalf("expected peers_degraded, body=%#v", body)
	}
	nodes, _ := body["nodes"].([]any)
	if len(nodes) != 0 {
		t.Fatalf("nodes = %#v", nodes)
	}
}

func TestPlacementAPI_InternalCreateDelete(t *testing.T) {
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
	if rr.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rr.Code, rr.Body.String())
	}
	var created agentView
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.AgentID == "" || created.DisplayName != "远端实例" {
		t.Fatalf("created = %+v", created)
	}
	var place placementPayload
	if err := json.Unmarshal(created.Placement, &place); err != nil {
		t.Fatal(err)
	}
	if place.Role != "home" || place.OwnerNodeID != "node-owner" || place.HomeNodeID != "node-home" {
		t.Fatalf("placement = %+v", place)
	}
	var host hostPayload
	if err := json.Unmarshal(created.Host, &host); err != nil {
		t.Fatal(err)
	}
	if host.OSKind == "" || host.DisplayLabel == "" {
		t.Fatalf("host = %+v", host)
	}

	// 错误 owner 不可删
	req = httptest.NewRequest(http.MethodDelete, "/v1/internal/placement/agents/"+created.AgentID, nil)
	req.Header.Set(placementControlHeader, "1")
	req.Header.Set(tokenHeader, "tok-home")
	req.Header.Set(ownerNodeHeader, "other-owner")
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("forbidden delete status=%d body=%s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/v1/internal/placement/agents/"+created.AgentID, nil)
	req.Header.Set(placementControlHeader, "1")
	req.Header.Set(tokenHeader, "tok-home")
	req.Header.Set(ownerNodeHeader, "node-owner")
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rr.Code, rr.Body.String())
	}

	// 无 control header 拒绝
	req = httptest.NewRequest(http.MethodPost, "/v1/internal/placement/agents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status=%d", rr.Code)
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
