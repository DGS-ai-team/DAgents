package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/manage"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestEdgeUpgrade_RemoteAgentProxied(t *testing.T) {
	var gotPath atomic.Value
	manageSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/edge/sessions" && r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"edge_session_id": "edge_test_1",
				"home_node_id":    "node-home",
				"agent_id":        "agt-remote",
				"owner_node_id":   "node-owner",
				"scopes":          []string{"agent", "messages", "streams"},
				"expires_at":      time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
				"proxy_prefix":    "/v1/edge/edge_test_1/proxy",
			})
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/v1/edge/edge_test_1/proxy/v1/agents/agt-remote/hydrate" {
			gotPath.Store(r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"via":"manage-edge"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer manageSrv.Close()

	cfg := &config.Config{
		NodeID: "node-owner",
		FSRoot: t.TempDir(),
	}
	cfg.Manage.Enabled = true
	cfg.Manage.URL = manageSrv.URL
	cfg.ApplyDefaults()

	agentsDB, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer agentsDB.Close()

	rec := store.AgentRecord{
		AgentID:        "agt-remote",
		DisplayName:    "远端",
		Origin:         store.AgentOriginRemote,
		SandboxBackend: "process",
		ConfigSnapshot: json.RawMessage(`{}`),
		PlacementJSON:  json.RawMessage(`{"role":"owner_ref","owner_node_id":"node-owner","home_node_id":"node-home"}`),
		HostJSON:       json.RawMessage(`{"os_kind":"linux","display_label":"Linux"}`),
	}
	if err := agentsDB.Save(context.Background(), rec); err != nil {
		t.Fatal(err)
	}

	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	srv.agents = agentsDB
	srv.edge = manage.NewEdgeClient(cfg)

	req := httptest.NewRequest(http.MethodGet, "/v1/agents/agt-remote/hydrate", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if gotPath.Load() == nil {
		t.Fatal("manage proxy path not hit")
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("manage-edge")) {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestEdgeUpgrade_LocalAgentNotProxied(t *testing.T) {
	cfg := &config.Config{NodeID: "node-local", FSRoot: t.TempDir()}
	cfg.Manage.Enabled = true
	cfg.Manage.URL = "http://127.0.0.1:9"
	cfg.ApplyDefaults()
	agentsDB, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer agentsDB.Close()
	_ = agentsDB.Save(context.Background(), store.AgentRecord{
		AgentID:        "agt-local",
		DisplayName:    "本地",
		Origin:         store.AgentOriginLocal,
		SandboxBackend: "process",
		ConfigSnapshot: json.RawMessage(`{}`),
	})
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	srv.agents = agentsDB
	srv.edge = manage.NewEdgeClient(cfg)

	req := httptest.NewRequest(http.MethodGet, "/v1/agents/agt-local", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("local get status=%d body=%s", rr.Code, rr.Body.String())
	}
	var view agentView
	if err := json.Unmarshal(rr.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.AgentID != "agt-local" {
		t.Fatalf("view=%+v", view)
	}
}
