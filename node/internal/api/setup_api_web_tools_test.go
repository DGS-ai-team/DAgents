package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/setup"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestSetupWebToolsSecretAndLoadedRuntimeRefresh(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "synthetic-llm-test-key")
	ctx := context.Background()
	var mu sync.Mutex
	var authHeaders []string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		mu.Unlock()
		if r.URL.Path != "/v1/tools/search" {
			t.Errorf("provider path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"results":[{"title":"Example","url":"https://example.test/","snippet":"s","summary":"s","content":"c","authority":5,"rerank_score":0.5}]}`))
	}))
	defer provider.Close()

	cfg := testConfig(t)
	cfg.LLM = config.LLMConfig{Provider: "mock", Model: "mock", Mock: true}
	cfg.ApplyDefaults()
	dir := cfg.RuntimeDir()
	nodeSettings, err := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer nodeSettings.Close()
	if err := nodeSettings.Save(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	llmConfigs, err := store.OpenLLMConfigs(cfg.LLMConfigsDBPath(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer llmConfigs.Close()
	if err := store.EnsureDefaultLLMConfig(ctx, llmConfigs, cfg); err != nil {
		t.Fatal(err)
	}
	agents, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer agents.Close()
	if err := agents.Save(ctx, store.AgentRecord{AgentID: "web-loaded", DisplayName: "loaded", ConfigSnapshot: json.RawMessage(`{"agent_type":"normal","defaults":{"tools":{"enabled_groups":["web_search"]}}}`), CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := agents.Save(ctx, store.AgentRecord{AgentID: "web-unloaded", DisplayName: "unloaded", ConfigSnapshot: json.RawMessage(`{"agent_type":"normal","defaults":{"tools":{"enabled_groups":["web_search"]}}}`), CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	srv := NewServer(cfg, nil, WithSkipStore(), WithNodeSettings(nodeSettings), WithLLM(&llm.MockClient{}))
	srv.llmConfigs = llmConfigs
	srv.agents = agents
	t.Cleanup(func() { srv.Close() })
	if err := srv.ensureAgentRuntime(ctx, "web-loaded"); err != nil {
		t.Fatal(err)
	}
	if srv.sessions.Get("web-unloaded") != nil {
		t.Fatal("setup should not pre-load the second Agent")
	}
	if defs := srv.sessions.SessionTools("web-loaded").Definitions(); hasAPITool(defs, "web_search") {
		t.Fatal("web_search visible before global enable")
	}

	patch := func(body string) setup.SettingsView {
		t.Helper()
		req := httptest.NewRequest(http.MethodPatch, "/v1/settings", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("settings PATCH status=%d body=%s", rec.Code, rec.Body.String())
		}
		var view setup.SettingsView
		if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(rec.Body.String(), "synthetic-web-key") || strings.Contains(rec.Body.String(), "synthetic-web-key-2") {
			t.Fatal("settings response leaked API key")
		}
		return view
	}
	callSearch := func(wantKey string) {
		t.Helper()
		if err := srv.ensureAgentRuntime(ctx, "web-loaded"); err != nil {
			t.Fatal(err)
		}
		reg := srv.sessions.SessionTools("web-loaded")
		if reg == nil || !hasAPITool(reg.Definitions(), "web_search") {
			t.Fatal("enabled Agent does not have web_search schema")
		}
		if _, err := reg.Execute(ctx, "web_search", `{"query":"example","call_purpose":"lookup"}`); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		got := authHeaders[len(authHeaders)-1]
		mu.Unlock()
		if got != "Bearer "+wantKey {
			t.Fatalf("provider Authorization=%q, want replaced key", got)
		}
	}

	patch(`{"features":{"web_search_enabled":true},"web":{"base_url":"` + provider.URL + `","api_key":"synthetic-web-key"}}`)
	callSearch("synthetic-web-key")
	saved, err := nodeSettings.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Web.APIKey != "" || saved.Web.APIKeyCiphertext == "" || strings.Contains(saved.Web.APIKeyCiphertext, "synthetic-web-key") {
		t.Fatal("web key was not stored as ciphertext")
	}
	box, err := store.OpenSecretBox(dir)
	if err != nil {
		t.Fatal(err)
	}
	restoredKey, err := box.Decrypt(saved.Web.APIKeyCiphertext)
	if err != nil || restoredKey != "synthetic-web-key" {
		t.Fatalf("restored key=%q err=%v", restoredKey, err)
	}

	patch(`{"web":{"api_key":"synthetic-web-key-2"}}`)
	callSearch("synthetic-web-key-2")

	// Rebuild a Node from the same persisted settings and SecretBox, then verify
	// that the active key is restored into a newly built Agent runtime.
	restartedConfig := testConfig(t)
	restartedConfig.RuntimeRoot = cfg.RuntimeRoot
	restartedConfig.ApplyDefaults()
	stored, err := nodeSettings.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	store.OverlayNodeSettings(restartedConfig, stored)
	restarted := NewServer(restartedConfig, nil, WithSkipStore(), WithNodeSettings(nodeSettings), WithLLM(&llm.MockClient{}))
	restarted.llmConfigs = llmConfigs
	restarted.agents = agents
	t.Cleanup(func() { restarted.Close() })
	if err := restarted.ensureAgentRuntime(ctx, "web-loaded"); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.sessions.SessionTools("web-loaded").Execute(ctx, "web_search", `{"query":"restored"}`); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := authHeaders[len(authHeaders)-1]
	mu.Unlock()
	if got != "Bearer synthetic-web-key-2" {
		t.Fatalf("restarted provider Authorization=%q", got)
	}
	if restarted.sessions.Get("web-unloaded") != nil {
		t.Fatal("runtime refresh must not instantiate unloaded Agents")
	}

	patch(`{"features":{"web_search_enabled":false}}`)
	if err := srv.ensureAgentRuntime(ctx, "web-loaded"); err != nil {
		t.Fatal(err)
	}
	reg := srv.sessions.SessionTools("web-loaded")
	if hasAPITool(reg.Definitions(), "web_search") {
		t.Fatal("web_search schema remained after global disable")
	}
	if _, err := reg.Execute(ctx, "web_search", `{"query":"bypass"}`); err == nil {
		t.Fatal("web_search remained executable after global disable")
	}
}

func hasAPITool(defs []tools.ToolDef, name string) bool {
	for _, def := range defs {
		if def.Function.Name == name {
			return true
		}
	}
	return false
}
