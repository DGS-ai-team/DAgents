package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	var desktopAction browser.Action
	enabled := true
	manager, err := browser.NewManager(&config.Config{Browser: config.BrowserConfig{Enabled: &enabled, DefaultTimeoutMS: 5000}}, &browser.MockDriver{Handler: func(_ context.Context, req browser.Request) (browser.Response, error) {
		if req.Op == "call" {
			if len(req.Actions) > 0 {
				desktopAction = req.Actions[0]
			}
			return browser.Response{OK: true}, nil
		}
		called = true
		if req.Op != "view" || req.SessionKey != "agent-1" {
			t.Fatalf("view request = %+v", req)
		}
		if req.PageID != "" && req.PageID != "page-1" {
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
	missing := httptest.NewRecorder()
	srv.Handler().ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/v1/agents/unknown/browser/view", nil))
	if missing.Code != http.StatusNotFound || called {
		t.Fatalf("missing agent status=%d called=%v body=%s", missing.Code, called, missing.Body.String())
	}
	start := httptest.NewRecorder()
	srv.Handler().ServeHTTP(start, httptest.NewRequest(http.MethodPost, "/v1/agents/agent-1/browser/action", strings.NewReader(`{"action":"start"}`)))
	if start.Code != http.StatusOK || desktopAction.Op != "start" {
		t.Fatalf("start status=%d action=%+v body=%s", start.Code, desktopAction, start.Body.String())
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

	navigate := httptest.NewRecorder()
	srv.Handler().ServeHTTP(navigate, httptest.NewRequest(http.MethodPost, "/v1/agents/agent-1/browser/action", strings.NewReader(`{"action":"navigate","page_id":"page-1","url":"example.test/docs"}`)))
	if navigate.Code != http.StatusOK || desktopAction.Op != "navigate" || desktopAction.Params["url"] != "https://example.test/docs" {
		t.Fatalf("navigate status=%d action=%+v body=%s", navigate.Code, desktopAction, navigate.Body.String())
	}
	invalidURL := httptest.NewRecorder()
	srv.Handler().ServeHTTP(invalidURL, httptest.NewRequest(http.MethodPost, "/v1/agents/agent-1/browser/action", strings.NewReader(`{"action":"navigate","url":"javascript:alert(1)"}`)))
	if invalidURL.Code != http.StatusBadRequest {
		t.Fatalf("unsafe URL status=%d body=%s", invalidURL.Code, invalidURL.Body.String())
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

func TestNormalizeDesktopBrowserURL(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"example.com/path", "https://example.com/path"},
		{"http://127.0.0.1:8080", "http://127.0.0.1:8080"},
	} {
		got, err := normalizeDesktopBrowserURL(tc.raw)
		if err != nil || got != tc.want {
			t.Fatalf("normalizeDesktopBrowserURL(%q) = %q, %v; want %q", tc.raw, got, err, tc.want)
		}
	}
	for _, raw := range []string{"", "javascript:alert(1)", "file:///tmp/page", "https://user:pass@example.com"} {
		if _, err := normalizeDesktopBrowserURL(raw); err == nil {
			t.Fatalf("normalizeDesktopBrowserURL(%q) unexpectedly succeeded", raw)
		}
	}
}
