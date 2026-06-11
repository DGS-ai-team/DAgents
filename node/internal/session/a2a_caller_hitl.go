package session

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

type a2aCallerWait struct {
	taskID string
	ch     chan map[string]any
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
	waiter := &a2aCallerWait{taskID: taskID, ch: make(chan map[string]any, 1)}
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
	b.hub.Publish(callerSessionID, b.agentID, eventType, data)

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

func hitlPayloadToSSE(payload map[string]any) (eventType string, data map[string]any) {
	if payload == nil {
		return "", nil
	}
	if raw, ok := payload["event_data"].(map[string]any); ok && len(raw) > 0 {
		if et, ok := payload["event_type"].(string); ok && strings.TrimSpace(et) != "" {
			return strings.TrimSpace(et), cloneEventData(raw)
		}
	}
	kind, _ := payload["hitl_kind"].(string)
	switch strings.TrimSpace(kind) {
	case "user_information":
		if raw, ok := payload["event_data"].(map[string]any); ok {
			return "user_information_required", cloneEventData(raw)
		}
	case "tool_approval":
		if raw, ok := payload["event_data"].(map[string]any); ok {
			return "approval_required", cloneEventData(raw)
		}
	}
	if et, ok := payload["event_type"].(string); ok {
		if raw, ok := payload["event_data"].(map[string]any); ok {
			return strings.TrimSpace(et), cloneEventData(raw)
		}
	}
	return "", nil
}
