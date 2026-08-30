package childagent

import (
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

// RelayHub 将子 Agent turn 的 SSE 事件转发到父 Agent，并附加子任务元数据。
type RelayHub struct {
	Inner         *stream.Hub
	ParentAgentID string
	AgentID       string
	ChildAgentID  string
	ChildPurpose  string
	// Observe receives child runtime events before they are projected to the
	// parent stream, allowing the manager to maintain a refreshable snapshot.
	Observe func(eventType string, data map[string]any)
}

// Publish 实现 stream.Publisher；忽略 orchestrator 传入的 agentID，统一发往父 Agent。
func (h *RelayHub) Publish(_agentID, eventType string, data map[string]any) stream.Event {
	if eventType == "turn_finished" {
		// 子 turn 的终态不代表父 Agent 回合结束，避免 Client 误判。
		return stream.Event{}
	}
	if data == nil {
		data = map[string]any{}
	}
	data["child_agent_id"] = h.ChildAgentID
	if eventType == "hitl_required" {
		data["hitl_scope"] = HitlScopeTemporaryAgent
		data["child_purpose"] = h.ChildPurpose
	}
	if h.Observe != nil {
		h.Observe(eventType, data)
	}
	return h.Inner.Publish(h.ParentAgentID, eventType, data)
}
