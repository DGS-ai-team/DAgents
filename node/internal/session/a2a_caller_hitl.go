package session

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

type a2aCallerWait struct {
	taskID          string
	ch              chan map[string]any
	eventType       string
	eventData       map[string]any
	callerSessionID string
}

// A2ACallerHITLBridge 供 agent_invoke 在 Task awaiting_caller 时将 HITL 中继到 caller session TUI。
type A2ACallerHITLBridge struct {
	mu      sync.Mutex
	agentID string
	hub     *stream.Hub
	byTask  map[string]*a2aCallerWait
	bySess  map[string]string // caller session_id -> task_id
}

// NewA2ACallerHITLBridge 创建 caller 侧 A2A HITL 中继。
func NewA2ACallerHITLBridge(agentID string, hub *stream.Hub) *A2ACallerHITLBridge {
	return &A2ACallerHITLBridge{
		agentID: strings.TrimSpace(agentID),
		hub:     hub,
		byTask:  make(map[string]*a2aCallerWait),
		bySess:  make(map[string]string),
	}
}

// WaitCallerHITL 向 caller session 推送 HITL SSE 并阻塞至用户 resume。
func (b *A2ACallerHITLBridge) WaitCallerHITL(
	ctx context.Context,
	callerSessionID, taskID string,
	hitlPayload map[string]any,
) (map[string]any, error) {
	if b == nil || b.hub == nil {
		return nil, fmt.Errorf("A2A caller HITL bridge is not configured")
	}
	callerSessionID = strings.TrimSpace(callerSessionID)
	taskID = strings.TrimSpace(taskID)
	if callerSessionID == "" || taskID == "" {
		return nil, fmt.Errorf("caller_session_id and task_id are required")
	}
	waiter := &a2aCallerWait{
		taskID:          taskID,
		ch:              make(chan map[string]any, 1),
		callerSessionID: callerSessionID,
	}
	b.mu.Lock()
	b.byTask[taskID] = waiter
	b.bySess[callerSessionID] = taskID
	b.mu.Unlock()
	defer b.clearWait(taskID, callerSessionID)

	eventType, data := hitlPayloadToSSE(hitlPayload)
	if eventType == "" {
		return nil, fmt.Errorf("unsupported A2A HITL payload")
	}
	data["a2a_task_id"] = taskID
	data["a2a_relay"] = true
	attachA2APeerMeta(data, hitlPayload)
	waiter.eventType = eventType
	waiter.eventData = cloneEventData(data)
	b.hub.Publish(callerSessionID, b.agentID, eventType, data)
	b.publishRelayTurnPause(callerSessionID, hitlPayload)

	select {
	case resume := <-waiter.ch:
		return resume, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// DeliverA2ACallerResume 拦截 caller session 的 resume，供 agent_invoke 转发至 Manage。
func (b *A2ACallerHITLBridge) DeliverA2ACallerResume(callerSessionID string, resume map[string]any) bool {
	if b == nil {
		return false
	}
	callerSessionID = strings.TrimSpace(callerSessionID)
	b.mu.Lock()
	taskID := b.bySess[callerSessionID]
	waiter := b.byTask[taskID]
	b.mu.Unlock()
	if waiter == nil || taskID == "" {
		return false
	}
	select {
	case waiter.ch <- cloneEventData(resume):
		return true
	default:
		return false
	}
}

func (b *A2ACallerHITLBridge) clearWait(taskID, callerSessionID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.byTask, taskID)
	delete(b.bySess, callerSessionID)
}

// PendingRelaySnapshot 返回 caller session 上仍在等待的 A2A relay HITL（F-H4 hydrate）。
func (b *A2ACallerHITLBridge) PendingRelaySnapshot(callerSessionID string) map[string]any {
	if b == nil {
		return nil
	}
	callerSessionID = strings.TrimSpace(callerSessionID)
	b.mu.Lock()
	taskID := b.bySess[callerSessionID]
	waiter := b.byTask[taskID]
	b.mu.Unlock()
	if waiter == nil || strings.TrimSpace(waiter.eventType) == "" || len(waiter.eventData) == 0 {
		return nil
	}
	return map[string]any{
		"event_type":   waiter.eventType,
		"data":         cloneEventData(waiter.eventData),
		"a2a_task_id":  strings.TrimSpace(waiter.taskID),
		"a2a_relay":    true,
	}
}

// publishRelayTurnPause 推送 synthetic done，与本地 HITL 暂停对齐，便于 Client 释放 turn 等待。
func (b *A2ACallerHITLBridge) publishRelayTurnPause(callerSessionID string, hitlPayload map[string]any) {
	if b == nil || b.hub == nil {
		return
	}
	finishReason, awaiting := relayHITLFinishReason(hitlPayload)
	payload := map[string]any{
		"finish_reason": finishReason,
		"turn_complete": false,
		"awaiting":      awaiting,
		"a2a_relay":     true,
	}
	b.hub.Publish(callerSessionID, b.agentID, "done", payload)
}

func relayHITLFinishReason(hitlPayload map[string]any) (finishReason, awaiting string) {
	return "awaiting_hitl", "hitl"
}

// hitlPayloadToSSE 将 Manage requires_input 载荷统一为 hitl_required（与本地 turn SSE 同构）。
// 仅接受已含 items[] 的现代载荷；旧 approval_args / user_information_args 形状不再转换。
func hitlPayloadToSSE(payload map[string]any) (eventType string, data map[string]any) {
	if payload == nil {
		return "", nil
	}
	raw, _ := payload["event_data"].(map[string]any)
	if len(raw) == 0 {
		return "", nil
	}
	if items := hitlItemsFromAny(raw["items"]); len(items) == 0 {
		return "", nil
	}
	out := cloneEventData(raw)
	if strings.TrimSpace(fmt.Sprint(out["hitl_id"])) == "" || fmt.Sprint(out["hitl_id"]) == "<nil>" {
		out["hitl_id"] = "a2a-hitl"
	}
	if _, ok := out["display_type"]; !ok {
		out["display_type"] = "normal_text"
	}
	return "hitl_required", out
}

func hitlItemsFromAny(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case []map[string]any:
		out := make([]any, 0, len(t))
		for _, m := range t {
			out = append(out, m)
		}
		return out
	default:
		return nil
	}
}

// attachA2APeerMeta 将 requires_input 中的对端 Agent 标识写入 caller SSE（供 Client 展示 from 标签）。
func attachA2APeerMeta(data, hitlPayload map[string]any) {
	if data == nil || hitlPayload == nil {
		return
	}
	if id, _ := hitlPayload["callee_agent_id"].(string); strings.TrimSpace(id) != "" {
		data["a2a_peer_agent_id"] = strings.TrimSpace(id)
	}
	if name, _ := hitlPayload["callee_agent_name"].(string); strings.TrimSpace(name) != "" {
		data["a2a_peer_agent_name"] = strings.TrimSpace(name)
	} else if peerID := strings.TrimSpace(fmt.Sprint(data["a2a_peer_agent_id"])); peerID != "" && peerID != "<nil>" {
		data["a2a_peer_agent_name"] = peerID
	}
}
