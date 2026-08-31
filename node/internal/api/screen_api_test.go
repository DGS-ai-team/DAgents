package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/screen"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestScreenAPI_Status(t *testing.T) {
	cfg := &config.Config{NodeID: "node-home", RuntimeRoot: t.TempDir()}
	cfg.ApplyDefaults()
	agentsDB, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer agentsDB.Close()
	_ = agentsDB.Save(context.Background(), store.AgentRecord{
		AgentID:        "agt-1",
		DisplayName:    "本地",
		Origin:         store.AgentOriginLocal,
		SandboxBackend: "process",
		ConfigSnapshot: json.RawMessage(`{}`),
	})
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	srv.agents = agentsDB

	req := httptest.NewRequest(http.MethodGet, "/v1/agents/agt-1/screen/status", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "display_available") {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestScreenAPI_StreamUnavailableOrFrames(t *testing.T) {
	cfg := &config.Config{NodeID: "node-home", RuntimeRoot: t.TempDir()}
	cfg.ApplyDefaults()
	agentsDB, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer agentsDB.Close()
	_ = agentsDB.Save(context.Background(), store.AgentRecord{
		AgentID:        "agt-1",
		DisplayName:    "本地",
		Origin:         store.AgentOriginLocal,
		SandboxBackend: "process",
		ConfigSnapshot: json.RawMessage(`{}`),
	})
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	srv.agents = agentsDB

	st := screen.DetectStatus()
	if !st.Available {
		req := httptest.NewRequest(http.MethodGet, "/v1/agents/agt-1/screen/stream", nil)
		rr := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("want 404 unavailable, got %d body=%s", rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "screen_unavailable") {
			t.Fatalf("body=%s", rr.Body.String())
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/agents/agt-1/screen/stream", nil)
	rr := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.Handler().ServeHTTP(rr, req)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		cancel()
		<-done
	}
	body := rr.Body.String()
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") && !strings.Contains(body, "event:") {
		t.Fatalf("expected sse, code=%d ct=%q body=%q", rr.Code, ct, body)
	}
}

func TestScreenAPI_RemoteStubRetired(t *testing.T) {
	cfg := &config.Config{NodeID: "node-owner", RuntimeRoot: t.TempDir()}
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
		HostJSON:       json.RawMessage(`{"display_available":true,"display_label":"Windows"}`),
	})
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	srv.agents = agentsDB
	req := httptest.NewRequest(http.MethodGet, "/v1/agents/agt-remote/screen/status", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "agent_not_found") {
		t.Fatalf("body=%s", rr.Body.String())
	}
}
