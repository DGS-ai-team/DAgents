package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestAgentModelViewReturnsEffectiveProfileAndSafeCatalog(t *testing.T) {
	cfg := testConfig(t)
	cfg.LLM.Profiles = map[string]config.LLMProfileConfig{
		"default": {Provider: "openai", Model: "gpt-default", APIKeyEnv: "OPENAI_API_KEY"},
		"agent":   {Provider: "deepseek", Model: "deepseek-chat", APIKeyEnv: "DEEPSEEK_API_KEY", Mock: true},
	}
	cfg.LLM.ProfileOrder = []string{"default", "agent"}
	cfg.LLM.Active = "default"
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	if srv.triggerSched != nil {
		srv.triggerSched.Stop()
	}
	as, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	srv.agents = as
	t.Cleanup(func() {
		if srv.sessions != nil {
			srv.sessions.Stop()
		}
		_ = as.Close()
	})
	if err := as.Save(context.Background(), store.AgentRecord{
		AgentID: "model-agent", ConfigSnapshot: json.RawMessage(`{"agent_type":"normal","defaults":{"llm":{"active":"agent"}}}`),
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/agents/model-agent/model-view", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var payload agentModelViewResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Agent.AgentID != "model-agent" || payload.LLMSettings.ActiveProfile != "agent" || payload.LLMSettings.Provider != "deepseek" || payload.LLMSettings.Model != "deepseek-chat" {
		t.Fatalf("unexpected effective model view: %+v", payload)
	}
	if len(payload.LLMSettings.Profiles) != 2 || len(payload.ProfileCatalog) != 2 {
		t.Fatalf("profile choices missing: settings=%v catalog=%v", payload.LLMSettings.Profiles, payload.ProfileCatalog)
	}
	for _, profile := range payload.ProfileCatalog {
		if profile.APIKey != "" || profile.APIKeyEnv == "" {
			t.Fatalf("catalog should omit secrets but preserve env metadata: %+v", profile)
		}
	}
}
