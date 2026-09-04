package session

import "github.com/DGS-ai-team/DAgents/node/internal/stream"

// ShouldBumpNotifySeq 判断 SSE 事件是否应推进 session 的 notify_seq（F-E13）。
func ShouldBumpNotifySeq(ev stream.Event) bool {
	if ev.AgentID == "" {
		return false
	}
	switch ev.Type {
	case "hitl_required":
		return true
	case "turn_finished":
		return shouldBumpNotifyOnTurnFinished(ev.Data)
	}
	return false
}

func shouldBumpNotifyOnTurnFinished(data map[string]any) bool {
	if data == nil {
		return true
	}
	finish, _ := data["finish_reason"].(string)
	switch finish {
	case "error", "cancelled":
		return false
	default:
		return true
	}
}
