package pending

import (
	"encoding/json"
	"time"

	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/nodeclient"
)

// EventHasAgent 判断 SSE 是否带有可关联的 Agent/session。
func EventHasAgent(ev nodeclient.StreamEvent) bool {
	return eventAgentID(ev) != ""
}

func eventAgentID(ev nodeclient.StreamEvent) string {
	return trim(ev.AgentID)
}

// ApplyNotificationChanged applies Node's complete notification projection.
// Unlike the old event classifier, this does not infer unread/HITL state from
// turn/tool event names and therefore cannot become stale when new event types
// are added. A full agent snapshot remains the reconnect/initial hydrate path.
func ApplyNotificationChanged(store *Store, ev nodeclient.StreamEvent) bool {
	if store == nil || ev.Type != "notification_changed" || !EventHasAgent(ev) {
		return false
	}
	var payload struct {
		HasUnread        bool `json:"has_unread"`
		HasPendingHITL   bool `json:"has_pending_hitl"`
		PendingHITLItems int  `json:"pending_hitl_items"`
	}
	raw, err := json.Marshal(ev.Data)
	if err != nil || json.Unmarshal(raw, &payload) != nil {
		return false
	}
	return store.ApplyNotification(eventAgentID(ev), payload.HasPendingHITL, payload.PendingHITLItems, payload.HasUnread)
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
			DisplayName: trim(ag.DisplayName),
			HITLItems:   items,
			HasUnread:   ag.HasUnread,
			EventType:   eventType,
			UpdatedAt:   now,
		}
	}
	return store.ReplaceFromNode(incoming)
}
