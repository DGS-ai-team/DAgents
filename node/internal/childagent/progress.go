package childagent

import (
	"fmt"
	"strings"
	"time"
)

const progressPreviewLimit = 240

// ObserveChildEvent updates the manager-owned progress snapshot for one child.
// The snapshot is intentionally derived from the existing child SSE stream;
// no second transcript or event queue is introduced.
func (m *Manager) ObserveChildEvent(childSessionID, eventType string, data map[string]any) {
	if m == nil {
		return
	}
	m.mu.Lock()
	agent, ok := m.activeByID[childSessionID]
	if !ok || agent == nil || agent.isTerminal() {
		m.mu.Unlock()
		return
	}
	agent.mu.Lock()
	previous := agent.Progress
	next := previous
	next.Status = agent.Status
	next.MaxTurns = agent.MaxTurns
	applyChildProgressEvent(&next, eventType, data)
	if progressEqual(previous, next) {
		agent.mu.Unlock()
		m.mu.Unlock()
		return
	}
	next.Revision = previous.Revision + 1
	next.UpdatedAt = time.Now().UTC()
	agent.Progress = next
	parentID := agent.ParentAgentID
	agent.mu.Unlock()
	m.mu.Unlock()

	m.publishProgress(parentID, agent, next)
}

func applyChildProgressEvent(progress *Progress, eventType string, data map[string]any) {
	if progress == nil {
		return
	}
	switch eventType {
	case "turn_state":
		if phase := progressString(data, "phase"); phase != "" {
			progress.Phase = phase
		}
		if step := progressInt(data, "step_index"); step > progress.TurnCount {
			progress.TurnCount = step
		}
		if exec := firstRunningExecution(data); exec != nil {
			progress.CurrentTool = progressString(exec, "tool_name")
			progress.CurrentToolCallID = progressString(exec, "tool_call_id")
			progress.CurrentToolStatus = progressString(exec, "status")
		}
		phase := strings.ToLower(strings.TrimSpace(progress.Phase))
		if phase == "tool_waiting" || phase == "waiting_user" ||
			strings.EqualFold(progressString(data, "interaction_kind"), "approval") {
			progress.PendingApproval = true
			progress.Phase = "waiting_approval"
		} else if phase == "tool_executing" || phase == "model_generating" {
			progress.PendingApproval = false
		}
	case "tool_call":
		if call := firstToolCall(data); call != nil {
			progress.CurrentTool = toolCallName(call)
			progress.CurrentToolCallID = toolCallID(call)
		}
		if id := progressString(data, "tool_call_id"); id != "" {
			progress.CurrentToolCallID = id
		}
		progress.CurrentToolStatus = "running"
		progress.Phase = "tool_executing"
		progress.PendingApproval = false
	case "tool_result":
		if name := progressString(data, "tool_name"); name != "" {
			progress.CurrentTool = name
		}
		if id := progressString(data, "tool_call_id"); id != "" {
			progress.CurrentToolCallID = id
		}
		status := strings.ToLower(strings.TrimSpace(progressString(data, "status")))
		if status == "" {
			status = "completed"
		}
		progress.CurrentToolStatus = status
		progress.LastOutputPreview = truncateProgress(firstProgressLine(data["content"]))
		progress.Phase = "tool_completed"
		progress.PendingApproval = false
	case "assistant", "reasoning":
		progress.Phase = "model_generating"
		progress.PendingApproval = false
	case "hitl_required":
		progress.Phase = "waiting_approval"
		progress.PendingApproval = true
	}
}

func (m *Manager) publishProgress(parentID string, agent *ActiveAgent, progress Progress) {
	if m == nil || m.hub == nil || agent == nil {
		return
	}
	snapshot := agent.Snapshot()
	payload := map[string]any{
		"child_agent_id":       snapshot.ChildAgentID,
		"parent_agent_id":      parentID,
		"tool_call_id":         snapshot.ToolCallID,
		"purpose":              snapshot.Purpose,
		"status":               progress.Status,
		"phase":                progress.Phase,
		"turn_count":           progress.TurnCount,
		"max_turns":            progress.MaxTurns,
		"current_tool":         progress.CurrentTool,
		"current_tool_call_id": progress.CurrentToolCallID,
		"current_tool_status":  progress.CurrentToolStatus,
		"last_output_preview":  progress.LastOutputPreview,
		"pending_approval":     progress.PendingApproval,
		"summary":              progress.Summary,
		"error":                progress.Error,
		"updated_at":           progress.UpdatedAt,
		"revision":             progress.Revision,
	}
	m.hub.Publish(parentID, EventTemporaryAgentProgress, payload)
}

func progressEqual(a, b Progress) bool {
	return a.Status == b.Status &&
		a.Phase == b.Phase &&
		a.TurnCount == b.TurnCount &&
		a.MaxTurns == b.MaxTurns &&
		a.CurrentTool == b.CurrentTool &&
		a.CurrentToolCallID == b.CurrentToolCallID &&
		a.CurrentToolStatus == b.CurrentToolStatus &&
		a.LastOutputPreview == b.LastOutputPreview &&
		a.PendingApproval == b.PendingApproval &&
		a.Summary == b.Summary &&
		a.Error == b.Error
}

func firstRunningExecution(data map[string]any) map[string]any {
	raw, ok := data["tool_executions"]
	if !ok {
		return nil
	}
	for _, item := range anySlice(raw) {
		if execution, ok := item.(map[string]any); ok {
			status := strings.ToLower(strings.TrimSpace(progressString(execution, "status")))
			if status == "running" || status == "executing" || status == "pending" {
				return execution
			}
		}
	}
	return nil
}

func firstToolCall(data map[string]any) map[string]any {
	for _, item := range anySlice(data["tool_calls"]) {
		if call, ok := item.(map[string]any); ok {
			return call
		}
	}
	if _, ok := data["function"].(map[string]any); ok {
		return data
	}
	return nil
}

func toolCallName(call map[string]any) string {
	if name := progressString(call, "name"); name != "" {
		return name
	}
	fn, _ := call["function"].(map[string]any)
	return progressString(fn, "name")
}

func toolCallID(call map[string]any) string {
	return progressString(call, "id")
}

func anySlice(raw any) []any {
	switch value := raw.(type) {
	case []any:
		return value
	case []map[string]any:
		out := make([]any, 0, len(value))
		for _, item := range value {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}

func progressString(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	value, ok := data[key]
	if !ok || value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "<nil>" {
		return ""
	}
	return text
}

func progressInt(data map[string]any, key string) int {
	if data == nil {
		return 0
	}
	switch value := data[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func firstProgressLine(raw any) string {
	for _, line := range strings.Split(fmt.Sprint(raw), "\n") {
		if text := strings.TrimSpace(line); text != "" && text != "<nil>" {
			return text
		}
	}
	return ""
}

func truncateProgress(text string) string {
	text = strings.TrimSpace(text)
	if len(text) <= progressPreviewLimit {
		return text
	}
	return text[:progressPreviewLimit-3] + "..."
}
