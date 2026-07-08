package pending

import (
	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/nodeclient"
)

// ApplyEvent 根据 SSE 事件更新待办表（F-E2/E11/E13）。
func ApplyEvent(store *Store, ev nodeclient.StreamEvent) bool {
	if store == nil {
		return false
	}
	sessionID := trim(ev.SessionID)
	if sessionID == "" {
		return false
	}
	switch ev.Type {
	case "hitl_required":
		store.MarkHITL(sessionID, countItems(ev.Data["items"]), ev.Type)
		return true
	case "approval_required", "user_information_required":
		store.MarkHITL(sessionID, 1, ev.Type)
		return true
	case "done":
		changed := false
		if shouldClearHITLOnDone(ev.Data) && store.ClearHITL(sessionID) {
			changed = true
		}
		if shouldMarkUnreadOnDone(ev.Data) {
			store.MarkUnread(sessionID)
			changed = true
		}
		if shouldClearSessionOnDone(ev.Data) && store.ClearSession(sessionID) {
			changed = true
		}
		return changed
	}
	return false
}

// SyncActiveAwaiting 用 GET /v1/sessions 清除已非 awaiting_hitl 的 HITL 待办（F-E10）。
func SyncActiveAwaiting(store *Store, sessions []nodeclient.SessionSummary) {
	if store == nil {
		return
	}
	clearHITL := make(map[string]struct{})
	for _, sess := range sessions {
		if !sess.Active {
			continue
		}
		phase := trim(sess.RunTurnPhase)
		if phase == "" {
			continue
		}
		if phase != "awaiting_hitl" {
			clearHITL[sess.SessionID] = struct{}{}
		}
	}
	store.ClearHITLForSessions(clearHITL)
}

func shouldClearHITLOnDone(data map[string]any) bool {
	if data == nil {
		return true
	}
	if awaiting, _ := data["awaiting"].(string); awaiting == "hitl" {
		return false
	}
	finish, _ := data["finish_reason"].(string)
	switch finish {
	case "awaiting_hitl", "awaiting_user_information", "awaiting_tool_approval":
		return false
	}
	return true
}

func shouldMarkUnreadOnDone(data map[string]any) bool {
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

func shouldClearSessionOnDone(data map[string]any) bool {
	if data == nil {
		return false
	}
	finish, _ := data["finish_reason"].(string)
	switch finish {
	case "error", "cancelled":
		return true
	}
	return false
}

func countItems(raw any) int {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return 1
	}
	return len(items)
}
