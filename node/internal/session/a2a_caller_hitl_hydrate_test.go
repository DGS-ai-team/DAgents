package session

import (
	"context"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

func TestPendingRelaySnapshotWhileWaiting(t *testing.T) {
	hub := stream.NewHub(32, nil)
	bridge := NewA2ACallerHITLBridge("node-b", hub)
	payload := map[string]any{
		"event_type":        "hitl_required",
		"callee_agent_name": "合规助手",
		"event_data": map[string]any{
			"hitl_id": "appr-1",
			"items": []any{
				map[string]any{"id": "call-1", "name": "bash_run", "hitl_type": "execute_tool"},
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = bridge.WaitCallerHITL(ctx, "sess-caller", "task-1", payload)
	}()
	time.Sleep(20 * time.Millisecond)

	snap := bridge.PendingRelaySnapshot("sess-caller")
	if snap == nil {
		t.Fatal("expected relay snapshot")
	}
	if snap["event_type"] != "hitl_required" {
		t.Fatalf("event_type=%v", snap["event_type"])
	}
	if snap["a2a_task_id"] != "task-1" {
		t.Fatalf("task_id=%v", snap["a2a_task_id"])
	}
	data, _ := snap["data"].(map[string]any)
	if data["a2a_relay"] != true {
		t.Fatalf("a2a_relay=%v", data["a2a_relay"])
	}
	if data["a2a_peer_agent_name"] != "合规助手" {
		t.Fatalf("peer=%v", data["a2a_peer_agent_name"])
	}

	cancel()
	<-done
	if bridge.PendingRelaySnapshot("sess-caller") != nil {
		t.Fatal("expected snapshot cleared after wait ends")
	}
}

func TestPendingRelaySnapshotEmpty(t *testing.T) {
	bridge := NewA2ACallerHITLBridge("node-b", nil)
	if bridge.PendingRelaySnapshot("missing") != nil {
		t.Fatal("expected nil for unknown session")
	}
}
