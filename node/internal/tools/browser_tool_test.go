package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/browser"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestBrowserToolsDisabledByDefault(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	for _, def := range reg.Definitions() {
		if strings.HasPrefix(def.Function.Name, "browser_") {
			t.Fatalf("unexpected browser tool %q", def.Function.Name)
		}
	}
}

func TestBrowserToolsWithMockManager(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	mock := &browser.MockDriver{
		Handler: func(_ context.Context, req browser.Request) (browser.Response, error) {
			switch req.Op {
			case "start":
				return browser.Response{OK: true, URL: "about:blank"}, nil
			case "navigate":
				return browser.Response{OK: true, URL: req.URL, Title: "Example"}, nil
			case "snapshot":
				resp := browser.Response{
					OK:    true,
					URL:   "https://example.com",
					Title: "Example",
					Detail: map[string]any{
						"llm_representation": "[1]<button>Login</button>",
						"elements": []any{
							map[string]any{"index": 1, "tag": "button", "text": "Login"},
						},
						"interactive_count": 1,
					},
				}
				if req.IncludeScreenshot && req.Path != "" {
					if err := os.WriteFile(req.Path, []byte("fake-png"), 0o644); err != nil {
						return browser.Response{OK: false, Error: err.Error()}, nil
					}
				}
				return resp, nil
			case "click":
				if req.Index != 7 {
					return browser.Response{OK: false, Error: "bad index"}, nil
				}
				return browser.Response{OK: true, Detail: map[string]any{"clicked_index": 7}}, nil
			case "click_coordinate":
				if req.CoordX != 10 || req.CoordY != 20 {
					return browser.Response{OK: false, Error: "bad coordinate"}, nil
				}
				return browser.Response{OK: true, Detail: map[string]any{"clicked_coordinate": map[string]any{"x": 10, "y": 20}}}, nil
			default:
				return browser.Response{OK: true}, nil
			}
		},
	}
	mgr, err := browser.NewManager(testBrowserConfig(t), mock)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetBrowserManager(mgr)

	names := map[string]bool{}
	for _, def := range reg.Definitions() {
		names[def.Function.Name] = true
	}
	for _, want := range []string{
		"browser_run_task", "browser_task_status", "browser_task_cancel",
		"browser_start", "browser_stop", "browser_navigate", "browser_click",
		"browser_fill", "browser_press", "browser_screenshot", "browser_wait",
		"browser_snapshot",
	} {
		if !names[want] {
			t.Fatalf("missing tool %q in definitions", want)
		}
	}
	if names["browser_click_coordinate"] {
		t.Fatal("browser_click_coordinate should be hidden when multimodal disabled")
	}

	ctx := WithSession(context.Background(), "sess-main")
	out, err := reg.Execute(ctx, "browser_start", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"ok":true`) {
		t.Fatalf("browser_start out = %s", out)
	}

	navArgs, _ := json.Marshal(map[string]string{"url": "https://example.com"})
	out, err = reg.Execute(ctx, "browser_navigate", string(navArgs))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "example.com") {
		t.Fatalf("navigate out = %s", out)
	}

	out, err = reg.Execute(ctx, "browser_snapshot", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	var snap map[string]any
	if err := json.Unmarshal([]byte(out), &snap); err != nil {
		t.Fatal(err)
	}
	if snap["llm_representation"] != "[1]<button>Login</button>" {
		t.Fatalf("snapshot llm_representation = %v", snap["llm_representation"])
	}
	if _, ok := snap["screenshot_path"]; ok {
		t.Fatalf("non-visual snapshot should not include screenshot_path: %s", out)
	}

	clickArgs, _ := json.Marshal(map[string]any{"index": 7})
	out, err = reg.Execute(ctx, "browser_click", string(clickArgs))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"clicked_index":7`) {
		t.Fatalf("click out = %s", out)
	}

	// 任务级派发：session → companion session_key
	mock.Handler = func(_ context.Context, req browser.Request) (browser.Response, error) {
		if req.Op == "start" {
			return browser.Response{OK: true}, nil
		}
		if req.Op == "run_task" {
			if req.SessionKey != "sess-main-browser" {
				return browser.Response{OK: false, Error: "bad session " + req.SessionKey}, nil
			}
			return browser.Response{OK: true, Detail: map[string]any{"task_id": "btask-1", "status": "queued"}}, nil
		}
		return browser.Response{OK: true}, nil
	}
	out, err = reg.Execute(ctx, "browser_run_task", `{"task":"open https://example.com","wait":false}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"task_id":"btask-1"`) {
		t.Fatalf("run_task out = %s", out)
	}
}

