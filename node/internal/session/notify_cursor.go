package session

import "github.com/DGS-ai-team/DAgents/node/internal/stream"

// ShouldBumpNotifySeq 判断 SSE 事件是否应推进 session 的 notify_seq（F-E13）。
func ShouldBumpNotifySeq(ev stream.Event) bool {
	if ev.SessionID == "" {
		return false
	}
	switch ev.Type {
	case "hitl_required":
		return true
	case "done":
		return shouldBumpNotifyOnDone(ev.Data)
	}
	return false
}

func shouldBumpNotifyOnDone(data map[string]any) bool {
	if data == nil {
		return false
	}
	if awaiting, _ := data["awaiting"].(string); awaiting == "hitl" {
		return false
	}
	finish, _ := data["finish_reason"].(string)
	switch finish {
	case "awaiting_hitl", "awaiting_user_information", "awaiting_tool_approval", "error", "cancelled":
		return false
	}
	turnComplete, ok := data["turn_complete"].(bool)
	if ok {
		return turnComplete
	}
	return finish == "stop" || finish == ""
}
