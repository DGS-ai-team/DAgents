package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/browser"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestAgentBrowserViewRequiresStoredAgentAndStartedBrowser(t *testing.T) {
	cfg := &config.Config{NodeID: "node-browser-view", RuntimeRoot: t.TempDir()}
	cfg.ApplyDefaults()
	cfg.Onboarding.NodeProfileCompleted = true
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	t.Cleanup(func() { srv.sessions.Stop() })
	agents, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer agents.Close()
	if err := agents.Save(context.Background(), store.AgentRecord{AgentID: "agent-1", DisplayName: "Agent 1", ConfigSnapshot: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	srv.agents = agents

	called := false
	busyResponse := false
	enabled := true
	manager, err := browser.NewManager(&config.Config{Browser: config.BrowserConfig{Enabled: &enabled, DefaultTimeoutMS: 5000}}, &browser.MockDriver{Handler: func(_ context.Context, req browser.Request) (browser.Response, error) {
		if req.Op == "call" {
			return browser.Response{OK: true}, nil
		}
		called = true
		if req.Op != "view" || req.SessionKey != "agent-1" {
			t.Fatalf("view request = %+v", req)
		}
		if req.PageID != "page-1" {
			return browser.Response{OK: false, ErrorCode: "page_not_found", Error: "page does not belong to this browser session"}, nil
		}
		if busyResponse {
			return browser.Response{OK: false, ErrorCode: "browser_busy", Error: "Agent browser is currently in use"}, nil
		}
		return browser.Response{OK: true, Detail: map[string]any{"page_id": "page-1", "pages": []any{}, "title": "Test page", "url": "https://example.test", "mime": "image/png", "image_base64": "cG5n"}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	srv.browserMu.Lock()
	srv.browserMgr = manager
	srv.browserMu.Unlock()
	if out, err := manager.Start(context.Background(), "agent-1", nil, 1280, 720); err != nil || !out.OK {
		t.Fatalf("start = %+v, %v", out, err)
	}

	missing := httptest.NewRecorder()
	srv.Handler().ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/v1/agents/unknown/browser/view", nil))
	if missing.Code != http.StatusNotFound || called {
		t.Fatalf("missing agent status=%d called=%v body=%s", missing.Code, called, missing.Body.String())
	}

	response := httptest.NewRecorder()
	srv.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/agents/agent-1/browser/view?page_id=page-1", nil))
	if response.Code != http.StatusOK || !called {
		t.Fatalf("view status=%d called=%v body=%s", response.Code, called, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["page_id"] != "page-1" || payload["image_base64"] != "cG5n" {
		t.Fatalf("view payload = %#v", payload)
	}
	busyResponse = true
	busy := httptest.NewRecorder()
	srv.Handler().ServeHTTP(busy, httptest.NewRequest(http.MethodGet, "/v1/agents/agent-1/browser/view?page_id=page-1", nil))
	if busy.Code != http.StatusLocked {
		t.Fatalf("busy status=%d body=%s", busy.Code, busy.Body.String())
	}
	busyResponse = false
	wrongPage := httptest.NewRecorder()
	srv.Handler().ServeHTTP(wrongPage, httptest.NewRequest(http.MethodGet, "/v1/agents/agent-1/browser/view?page_id=other-agent-page", nil))
	if wrongPage.Code != http.StatusNotFound {
		t.Fatalf("cross-page status=%d body=%s", wrongPage.Code, wrongPage.Body.String())
	}

	srv.browserMu.Lock()
	srv.browserMgr = nil
	srv.browserMu.Unlock()
	disabled := httptest.NewRecorder()
	srv.Handler().ServeHTTP(disabled, httptest.NewRequest(http.MethodGet, "/v1/agents/agent-1/browser/view", nil))
	if disabled.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled status=%d body=%s", disabled.Code, disabled.Body.String())
	}
	srv.browserMu.Lock()
	srv.browserMgr = manager
	srv.browserMu.Unlock()
	if out, err := manager.Stop(context.Background(), "agent-1"); err != nil || !out.OK {
		t.Fatalf("stop = %+v, %v", out, err)
	}
	unstarted := httptest.NewRecorder()
	srv.Handler().ServeHTTP(unstarted, httptest.NewRequest(http.MethodGet, "/v1/agents/agent-1/browser/view", nil))
	if unstarted.Code != http.StatusConflict {
		t.Fatalf("unstarted status=%d body=%s", unstarted.Code, unstarted.Body.String())
	}
}
