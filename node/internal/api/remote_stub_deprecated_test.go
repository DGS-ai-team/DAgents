package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestRemoteStub_HydrateDeprecated(t *testing.T) {
	cfg := &config.Config{NodeID: "node-owner", FSRoot: t.TempDir()}
	cfg.Manage.Enabled = true
	cfg.Manage.URL = "http://127.0.0.1:9"
	cfg.ApplyDefaults()
	agentsDB, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer agentsDB.Close()
	_ = agentsDB.Save(context.Background(), store.AgentRecord{
		AgentID:        "agt-remote",
		DisplayName:    "远端",
		Origin:         store.AgentOriginRemote,
		SandboxBackend: "process",
		ConfigSnapshot: json.RawMessage(`{}`),
		PlacementJSON:  json.RawMessage(`{"role":"owner_ref","owner_node_id":"node-owner","home_node_id":"node-home"}`),
	})
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	srv.agents = agentsDB

	req := httptest.NewRequest(http.MethodGet, "/v1/agents/agt-remote/hydrate", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusGone || !bytes.Contains(rr.Body.Bytes(), []byte("placement_deprecated")) {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRemoteStub_MessagesAndStreamsDeprecated(t *testing.T) {
	cfg := &config.Config{NodeID: "node-owner", FSRoot: t.TempDir()}
	cfg.Manage.Enabled = true
	cfg.Manage.URL = "http://127.0.0.1:9"
	cfg.ApplyDefaults()
	agentsDB, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer agentsDB.Close()
	_ = agentsDB.Save(context.Background(), store.AgentRecord{
		AgentID:        "agt-remote",
		DisplayName:    "远端",
		Origin:         store.AgentOriginRemote,
		SandboxBackend: "process",
		ConfigSnapshot: json.RawMessage(`{}`),
		PlacementJSON:  json.RawMessage(`{"role":"owner_ref","home_node_id":"node-home","owner_node_id":"node-owner"}`),
	})
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	srv.agents = agentsDB

	body, _ := json.Marshal(map[string]any{"agent_id": "agt-remote", "content": "hello", "request_type": "message"})
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusGone || !bytes.Contains(rr.Body.Bytes(), []byte("placement_deprecated")) {
		t.Fatalf("messages status=%d body=%s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/streams?agent_id=agt-remote", nil)
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusGone || !bytes.Contains(rr.Body.Bytes(), []byte("placement_deprecated")) {
		t.Fatalf("streams status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRemoteStub_EnsureDeprecated(t *testing.T) {
	cfg := &config.Config{NodeID: "node-owner", FSRoot: t.TempDir()}
	cfg.Manage.Enabled = false
	cfg.ApplyDefaults()
	agentsDB, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer agentsDB.Close()
	_ = agentsDB.Save(context.Background(), store.AgentRecord{
		AgentID:        "agt-remote",
		DisplayName:    "远端",
		Origin:         store.AgentOriginRemote,
		SandboxBackend: "process",
		ConfigSnapshot: json.RawMessage(`{}`),
		PlacementJSON:  json.RawMessage(`{"role":"owner_ref","home_node_id":"node-home"}`),
	})
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	srv.agents = agentsDB

	req := httptest.NewRequest(http.MethodPost, "/v1/agents/agt-remote/ensure", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusGone || !bytes.Contains(rr.Body.Bytes(), []byte("placement_deprecated")) {
		t.Fatalf("ensure status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestLocalAgent_GetStillWorks(t *testing.T) {
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

	req := httptest.NewRequest(http.MethodGet, "/v1/agents/agt-local", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("local get status=%d body=%s", rr.Code, rr.Body.String())
	}
}
