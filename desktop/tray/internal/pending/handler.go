package pending

import (
	"time"

	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/nodeclient"
)

// ShouldSyncOnEvent SSE 事件是否应触发从 Node 同步待办表（F-E13）。
func ShouldSyncOnEvent(ev nodeclient.StreamEvent) bool {
	sessionID := trim(ev.SessionID)
	if sessionID == "" {
		return false
	}
	switch ev.Type {
	case "hitl_required", "approval_required", "user_information_required", "done":
		return true
	}
	return false
}

// SyncFromSessions 用 GET /v1/sessions 的 Node 真相源重建待办表（F-E10/E13）。
func SyncFromSessions(store *Store, sessions []nodeclient.SessionSummary) bool {
	if store == nil {
		return false
	}
	incoming := make(map[string]Entry)
	now := time.Now()
	for _, sess := range sessions {
		if !sess.HasUnread && !sess.HasPendingHITL {
			continue
		}
		items := sess.PendingHITLItems
		if items <= 0 && sess.HasPendingHITL {
			items = 1
		}
		eventType := ""
		if sess.HasPendingHITL {
			eventType = "hitl_required"
		}
		incoming[sess.SessionID] = Entry{
			SessionID: sess.SessionID,
			HITLItems: items,
			HasUnread: sess.HasUnread,
			EventType: eventType,
			UpdatedAt: now,
		}
	}
	return store.ReplaceFromNode(incoming)
}
