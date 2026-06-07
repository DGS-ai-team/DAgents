package childagent

import (
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

// RelayHub 将子 Agent turn 的 SSE 事件转发到父 session，并附加子任务元数据。
type RelayHub struct {
	Inner           *stream.Hub
	ParentSessionID string
	AgentID         string
	ChildSessionID  string
	ChildPurpose string
}

// Publish 实现 stream.Publisher；忽略 orchestrator 传入的 sessionID，统一发往父 session。
func (h *RelayHub) Publish(_sessionID, agentID, eventType string, data map[string]any) stream.Event {
	if eventType == "done" {
		// 子 turn 的 done 不代表父 session 回合结束，避免 Client 误判。
		return stream.Event{}
	}
	if data == nil {
		data = map[string]any{}
	}
	data["child_session_id"] = h.ChildSessionID
	if eventType == "approval_required" {
		data["hitl_scope"] = HitlScopeTemporaryAgent
		data["child_purpose"] = h.ChildPurpose
	}
	aid := agentID
	if aid == "" {
		aid = h.AgentID
	}
	return h.Inner.Publish(h.ParentSessionID, aid, eventType, data)
}
