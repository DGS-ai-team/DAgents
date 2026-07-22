package pending

import (
	"time"

	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/nodeclient"
)

// ShouldSyncOnEvent SSE 事件是否应触发从 Node 同步待办表（F-E13）。
func ShouldSyncOnEvent(ev nodeclient.StreamEvent) bool {
	if !EventHasAgent(ev) {
		return false
	}
	switch ev.Type {
	case "hitl_required", "approval_required", "user_information_required", "done":
		return true
	}
	return false
}

// EventHasAgent 判断 SSE 是否带有可关联的 Agent/session。
func EventHasAgent(ev nodeclient.StreamEvent) bool {
	return eventAgentID(ev) != ""
}

func eventAgentID(ev nodeclient.StreamEvent) string {
	if id := trim(ev.AgentID); id != "" {
		return id
	}
	return trim(ev.SessionID)
}

// SyncFromAgents 用 GET /v1/agents 的 Node 真相源重建待办表（F-E10/E13）。
func SyncFromAgents(store *Store, agents []nodeclient.AgentSummary) bool {
	if store == nil {
		return false
	}
	incoming := make(map[string]Entry)
	now := time.Now()
	for _, ag := range agents {
		id := trim(ag.AgentID)
		if id == "" {
			continue
		}
		if !ag.HasUnread && !ag.HasPendingHITL {
			continue
		}
		items := ag.PendingHITLItems
		if items <= 0 && ag.HasPendingHITL {
			items = 1
		}
		eventType := ""
		if ag.HasPendingHITL {
			eventType = "hitl_required"
		}
		incoming[id] = Entry{
			AgentID:     id,
			SessionID:   id,
			DisplayName: trim(ag.DisplayName),
			HITLItems:   items,
			HasUnread:   ag.HasUnread,
			EventType:   eventType,
			UpdatedAt:   now,
		}
	}
	return store.ReplaceFromNode(incoming)
}

// SyncFromSessions 为 SyncFromAgents 的历史别名。
func SyncFromSessions(store *Store, sessions []nodeclient.SessionSummary) bool {
	return SyncFromAgents(store, sessions)
}
