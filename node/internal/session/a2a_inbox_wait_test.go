package session

import (
	"context"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

// TestWaitInboxTurnWithSub_doneBeforeApproval 验证 done 先于 approval_required 时继续等待直至拿到 HITL 数据。
func TestWaitInboxTurnWithSub_doneBeforeApproval(t *testing.T) {
	hub := stream.NewHub(32, logx.Discard())
	mgr := &Manager{hub: hub}
	sessionID := "a2a-race-hitl"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	afterSeq := hub.CurrentSeq()
	sub := hub.Subscribe(afterSeq)
	defer hub.Unsubscribe(sub)

	go func() {
		time.Sleep(10 * time.Millisecond)
		hub.Publish(sessionID, "agent", "done", map[string]any{
			"turn_complete": false,
			"awaiting":      "tool_approval",
		})
		time.Sleep(10 * time.Millisecond)
		hub.Publish(sessionID, "agent", "approval_required", map[string]any{
			"approval_id": "appr-race",
			"approval_args": map[string]any{
				"tool_calls": []map[string]any{
					{"id": "call-race", "name": "bash_run"},
				},
			},
		})
		time.Sleep(10 * time.Millisecond)
		hub.Publish(sessionID, "agent", "done", map[string]any{
			"turn_complete": false,
			"awaiting":      "tool_approval",
		})
	}()

	step, err := mgr.waitInboxTurnWithSub(ctx, sessionID, sub)
	if err != nil {
		t.Fatal(err)
	}
	if step.HITL == nil || step.HITL.Awaiting != "tool_approval" {
		t.Fatalf("hitl=%+v", step.HITL)
	}
	if approvalIDFromHITL(step.HITL) != "appr-race" {
		t.Fatalf("approval_id=%q", approvalIDFromHITL(step.HITL))
	}
	ids := toolCallIDsFromHITL(step.HITL)
	if len(ids) != 1 || ids[0] != "call-race" {
		t.Fatalf("tool ids=%v", ids)
	}
}
