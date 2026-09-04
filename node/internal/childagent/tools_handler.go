package childagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// HandleCancelTool 实现 cancel_temporary_agent 工具。
func (m *Manager) HandleCancelTool(parentSessionID, argsJSON string) (string, error) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return "ERROR: invalid json", nil
	}
	childID := strings.TrimSpace(fmt.Sprint(raw["child_agent_id"]))
	reason := strings.TrimSpace(fmt.Sprint(raw["reason"]))
	if childID == "" || childID == "<nil>" {
		return "ERROR: child_agent_id is required", nil
	}
	prev, err := m.cancelInternal(parentSessionID, childID, reason)
	if err != nil {
		return "ERROR: " + err.Error(), nil
	}
	body, _ := json.Marshal(map[string]any{
		"child_agent_id":  childID,
		"status":          StatusCancelled,
		"previous_status": prev,
	})
	return string(body), nil
}

func (m *Manager) cancelInternal(parentSessionID, childID, reason string) (previous string, err error) {
	m.mu.Lock()
	agent, ok := m.activeByID[childID]
	if !ok {
		m.mu.Unlock()
		return "", fmt.Errorf("child_agent_id not found or not owned by parent")
	}
	if agent.ParentAgentID != parentSessionID {
		m.mu.Unlock()
		return "", fmt.Errorf("child_agent_id not found or not owned by parent")
	}
	if agent.isTerminal() {
		prev := string(agent.Snapshot().Status)
		m.mu.Unlock()
		return prev, nil
	}
	prev := string(agent.Snapshot().Status)
	m.mu.Unlock()
	_, err = m.Cancel(parentSessionID, childID, reason)
	return prev, err
}

// HandleParentTool 分发父 Agent 临时 Agent 管理工具（不经过外部 Workgroup）。
func (m *Manager) HandleParentTool(ctx context.Context, parentSessionID, toolName, argsJSON string, toolCallIDs ...string) (string, error) {
	switch toolName {
	case ToolCreateTemporaryAgent:
		toolCallID := ""
		if len(toolCallIDs) > 0 {
			toolCallID = toolCallIDs[0]
		}
		return m.handleCreate(ctx, parentSessionID, argsJSON, toolCallID)
	case ToolCancelTemporaryAgent:
		return m.HandleCancelTool(parentSessionID, argsJSON)
	default:
		return "", fmt.Errorf("unknown temporary agent tool %q", toolName)
	}
}
