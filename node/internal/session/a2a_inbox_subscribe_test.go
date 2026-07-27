package session

import (
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

// TestInboxSubscribeAfterSeqSkipsStaleHITL 验证 resume 前 Subscribe(afterSeq) 不回放历史 hitl_required。
func TestInboxSubscribeAfterSeqSkipsStaleHITL(t *testing.T) {
	hub := stream.NewHub(32, nil)
	sessionID := "a2a-task-stale"

	// 模拟上一轮 turn 已发布的陈旧 hitl_required。
	hub.Publish(sessionID, "agent", "hitl_required", map[string]any{
		"hitl_id": "stale-appr",
		"items": []any{
			map[string]any{"hitl_type": "execute_tool", "id": "call-stale", "name": "bash_run"},
		},
	})
	afterSeq := hub.CurrentSeq()

	sub := hub.Subscribe(afterSeq)
	defer hub.Unsubscribe(sub)

	// resume 后仅应收到新事件。
	go func() {
		time.Sleep(10 * time.Millisecond)
		hub.Publish(sessionID, "agent", "done", map[string]any{
			"turn_complete": true,
		})
	}()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-sub:
			if ev.Type == "hitl_required" {
				t.Fatal("stale hitl_required must not be replayed after Subscribe(afterSeq)")
			}
			if ev.Type == "done" {
				return
			}
		case <-deadline:
			t.Fatal("timeout waiting for done")
		}
	}
}
