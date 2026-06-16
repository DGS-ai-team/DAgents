package session

import (
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

// TestInboxSubscribeAfterSeqSkipsStaleApproval 验证 resume 前 Subscribe(afterSeq) 不回放历史 approval_required。
func TestInboxSubscribeAfterSeqSkipsStaleApproval(t *testing.T) {
	hub := stream.NewHub(32, nil)
	sessionID := "a2a-task-stale"

	// 模拟上一轮 turn 已发布的陈旧 approval_required。
	hub.Publish(sessionID, "agent", "approval_required", map[string]any{
		"approval_id": "stale-appr",
		"approval_args": map[string]any{
			"tool_calls": []any{
				map[string]any{"id": "call-stale", "name": "bash_run"},
			},
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
			if ev.Type == "approval_required" {
				t.Fatal("stale approval_required must not be replayed after Subscribe(afterSeq)")
			}
			if ev.Type == "done" {
				return
			}
		case <-deadline:
			t.Fatal("timeout waiting for done")
		}
	}
}
