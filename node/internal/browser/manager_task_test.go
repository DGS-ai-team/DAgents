package browser

import (
	"context"
	"testing"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestManagerRunTaskAutoStart(t *testing.T) {
	on := true
	mock := &MockDriver{
		Handler: func(_ context.Context, req Request) (Response, error) {
			switch req.Op {
			case "start":
				return Response{OK: true, URL: "about:blank"}, nil
			case "run_task":
				if req.SessionKey != "agt-1-browser" || req.Task != "open example.com" {
					t.Fatalf("unexpected req %+v", req)
				}
				return Response{OK: true, Detail: map[string]any{
					"task_id": "btask-abc",
					"status":  "queued",
				}}, nil
			case "task_status":
				return Response{OK: true, Detail: map[string]any{
					"task_id": req.TaskID,
					"status":  "completed",
				}}, nil
			case "task_cancel":
				return Response{OK: true, Detail: map[string]any{
					"task_id": req.TaskID,
					"status":  "cancelled",
				}}, nil
			default:
				return Response{OK: false, Error: "unexpected " + req.Op}, nil
			}
		},
	}
	cfg := &config.Config{
		Browser: config.BrowserConfig{
			Enabled:     &on,
			MaxSessions: 4,
			ServiceURL:  "http://127.0.0.1:18766",
		},
	}
	cfg.ApplyDefaults()
	mgr, err := NewManager(cfg, mock)
	if err != nil {
		t.Fatal(err)
	}
	out, err := mgr.RunTask(context.Background(), "agt-1-browser", "open example.com", 20)
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK || out.Detail["task_id"] != "btask-abc" {
		t.Fatalf("out=%+v", out)
	}
	st, err := mgr.TaskStatus(context.Background(), "agt-1-browser", "btask-abc")
	if err != nil || !st.OK {
		t.Fatalf("status=%+v err=%v", st, err)
	}
	cn, err := mgr.TaskCancel(context.Background(), "agt-1-browser", "btask-abc")
	if err != nil || !cn.OK {
		t.Fatalf("cancel=%+v err=%v", cn, err)
	}
}

func TestManagerRunTaskWait(t *testing.T) {
	on := true
	polls := 0
	mock := &MockDriver{
		Handler: func(_ context.Context, req Request) (Response, error) {
			switch req.Op {
			case "start":
				return Response{OK: true}, nil
			case "run_task":
				return Response{OK: true, Detail: map[string]any{"task_id": "btask-w", "status": "queued"}}, nil
			case "task_status":
				polls++
				if polls < 2 {
					return Response{OK: true, Detail: map[string]any{"task_id": "btask-w", "status": "running"}}, nil
				}
				return Response{OK: true, Detail: map[string]any{
					"task_id": "btask-w",
					"status":  "completed",
					"summary": "ok",
					"success": true,
				}}, nil
			default:
				return Response{OK: false, Error: "unexpected " + req.Op}, nil
			}
		},
	}
	cfg := &config.Config{
		Browser: config.BrowserConfig{Enabled: &on, MaxSessions: 2, ServiceURL: "http://127.0.0.1:18766"},
	}
	cfg.ApplyDefaults()
	mgr, err := NewManager(cfg, mock)
	if err != nil {
		t.Fatal(err)
	}
	out, err := mgr.RunTaskWithOpts(context.Background(), "agt-w-browser", "do it", RunTaskOpts{
		MaxSteps: 10, Wait: true, WaitTimeoutSec: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK || out.Detail["status"] != "completed" || out.Detail["waited"] != true {
		t.Fatalf("out=%+v polls=%d", out, polls)
	}
	if polls < 2 {
		t.Fatalf("expected poll, polls=%d", polls)
	}
}

func TestManagerRunTaskRequiresTask(t *testing.T) {
	on := true
	cfg := &config.Config{
		Browser: config.BrowserConfig{
			Enabled:     &on,
			MaxSessions: 2,
			ServiceURL:  "http://127.0.0.1:18766",
		},
	}
	cfg.ApplyDefaults()
	mgr, err := NewManager(cfg, &MockDriver{})
	if err != nil {
		t.Fatal(err)
	}
	out, err := mgr.RunTask(context.Background(), "sess", "  ", 0)
	if err != nil {
		t.Fatal(err)
	}
	if out.OK || out.Error == "" {
		t.Fatalf("expected error, got %+v", out)
	}
}
