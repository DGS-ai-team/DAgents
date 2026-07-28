package session

import (
	"context"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

// TestWaitInboxTurnWithSub_doneBeforeHITL 验证 done 先于 hitl_required 时继续等待直至拿到 HITL 数据。
func TestWaitInboxTurnWithSub_doneBeforeHITL(t *testing.T) {
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
		hub.Publish(sessionID, "done", map[string]any{
			"turn_complete": false,
			"awaiting":      "hitl",
		})
		time.Sleep(10 * time.Millisecond)
		hub.Publish(sessionID, "hitl_required", map[string]any{
			"hitl_id": "appr-race",
			"message": "检测到工具调用，等待用户确认后继续执行。",
			"items": []any{
				map[string]any{"hitl_type": "execute_tool", "id": "call-race", "name": "bash_run"},
			},
		})
		time.Sleep(10 * time.Millisecond)
		hub.Publish(sessionID, "done", map[string]any{
			"turn_complete": false,
			"awaiting":      "hitl",
		})
	}()

	out, err := mgr.waitInboxTurnWithSub(ctx, sessionID, sub)
	if err != nil {
		t.Fatal(err)
	}
	if out.HITL == nil {
		t.Fatal("expected HITL pause")
	}
	if out.HITL.EventType != "hitl_required" {
		t.Fatalf("event_type=%q", out.HITL.EventType)
	}
}