func TestBrowserToolsVisualMode(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetMultimodalEnabled(true)
	mock := &browser.MockDriver{
		Handler: func(_ context.Context, req browser.Request) (browser.Response, error) {
			switch req.Op {
			case "snapshot":
				if !req.IncludeScreenshot || req.Path == "" {
					return browser.Response{OK: false, Error: "expected include_screenshot"}, nil
				}
				if err := os.WriteFile(req.Path, []byte("fake-png"), 0o644); err != nil {
					return browser.Response{OK: false, Error: err.Error()}, nil
				}
				return browser.Response{
					OK:    true,
					URL:   "https://example.com",
					Title: "Example",
					Detail: map[string]any{
						"llm_representation": "[1]<button>Login</button>",
						"interaction":        "visual",
					},
				}, nil
			case "click_coordinate":
				return browser.Response{OK: true, Detail: map[string]any{"clicked_coordinate": map[string]any{"x": req.CoordX, "y": req.CoordY}}}, nil
			default:
				return browser.Response{OK: true}, nil
			}
		},
	}
	mgr, err := browser.NewManager(testBrowserConfigWithRoot(t, dir), mock)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetBrowserManager(mgr)

	names := map[string]bool{}
	for _, def := range reg.Definitions() {
		names[def.Function.Name] = true
	}
	if !names["browser_click_coordinate"] {
		t.Fatal("expected browser_click_coordinate in visual mode")
	}

	ctx := WithToolCallID(WithSession(context.Background(), "sess-visual"), "call-browser-snap")
	out, err := reg.Execute(ctx, "browser_snapshot", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	var snap map[string]any
	if err := json.Unmarshal([]byte(out), &snap); err != nil {
		t.Fatal(err)
	}
	if snap["screenshot_path"] == nil || snap["screenshot_path"] == "" {
		t.Fatalf("visual snapshot missing screenshot_path: %s", out)
	}
	payload := reg.TakeReadImageVisionForCall("call-browser-snap")
	if payload == nil {
		t.Fatal("expected vision payload after browser_snapshot")
	}
	if !strings.Contains(payload.Prompt, "browser_click_coordinate") {
		t.Fatalf("vision prompt = %q", payload.Prompt)
	}

	coordArgs, _ := json.Marshal(map[string]any{"x": 10, "y": 20})
	out, err = reg.Execute(ctx, "browser_click_coordinate", string(coordArgs))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"clicked_coordinate"`) {
		t.Fatalf("click_coordinate out = %s", out)
	}
}

func testBrowserConfig(t *testing.T) *config.Config {
	return testBrowserConfigWithRoot(t, t.TempDir())
}

func testBrowserConfigWithRoot(t *testing.T, fsRoot string) *config.Config {
	t.Helper()
	enabled := true
	cfg := &config.Config{
		FSRoot: fsRoot,
		Browser: config.BrowserConfig{
			Enabled:           &enabled,
			DefaultTimeoutMS:  5000,
			OutputDir:         "browser",
			MaxSessions:       2,
			AllowedURLSchemes: []string{"https", "http"},
		},
	}
	cfg.ApplyDefaults()
	return cfg
}

func TestBrowserRunTaskCompanionMissing(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	mock := &browser.MockDriver{
		Handler: func(_ context.Context, req browser.Request) (browser.Response, error) {
			return browser.Response{OK: true}, nil
		},
	}
	mgr, err := browser.NewManager(testBrowserConfig(t), mock)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetBrowserManager(mgr)
	reg.SetBrowserCompanionExists(func(_ context.Context, id string) (bool, error) {
		if id != "sess-main-browser" {
			t.Fatalf("id=%q", id)
		}
		return false, nil
	})
	ctx := WithSession(context.Background(), "sess-main")
	out, err := reg.Execute(ctx, "browser_run_task", `{"task":"x","wait":false}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "伴生不存在") {
		t.Fatalf("out=%s", out)
	}
}
