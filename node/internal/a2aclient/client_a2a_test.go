package a2aclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestParseRequiresInputPayload(t *testing.T) {
	raw := `{"hitl_kind":"tool_approval","event_type":"approval_required","event_data":{"approval_id":"ap-1"}}`
	out, err := ParseRequiresInputPayload(raw)
	if err != nil {
		t.Fatal(err)
	}
	if out["hitl_kind"] != "tool_approval" {
		t.Fatalf("payload=%v", out)
	}
	if _, err := ParseRequiresInputPayload(""); err == nil {
		t.Fatal("expected error for empty payload")
	}
	if _, err := ParseRequiresInputPayload("not-json"); err == nil {
		t.Fatal("expected error for invalid json")
	}
}

func TestWaitForInvokeResult_taskFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task": map[string]any{
				"task_id":      "task-fail",
				"status":       "failed",
				"error_detail": "callee execution error",
			},
		})
	}))
	defer srv.Close()

	cfg := &config.Config{AgentID: "node-b", Manage: config.ManageConfig{URL: srv.URL}}
	_, err := New(cfg).WaitForCompletion(context.Background(), "task-fail", 3*time.Second)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); !strings.Contains(got, "task-fail") || !strings.Contains(got, "callee execution error") {
		t.Fatalf("err=%q", got)
	}
}

func TestWaitForInvokeResult_taskExpired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task": map[string]any{
				"task_id": "task-exp",
				"status":  "expired",
			},
		})
	}))
	defer srv.Close()

	cfg := &config.Config{AgentID: "node-b", Manage: config.ManageConfig{URL: srv.URL}}
	_, err := New(cfg).WaitForCompletion(context.Background(), "task-exp", 3*time.Second)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); !strings.Contains(got, "task-exp") || !strings.Contains(got, "expired") {
		t.Fatalf("err=%q", got)
	}
}

func TestWaitForInvokeResult_awaitingCallerNoHandler(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task": map[string]any{
				"task_id":     "task-await",
				"status":      "awaiting_caller",
				"result_text": `{"hitl_kind":"tool_approval"}`,
			},
		})
	}))
	defer srv.Close()

	cfg := &config.Config{AgentID: "node-b", Manage: config.ManageConfig{URL: srv.URL}}
	_, err := New(cfg).WaitForCompletion(context.Background(), "task-await", 3*time.Second)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); !strings.Contains(got, "awaiting caller input") {
		t.Fatalf("err=%q", got)
	}
}

func TestWaitForInvokeResult_userInformationRelay(t *testing.T) {
	hitlPayload := `{"hitl_kind":"user_information","event_data":{"content":"请确认环境","user_information_args":{"tool_call_id":"call-ask-1"}}}`
	var polls atomic.Int32
	var hitlCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/a2a/tasks/task-ui":
			n := polls.Add(1)
			status := "awaiting_caller"
			result := hitlPayload
			if n >= 4 {
				status = "completed"
				result = "APPROVED | rule=R-ENV-01"
			} else if n >= 3 {
				status = "processing"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"task": map[string]any{
					"task_id":           "task-ui",
					"status":              status,
					"result_text":         result,
					"caller_session_id":   "sess-caller",
				},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/caller_notify"):
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "caller_notified"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/caller_resume"):
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "caller_responded"})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	cfg := &config.Config{AgentID: "node-b", Manage: config.ManageConfig{URL: srv.URL}}
	handler := &stubCallerHITL{
		t:     t,
		calls: &hitlCalls,
		assertPayload: func(payload map[string]any) {
			if payload["hitl_kind"] != "user_information" {
				t.Fatalf("hitl_kind=%v", payload["hitl_kind"])
			}
		},
		resume: map[string]any{
			"type":         "user_information",
			"tool_call_id": "call-ask-1",
			"answer":       "production",
		},
	}
	rec, err := New(cfg).WaitForInvokeResult(context.Background(), "task-ui", "sess-caller", 8*time.Second, handler)
	if err != nil {
		t.Fatal(err)
	}
	if hitlCalls.Load() != 1 {
		t.Fatalf("WaitCallerHITL calls=%d", hitlCalls.Load())
	}
	if !strings.Contains(rec.ResultText, "APPROVED") || !strings.Contains(rec.ResultText, "R-ENV-01") {
		t.Fatalf("result=%q", rec.ResultText)
	}
}
