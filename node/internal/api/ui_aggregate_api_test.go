package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
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
	if out.Info.WorkgroupEnabled != cfg.ManageWorkgroupEnabled() {
		t.Fatalf("workgroup_enabled=%v want=%v", out.Info.WorkgroupEnabled, cfg.ManageWorkgroupEnabled())
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
