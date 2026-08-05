package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestUIBootstrap(t *testing.T) {
	cfg := testConfig(t)
	cfg.LLM.Mock = true
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	req := httptest.NewRequest(http.MethodGet, "/v1/ui/bootstrap", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var out uiBootstrapResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Health.Status != "ok" || out.Health.NodeID == "" {
		t.Fatalf("health=%+v", out.Health)
	}
	if out.Info.NodeID == "" {
		t.Fatalf("info=%+v", out.Info)
	}
	if !out.Onboarding.NodeProfileCompleted {
		t.Fatal("legacy/nil onboarding should report completed=true")
	}
}

func TestUIBootstrap_onboardingIncomplete(t *testing.T) {
	cfg := testConfig(t)
	cfg.LLM.Mock = true
	done := false
	cfg.Onboarding.NodeProfileCompleted = &done
	cfg.User.PreferredName = ""
	cfg.Agent.Name = "seed-node"
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	req := httptest.NewRequest(http.MethodGet, "/v1/ui/bootstrap", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var out uiBootstrapResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Onboarding.NodeProfileCompleted {
		t.Fatal("expected incomplete onboarding")
	}
	if out.Agent.Name != "seed-node" {
		t.Fatalf("agent=%+v", out.Agent)
	}
}


func TestWorkspaceActivity(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{NodeID: "node-test", FSRoot: filepath.Join(root, "runtime")}
	cfg.ApplyDefaults()
	cfg.LLM.Mock = true

	agentsDB, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agentsDB.Close() })

	userDir := cfg.AgentTemplatesDir()
	_ = os.MkdirAll(userDir, 0o755)
	_ = os.WriteFile(filepath.Join(userDir, "general.yaml"), []byte(`
id: general
display_name: 通用
defaults:
  tools:
    enabled_groups: [fs, bash, skills]
`), 0o644)

	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	srv.agents = agentsDB
	t.Cleanup(func() {
		if srv.sessions != nil {
			srv.sessions.Stop()
		}
	})

	body, _ := json.Marshal(map[string]any{"template_id": "general", "display_name": "Act"})
	req := httptest.NewRequest(http.MethodPost, "/v1/agents", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rr.Code, rr.Body.String())
	}
	var created agentView
	_ = json.Unmarshal(rr.Body.Bytes(), &created)

	req = httptest.NewRequest(http.MethodGet, "/v1/agents/"+created.AgentID+"/workspace-activity", nil)
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("activity status=%d body=%s", rr.Code, rr.Body.String())
	}
	var act workspaceActivityResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &act); err != nil {
		t.Fatal(err)
	}
	if act.AgentID != created.AgentID {
		t.Fatalf("agent_id=%q", act.AgentID)
	}
	if act.Files == nil || act.Commands == nil {
		t.Fatalf("nil slices: %+v", act)
	}
}
