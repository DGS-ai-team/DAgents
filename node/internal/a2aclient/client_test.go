package a2aclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestClientCreateAndWait(t *testing.T) {
	var polls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/a2a/tasks":
			if got := r.Header.Get(agentIDHeader); got != "node-b" {
				t.Fatalf("agent header = %q", got)
			}
			_ = json.NewEncoder(w).Encode(CreateResponse{
				TaskID:    "task-1",
				Status:    "queued",
				ToAgentID: "node-a",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/a2a/tasks/task-1":
			n := polls.Add(1)
			status := "processing"
			result := ""
			if n >= 2 {
				status = "completed"
				result = "APPROVED with CHG-2026-0142"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"task": map[string]any{
					"task_id":     "task-1",
					"status":      status,
					"result_text": result,
				},
			})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	cfg := &config.Config{AgentID: "node-b", Manage: config.ManageConfig{URL: srv.URL}}
	client := New(cfg)
	ctx := context.Background()
	created, err := client.CreateInvokeTask(ctx, "node-a", "consult", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if created.TaskID != "task-1" {
		t.Fatalf("task_id=%q", created.TaskID)
	}
	rec, err := client.WaitForCompletion(ctx, created.TaskID, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if rec.ResultText == "" {
		t.Fatalf("empty result")
	}
}

func TestWaitForInvokeResultDoesNotRePromptAfterCallerResume(t *testing.T) {
	hitlPayload := `{"hitl_kind":"tool_approval","event_type":"approval_required","event_data":{"approval_id":"appr-1"}}`
	var polls atomic.Int32
	var hitlCalls atomic.Int32
	var notifyCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/a2a/tasks/task-hitl":
			n := polls.Add(1)
			status := "awaiting_caller"
			result := hitlPayload
			if n >= 5 {
				status = "completed"
				result = "done after hitl"
			} else if n >= 4 {
				status = "processing"
				result = hitlPayload
			} else if n >= 3 {
				status = "caller_responded"
				result = hitlPayload
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"task": map[string]any{
					"task_id":           "task-hitl",
					"status":              status,
					"result_text":         result,
					"caller_session_id":   "sess-caller",
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/a2a/tasks/task-hitl/caller_notify":
			notifyCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "caller_notified"})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/a2a/tasks/task-hitl/caller_resume":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "caller_responded"})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	cfg := &config.Config{AgentID: "node-b", Manage: config.ManageConfig{URL: srv.URL}}
	client := New(cfg)
	handler := &stubCallerHITL{t: t, calls: &hitlCalls, resume: map[string]any{"type": "selection", "approved": []string{"call-1"}}}
	rec, err := client.WaitForInvokeResult(context.Background(), "task-hitl", "sess-caller", 8*time.Second, handler)
	if err != nil {
		t.Fatal(err)
	}
	if hitlCalls.Load() != 1 {
		t.Fatalf("WaitCallerHITL calls=%d, want 1", hitlCalls.Load())
	}
	if notifyCalls.Load() != 1 {
		t.Fatalf("SubmitCallerNotify calls=%d, want 1", notifyCalls.Load())
	}
	if rec.ResultText != "done after hitl" {
		t.Fatalf("result_text=%q", rec.ResultText)
	}
}

type stubCallerHITL struct {
	t             *testing.T
	calls         *atomic.Int32
	resume        map[string]any
	assertPayload func(map[string]any)
}

func (s *stubCallerHITL) WaitCallerHITL(_ context.Context, callerSessionID, taskID string, payload map[string]any) (map[string]any, error) {
	s.calls.Add(1)
	if s.assertPayload != nil {
		s.assertPayload(payload)
	}
	if callerSessionID != "sess-caller" {
		s.t.Fatalf("callerSessionID=%q taskID=%q", callerSessionID, taskID)
	}
	return s.resume, nil
}

func TestClientDiscover(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("discovery_group") != "lab" {
			t.Fatalf("group=%q", r.URL.Query().Get("discovery_group"))
		}
		_ = json.NewEncoder(w).Encode(DiscoverResponse{
			Agents: []DiscoverAgent{{AgentID: "node-a", Name: "合规助手"}},
		})
	}))
	defer srv.Close()

	cfg := &config.Config{AgentID: "node-b", Manage: config.ManageConfig{URL: srv.URL}}
	resp, err := New(cfg).DiscoverAgents(context.Background(), "lab")
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Agents) != 1 || resp.Agents[0].AgentID != "node-a" {
		t.Fatalf("resp=%+v", resp)
	}
}
