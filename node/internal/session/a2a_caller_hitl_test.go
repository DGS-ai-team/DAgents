package session

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

func TestWaitCallerHITLPublishesRelayPauseDone(t *testing.T) {
	hub := stream.NewHub(32, nil)
	bridge := NewA2ACallerHITLBridge("node-b", hub)
	sub := hub.Subscribe(0)
	defer hub.Unsubscribe(sub)

	payload := map[string]any{
		"hitl_kind":         "tool_approval",
		"event_type":        "approval_required",
		"callee_agent_id":   "compliance-a",
		"callee_agent_name": "合规助手",
		"event_data": map[string]any{
			"approval_args": map[string]any{
				"tool_calls": []any{
					map[string]any{"id": "call-1", "name": "bash_run"},
				},
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
		_ = bridge.DeliverA2ACallerResume("sess-caller", map[string]any{"type": "approve"})
	}()

	_, err := bridge.WaitCallerHITL(ctx, "sess-caller", "task-1", payload)
	if err != nil {
		t.Fatal(err)
	}
	wg.Wait()

	if len(got) < 2 {
		t.Fatalf("events=%d, want >= 2", len(got))
	}
	if got[0].Type != "approval_required" {
		t.Fatalf("first event type=%q", got[0].Type)
	}
	if got[0].Data["a2a_relay"] != true {
		t.Fatalf("approval a2a_relay=%v", got[0].Data["a2a_relay"])
	}
	if got[0].Data["a2a_peer_agent_name"] != "合规助手" {
		t.Fatalf("peer name=%v", got[0].Data["a2a_peer_agent_name"])
	}
	if got[1].Type != "done" {
		t.Fatalf("second event type=%q", got[1].Type)
	}
	if got[1].Data["turn_complete"] != false {
		t.Fatalf("done turn_complete=%v", got[1].Data["turn_complete"])
	}
	if got[1].Data["finish_reason"] != "awaiting_tool_approval" {
		t.Fatalf("finish_reason=%v", got[1].Data["finish_reason"])
	}
}

func TestAttachA2APeerMeta(t *testing.T) {
	data := map[string]any{"approval_id": "ap-1"}
	payload := map[string]any{
		"callee_agent_id":   "compliance-a",
		"callee_agent_name": "合规助手",
	}
	attachA2APeerMeta(data, payload)
	if data["a2a_peer_agent_id"] != "compliance-a" {
		t.Fatalf("id=%v", data["a2a_peer_agent_id"])
	}
	if data["a2a_peer_agent_name"] != "合规助手" {
		t.Fatalf("name=%v", data["a2a_peer_agent_name"])
	}
}

func TestAttachA2APeerMetaEmpty(t *testing.T) {
	data := map[string]any{}
	attachA2APeerMeta(data, map[string]any{})
	if _, ok := data["a2a_peer_agent_id"]; ok {
		t.Fatal("expected no peer id")
	}
}

func TestRelayHITLFinishReason(t *testing.T) {
	fr, aw := relayHITLFinishReason(map[string]any{"hitl_kind": "user_information"})
	if fr != "awaiting_user_information" || aw != "user_information" {
		t.Fatalf("user_information: fr=%q aw=%q", fr, aw)
	}
	fr, aw = relayHITLFinishReason(map[string]any{"hitl_kind": "tool_approval"})
	if fr != "awaiting_tool_approval" || aw != "tool_approval" {
		t.Fatalf("tool_approval: fr=%q aw=%q", fr, aw)
	}
}
