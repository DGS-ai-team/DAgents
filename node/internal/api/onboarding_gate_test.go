package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestOnboardingGate_blocksBusinessAPIs(t *testing.T) {
	cfg := testConfig(t)
	cfg.LLM.Mock = true
	done := false
	cfg.Onboarding.NodeProfileCompleted = &done
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())

	// 放行：bootstrap / setup / health / probe-models
	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodGet, "/health"},
		{http.MethodGet, "/v1/ui/bootstrap"},
		{http.MethodGet, "/v1/setup/config"},
		{http.MethodPost, "/v1/setup/llm/probe-models"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rr := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, req)
		if rr.Code == http.StatusForbidden {
			t.Fatalf("%s %s should be allowed before onboarding, got %d body=%s", tc.method, tc.path, rr.Code, rr.Body.String())
		}
	}

	// 拦截：agents / messages / llm / agent info
	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodGet, "/v1/agents"},
		{http.MethodGet, "/v1/agent/info"},
		{http.MethodGet, "/v1/llm/settings"},
		{http.MethodPost, "/v1/messages"},
		{http.MethodGet, "/v1/streams"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rr := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("%s %s want 403, got %d body=%s", tc.method, tc.path, rr.Code, rr.Body.String())
		}
		var body map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &body)
		errObj, _ := body["error"].(map[string]any)
		if errObj == nil || errObj["code"] != "node_profile_required" {
			t.Fatalf("%s %s error=%v", tc.method, tc.path, body)
		}
	}
}

func TestOnboardingGate_allowsAfterComplete(t *testing.T) {
	cfg := testConfig(t)
	cfg.LLM.Mock = true
	// nil onboarding → legacy completed
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	req := httptest.NewRequest(http.MethodGet, "/v1/agents", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code == http.StatusForbidden {
		t.Fatalf("completed profile should allow agents list, got 403: %s", rr.Body.String())
	}
}

func TestOnboardingPathAllowed(t *testing.T) {
	if !onboardingPathAllowed(http.MethodGet, "/ui/assets/app.js") {
		t.Fatal("ui assets should be allowed")
	}
	if !onboardingPathAllowed(http.MethodPost, "/v1/setup/llm/probe-models") {
		t.Fatal("probe-models should be allowed during onboarding")
	}
	if onboardingPathAllowed(http.MethodGet, "/v1/agents") {
		t.Fatal("agents should be blocked")
	}
}
