package browser

import (
	"context"
	"sync"
	"testing"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestManagerViewUsesReadOnlyOpAndRequiresStartedAgentSession(t *testing.T) {
	enabled := true
	called := false
	m := &Manager{cfg: testBrowserConfig(enabled), sessions: map[string]struct{}{"agent-1": {}}, locks: map[string]*sync.Mutex{}, driver: &MockDriver{Handler: func(_ context.Context, req Request) (Response, error) {
		called = true
		if req.Op != "view" || req.SessionKey != "agent-1" || req.PageID != "page-2" || req.TimeoutMS != 5000 {
			t.Fatalf("view request = %+v", req)
		}
		return Response{OK: true, Detail: map[string]any{"page_id": "page-2", "pages": []any{}, "mime": "image/png", "image_base64": "cG5n"}}, nil
	}}}
	out, err := m.View(context.Background(), "agent-1", "page-2")
	if err != nil || !out.OK || out.Detail["page_id"] != "page-2" || !called {
		t.Fatalf("View = %+v, err=%v, called=%v", out, err, called)
	}
	called = false
	out, err = m.View(context.Background(), "agent-unknown", "")
	if err != nil || out.OK || out.ErrorCode != "browser_not_started" || called {
		t.Fatalf("unstarted View = %+v, err=%v, called=%v", out, err, called)
	}
}

func TestManagerViewReturnsBusyWithoutWaitingForAgentBrowserLock(t *testing.T) {
	enabled := true
	called := false
	m := &Manager{cfg: testBrowserConfig(enabled), sessions: map[string]struct{}{"agent-1": {}}, locks: map[string]*sync.Mutex{}, driver: &MockDriver{Handler: func(_ context.Context, req Request) (Response, error) {
		called = true
		return Response{OK: true}, nil
	}}}
	lock := m.sessionLock("agent-1")
	lock.Lock()
	defer lock.Unlock()
	out, err := m.View(context.Background(), "agent-1", "")
	if err != nil || out.OK || out.ErrorCode != "browser_busy" || called {
		t.Fatalf("busy View = %+v, err=%v, called=%v", out, err, called)
	}
}

func TestManagerControlLimitsActionsAndReturnsBusyWithoutWaiting(t *testing.T) {
	enabled := true
	called := false
	m := &Manager{cfg: testBrowserConfig(enabled), sessions: map[string]struct{}{"agent-1": {}}, locks: map[string]*sync.Mutex{}, driver: &MockDriver{Handler: func(_ context.Context, req Request) (Response, error) {
		called = true
		if req.Op != "call" || req.SessionKey != "agent-1" || len(req.Actions) != 1 || req.Actions[0].Op != "navigate" || req.Actions[0].PageID != "page-1" {
			t.Fatalf("control request = %+v", req)
		}
		return Response{OK: true, Detail: map[string]any{"status": "succeeded"}}, nil
	}}}
	out, err := m.Control(context.Background(), "agent-1", "page-1", Action{Op: "navigate", Params: map[string]any{"url": "https://example.test"}})
	if err != nil || !out.OK || !called {
		t.Fatalf("Control = %+v, err=%v, called=%v", out, err, called)
	}
	called = false
	if out, err = m.Control(context.Background(), "agent-1", "", Action{Op: "evaluate"}); err != nil || out.OK || out.ErrorCode != "invalid_action" || called {
		t.Fatalf("unsupported Control = %+v, err=%v, called=%v", out, err, called)
	}
	lock := m.sessionLock("agent-1")
	lock.Lock()
	defer lock.Unlock()
	if out, err = m.Control(context.Background(), "agent-1", "page-1", Action{Op: "navigate"}); err != nil || out.OK || out.ErrorCode != "browser_busy" || called {
		t.Fatalf("busy Control = %+v, err=%v, called=%v", out, err, called)
	}
}

func TestManagerStartStopWithMockDriver(t *testing.T) {
	mock := &MockDriver{
		Handler: func(_ context.Context, req Request) (Response, error) {
			switch req.Op {
			case "call":
				if len(req.Actions) != 1 || (req.Actions[0].Op != "start" && req.Actions[0].Op != "stop") {
					return Response{OK: false, Error: "unexpected lifecycle action"}, nil
				}
				return Response{OK: true, URL: "about:blank", Title: ""}, nil
			default:
				return Response{OK: false, Error: "unexpected " + req.Op}, nil
			}
		},
	}
	enabled := true
	m := &Manager{
		cfg:      testBrowserConfig(enabled),
		driver:   mock,
		sessions: make(map[string]struct{}),
	}
	if !m.Enabled() {
		t.Fatal("expected enabled manager")
	}
	out, err := m.Start(context.Background(), "sess-1", nil, 1280, 720)
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK {
		t.Fatalf("start = %+v", out)
	}
	out, err = m.Stop(context.Background(), "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK {
		t.Fatalf("stop = %+v", out)
	}
	_ = m.Close()
	if !mock.Closed {
		t.Fatal("expected mock driver closed")
	}
}

func TestSessionScreenshotPath(t *testing.T) {
	abs, rel, err := SessionScreenshotPath(t.TempDir(), "browser", "sess/a", "shot 1")
	if err != nil {
		t.Fatal(err)
	}
	if rel != "browser/sess-a/shot-1.png" {
		t.Fatalf("rel = %q", rel)
	}
	if abs == "" {
		t.Fatal("empty abs")
	}
}

func testBrowserConfig(enabled bool) config.BrowserConfig {
	return config.BrowserConfig{
		Enabled:           &enabled,
		DefaultTimeoutMS:  5000,
		OutputDir:         "browser",
		MaxSessions:       2,
		AllowedURLSchemes: []string{"https", "http"},
	}
}
