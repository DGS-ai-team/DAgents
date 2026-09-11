package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

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

func TestBrowserToolsTaskOnlyWithMockManager(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	mock := &browser.MockDriver{
		Handler: func(_ context.Context, req browser.Request) (browser.Response, error) {
			switch req.Op {
			case "start":
				return browser.Response{OK: true}, nil
			case "run_task":
				if req.SessionKey != "sess-main-browser" {
					return browser.Response{OK: false, Error: "bad session " + req.SessionKey}, nil
				}
				return browser.Response{OK: true, Detail: map[string]any{"task_id": "btask-1", "status": "queued"}}, nil
			case "task_status":
				return browser.Response{OK: true, Detail: map[string]any{"task_id": "btask-1", "status": "running"}}, nil
			case "task_cancel":
				return browser.Response{OK: true, Detail: map[string]any{"task_id": "btask-1", "status": "cancelled"}}, nil
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
	for _, want := range []string{"browser_run_task", "browser_task_status", "browser_task_cancel"} {
		if !names[want] {
			t.Fatalf("missing tool %q in definitions", want)
		}
	}
	for name := range names {
		if strings.HasPrefix(name, "browser_") &&
			name != "browser_run_task" && name != "browser_task_status" && name != "browser_task_cancel" {
			t.Fatalf("retired granular tool still exposed: %q", name)
		}
	}

	ctx := WithSession(context.Background(), "sess-main")
	out, err := reg.Execute(ctx, "browser_run_task", `{"task":"open https://example.com","wait":false}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"task_id":"btask-1"`) {
		t.Fatalf("run_task out = %s", out)
	}

	statusArgs, _ := json.Marshal(map[string]string{"task_id": "btask-1"})
	out, err = reg.Execute(ctx, "browser_task_status", string(statusArgs))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"status":"running"`) {
		t.Fatalf("status out = %s", out)
	}

	out, err = reg.Execute(ctx, "browser_task_cancel", string(statusArgs))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"status":"cancelled"`) {
		t.Fatalf("cancel out = %s", out)
	}

	_, err = reg.Execute(ctx, "browser_navigate", `{"url":"https://example.com"}`)
	if err == nil {
		t.Fatal("expected browser_navigate to be unavailable")
	}
}

func TestBrowserRunTaskWaitFalseAutoCallbacksOnCompletion(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	statusCalls := 0
	mock := &browser.MockDriver{
		Handler: func(_ context.Context, req browser.Request) (browser.Response, error) {
			switch req.Op {
			case "start":
				return browser.Response{OK: true}, nil
			case "run_task":
				return browser.Response{OK: true, Detail: map[string]any{
					"task_id": "btask-auto",
					"status":  "queued",
				}}, nil
			case "task_status":
				statusCalls++
				if statusCalls == 1 {
					return browser.Response{OK: true, Detail: map[string]any{
						"task_id": "btask-auto",
						"status":  "running",
					}}, nil
				}
				return browser.Response{OK: true, Detail: map[string]any{
					"task_id": "btask-auto",
					"status":  "completed",
					"success": true,
					"summary": "Example Domain",
				}}, nil
			default:
				return browser.Response{OK: true}, nil
			}
		},
	}
	mgr, err := browser.NewManager(testBrowserConfig(t), mock)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	reg.SetBrowserManager(mgr)
	done := make(chan BrowserTaskDone, 1)
	reg.SetBrowserTaskNotifier(func(sessionID string, result BrowserTaskDone) {
		if sessionID != "sess-main" {
			t.Errorf("callback session_id=%q", sessionID)
		}
		done <- result
	})

	ctx := WithToolCallID(WithSession(context.Background(), "sess-main"), "call-browser-1")
	out, err := reg.Execute(ctx, "browser_run_task", `{"task":"open https://example.com","wait":false}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"task_id":"btask-auto"`) {
		t.Fatalf("run_task out=%s", out)
	}

	select {
	case result := <-done:
		if result.TaskID != "btask-auto" || result.ToolCallID != "call-browser-1" {
			t.Fatalf("callback identity=%+v", result)
		}
		if result.Status != "succeeded" || !strings.Contains(result.ResultText, "Example Domain") {
			t.Fatalf("callback result=%+v", result)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("timed out waiting for browser task callback")
	}
}

func testBrowserConfig(t *testing.T) *config.Config {
	return testBrowserConfigWithRoot(t, t.TempDir())
}

func testBrowserConfigWithRoot(t *testing.T, workspaceRoot string) *config.Config {
	t.Helper()
	enabled := true
	cfg := &config.Config{
		RuntimeRoot: workspaceRoot,
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
