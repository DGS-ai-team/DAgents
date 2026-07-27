package manage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

type stubInboxRunner struct {
	steps []session.InboxTurnResult
	err   error
	calls int
}

func (s *stubInboxRunner) RunInboxTurn(_ context.Context, _, _ string, _ map[string]any) (session.InboxTurnResult, error) {
	defer func() { s.calls++ }()
	if s.err != nil {
		return session.InboxTurnResult{}, s.err
	}
	if s.calls >= len(s.steps) {
		return session.InboxTurnResult{Complete: true, Text: "fallback"}, nil
	}
	return s.steps[s.calls], nil
}

func TestEncodeRequiresInputPayload(t *testing.T) {
	cfg := &config.Config{
		NodeID: "compliance-a",
		Agent: config.AgentConfig{
			Name: "合规助手",
			Role: "compliance",
		},
	}
	payload, err := encodeRequiresInputPayload(cfg, InboxTask{
		TaskID:          "task-enc",
		FromAgentID:     "node-b",
		CallerSessionID: "sess-caller",
	}, "a2a-task-enc", &session.InboxHITLPause{
		Awaiting:  "hitl",
		EventType: "hitl_required",
		Data: map[string]any{
			"hitl_id": "appr-enc",
			"items": []any{
				map[string]any{"hitl_type": "execute_tool", "id": "call-1", "name": "bash_run"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["hitl_kind"] != "hitl" {
		t.Fatalf("hitl_kind=%v", parsed["hitl_kind"])
	}
	if parsed["callee_agent_id"] != "compliance-a" {
		t.Fatalf("callee_agent_id=%v", parsed["callee_agent_id"])
	}
	if parsed["callee_agent_name"] != "合规助手" {
		t.Fatalf("callee_agent_name=%v", parsed["callee_agent_name"])
	}
	if parsed["caller_session_id"] != "sess-caller" {
		t.Fatalf("caller_session_id=%v", parsed["caller_session_id"])
	}
	if parsed["event_type"] != "hitl_required" {
		t.Fatalf("event_type=%v", parsed["event_type"])
	}
	eventData, _ := parsed["event_data"].(map[string]any)
	if eventData["hitl_id"] != "appr-enc" {
		t.Fatalf("event_data=%v", eventData)
	}
}

func TestEncodeRequiresInputPayload_userInformation(t *testing.T) {
	cfg := &config.Config{NodeID: "compliance-a"}
	payload, err := encodeRequiresInputPayload(cfg, InboxTask{TaskID: "task-ui"}, "a2a-task-ui", &session.InboxHITLPause{
		Awaiting:  "hitl",
		EventType: "hitl_required",
		Data: map[string]any{
			"hitl_id": "a2a-user-info",
			"items": []any{
				map[string]any{
					"hitl_type": "user_information",
					"id":        "call-ask-1",
					"user_information_args": map[string]any{
						"tool_call_id": "call-ask-1",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["hitl_kind"] != "hitl" {
		t.Fatalf("hitl_kind=%v", parsed["hitl_kind"])
	}
	if parsed["event_type"] != "hitl_required" {
		t.Fatalf("event_type=%v", parsed["event_type"])
	}
}

func TestComplianceExecutor_turnErrorRepliesFailed(t *testing.T) {
	cfg := &config.Config{
		NodeID: "compliance-a",
		Manage:  config.ManageConfig{Enabled: true},
	}
	var replyBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/reply") {
			_ = json.NewDecoder(r.Body).Decode(&replyBody)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"task_id": "t-err", "status": replyBody["status"]})
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()
	cfg.Manage.URL = srv.URL

	runner := &stubInboxRunner{err: errors.New("inbox turn exploded")}
	ex := NewComplianceExecutor(cfg, runner, nil)
	err := ex.HandleTask(context.Background(), InboxTask{TaskID: "t-err", FromAgentID: "node-b", Content: "consult"})
	if err != nil {
		t.Fatal(err)
	}
	if replyBody["status"] != "failed" {
		t.Fatalf("status=%q body=%v", replyBody["status"], replyBody)
	}
	if !strings.Contains(replyBody["error_detail"], "inbox turn exploded") {
		t.Fatalf("error_detail=%q", replyBody["error_detail"])
	}
}

func TestComplianceExecutor_nilSessionsRepliesFailed(t *testing.T) {
	cfg := &config.Config{
		NodeID: "compliance-a",
		Manage:  config.ManageConfig{Enabled: true},
	}
	var replyBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/reply") {
			_ = json.NewDecoder(r.Body).Decode(&replyBody)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"task_id": "t-nil", "status": replyBody["status"]})
		}
	}))
	defer srv.Close()
	cfg.Manage.URL = srv.URL

	ex := NewComplianceExecutor(cfg, nil, nil)
	if err := ex.HandleTask(context.Background(), InboxTask{TaskID: "t-nil", Content: "x"}); err != nil {
		t.Fatal(err)
	}
	if replyBody["status"] != "failed" {
		t.Fatalf("status=%q", replyBody["status"])
	}
}

func TestComplianceExecutor_requiresInputThenFailedOnSecondTurn(t *testing.T) {
	cfg := &config.Config{
		NodeID: "compliance-a",
		Manage:  config.ManageConfig{Enabled: true},
	}
	var bodies []map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/reply"):
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			bodies = append(bodies, body)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"task_id": "t-2step", "status": body["status"]})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/caller_input"):
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ready": true,
				"resume_value": map[string]any{
					"type":     "reject",
					"approved": []any{},
					"rejected": []any{"call-1"},
				},
			})
		default:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer srv.Close()
	cfg.Manage.URL = srv.URL

	runner := &stubInboxRunner{
		steps: []session.InboxTurnResult{
			{
				HITL: &session.InboxHITLPause{
					Awaiting:  "hitl",
					EventType: "hitl_required",
					Data: map[string]any{
						"hitl_id": "ap-1",
						"items": []any{
							map[string]any{"hitl_type": "execute_tool", "id": "call-1", "name": "bash_run"},
						},
					},
				},
			},
			{Complete: true, Text: "DENIED | rule=R-TIME-01 | user rejected"},
		},
	}
	ex := NewComplianceExecutor(cfg, runner, nil)
	if err := ex.HandleTask(context.Background(), InboxTask{TaskID: "t-2step", FromAgentID: "node-b", Content: "consult"}); err != nil {
		t.Fatal(err)
	}
	if len(bodies) < 2 {
		t.Fatalf("bodies=%v", bodies)
	}
	if bodies[0]["status"] != "requires_input" {
		t.Fatalf("first status=%q", bodies[0]["status"])
	}
	if bodies[len(bodies)-1]["status"] != "completed" {
		t.Fatalf("final status=%q", bodies[len(bodies)-1]["status"])
	}
	if !strings.Contains(bodies[len(bodies)-1]["result_text"], "DENIED") {
		t.Fatalf("result=%q", bodies[len(bodies)-1]["result_text"])
	}
}
