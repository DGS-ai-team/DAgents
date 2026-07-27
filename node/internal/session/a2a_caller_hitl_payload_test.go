package session

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

func TestHitlPayloadToSSE_toolApproval(t *testing.T) {
	payload := map[string]any{
		"hitl_kind":  "tool_approval",
		"event_type": "approval_required",
		"event_data": map[string]any{
			"approval_id": "appr-1",
			"approval_args": map[string]any{
				"tool_calls": []any{
					map[string]any{"id": "call-1", "name": "bash_run"},
				},
			},
		},
	}
	et, data := hitlPayloadToSSE(payload)
	if et != "hitl_required" {
		t.Fatalf("event type=%q", et)
	}
	if data["hitl_id"] != "appr-1" {
		t.Fatalf("data=%v", data)
	}
	items := hitlItemsFromAny(data["items"])
	if len(items) != 1 {
		t.Fatalf("items=%v", data["items"])
	}
}

func TestHitlPayloadToSSE_userInformation(t *testing.T) {
	payload := map[string]any{
		"hitl_kind": "user_information",
		"event_data": map[string]any{
			"content": "请确认环境",
			"user_information_args": map[string]any{
				"tool_call_id": "call-ask-1",
				"question":     "请确认环境",
			},
		},
	}
	et, data := hitlPayloadToSSE(payload)
	if et != "hitl_required" {
		t.Fatalf("event type=%q", et)
	}
	items := hitlItemsFromAny(data["items"])
	if len(items) != 1 {
		t.Fatalf("items=%v", data["items"])
	}
	item, _ := items[0].(map[string]any)
	args, _ := item["user_information_args"].(map[string]any)
	if args["tool_call_id"] != "call-ask-1" {
		t.Fatalf("args=%v", args)
	}
}

func TestHitlPayloadToSSE_unsupported(t *testing.T) {
	if et, data := hitlPayloadToSSE(nil); et != "" || data != nil {
		t.Fatalf("nil payload: et=%q data=%v", et, data)
	}
	if et, _ := hitlPayloadToSSE(map[string]any{"hitl_kind": "unknown"}); et != "" {
		t.Fatalf("unsupported: et=%q", et)
	}
}

func TestDeliverA2ACallerResume_wrongSession(t *testing.T) {
	bridge := NewA2ACallerHITLBridge("node-b", stream.NewHub(32, nil))
	if bridge.DeliverA2ACallerResume("no-waiter", map[string]any{"type": "approve"}) {
		t.Fatal("expected false for unknown session")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	payload := map[string]any{
		"hitl_kind": "tool_approval",
		"event_data": map[string]any{
			"approval_args": map[string]any{
				"tool_calls": []any{map[string]any{"id": "call-1", "name": "bash_run"}},
			},
		},
	}
	go func() {
		time.Sleep(30 * time.Millisecond)
		_ = bridge.DeliverA2ACallerResume("wrong-session", map[string]any{"type": "approve"})
		_ = bridge.DeliverA2ACallerResume("sess-caller", map[string]any{"type": "approve"})
	}()
	resume, err := bridge.WaitCallerHITL(ctx, "sess-caller", "task-1", payload)
	if err != nil {
		t.Fatal(err)
	}
	if resume["type"] != "approve" {
		t.Fatalf("resume=%v", resume)
	}
}

func TestWaitCallerHITL_userInformationRelay(t *testing.T) {
	hub := stream.NewHub(32, nil)
	bridge := NewA2ACallerHITLBridge("node-b", hub)
	sub := hub.Subscribe(0)
	defer hub.Unsubscribe(sub)

	payload := map[string]any{
		"hitl_kind":         "user_information",
		"callee_agent_name": "合规助手",
		"event_data": map[string]any{
			"content": "请确认部署环境",
			"user_information_args": map[string]any{
				"tool_call_id": "call-ask-1",
				"question":     "请确认部署环境",
			},
		},
	}

	var got []stream.Event
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		deadline := time.After(2 * time.Second)
		for len(got) < 2 {
			select {
			case ev := <-sub:
				got = append(got, ev)
			case <-deadline:
				return
			}
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = bridge.DeliverA2ACallerResume("sess-caller", map[string]any{
			"type":         "user_information",
			"tool_call_id": "call-ask-1",
			"answer":       "production",
		})
	}()

	resume, err := bridge.WaitCallerHITL(ctx, "sess-caller", "task-ui", payload)
	if err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if len(got) < 2 {
		t.Fatalf("events=%d", len(got))
	}
	if got[0].Type != "hitl_required" {
		t.Fatalf("first=%q", got[0].Type)
	}
	if got[0].Data["a2a_peer_agent_name"] != "合规助手" {
		t.Fatalf("peer=%v", got[0].Data["a2a_peer_agent_name"])
	}
	if got[1].Data["finish_reason"] != "awaiting_hitl" {
		t.Fatalf("done finish_reason=%v", got[1].Data["finish_reason"])
	}
	if resume["answer"] != "production" {
		t.Fatalf("resume=%v", resume)
	}
}
