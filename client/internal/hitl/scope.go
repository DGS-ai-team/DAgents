package hitl

import (
	"fmt"
	"strings"
)

// ApprovalQueueKey 为 HITL 队列中 approval 的去重键（子按 child_session_id，父按 approval_id）。
func ApprovalQueueKey(data map[string]any) string {
	if data == nil {
		return "parent:"
	}
	if id := ChildSessionIDFromData(data); id != "" {
		return "child:" + id
	}
	if id, _ := data["approval_id"].(string); strings.TrimSpace(id) != "" {
		return "parent:" + strings.TrimSpace(id)
	}
	return "parent:"
}

// ChildSessionIDFromData 从 SSE data 提取子 session id。
func ChildSessionIDFromData(data map[string]any) string {
	if data == nil {
		return ""
	}
	id, _ := data["child_session_id"].(string)
	return strings.TrimSpace(id)
}

// IsTemporaryAgentApproval 判断 approval_required 是否属于临时 Agent 工具审批（非 A2A）。
func IsTemporaryAgentApproval(data map[string]any) bool {
	if data == nil {
		return false
	}
	scope := strings.TrimSpace(fmt.Sprint(data["hitl_scope"]))
	if scope == "temporary_agent" {
		return true
	}
	return ChildSessionIDFromData(data) != ""
}

// IsChildRuntimeEvent 判断是否为子 Agent turn 产生的 SSE（应隐藏于主 transcript）。
func IsChildRuntimeEvent(data map[string]any) bool {
	return ChildSessionIDFromData(data) != ""
}

// ApprovalHeader 返回审批面板标题。
func ApprovalHeader(data map[string]any) string {
	if !IsTemporaryAgentApproval(data) {
		return "工具审批"
	}
	purpose := strings.TrimSpace(fmt.Sprint(data["child_purpose"]))
	if purpose == "" {
		purpose = "临时 Agent"
	}
	childID := ChildSessionIDFromData(data)
	short := childID
	if len(short) > 14 {
		short = short[:14] + "…"
	}
	if short != "" {
		return fmt.Sprintf("临时 Agent 审批 · %s · %s", purpose, short)
	}
	return "临时 Agent 审批 · " + purpose
}

func attachApprovalRouting(data map[string]any, rv map[string]any) map[string]any {
	if rv == nil {
		rv = map[string]any{}
	}
	if id := ChildSessionIDFromData(data); id != "" {
		rv["child_session_id"] = id
	}
	if id, _ := data["approval_id"].(string); strings.TrimSpace(id) != "" {
		rv["approval_id"] = strings.TrimSpace(id)
	}
	return rv
}

// ShouldSkipChildRuntimeDisplay 判断是否隐藏子 Agent turn 产生的 SSE（审批与生命周期除外）。
func ShouldSkipChildRuntimeDisplay(eventType string, data map[string]any) bool {
	if !IsChildRuntimeEvent(data) {
		return false
	}
	switch eventType {
	case "approval_required", "temporary_agent_created", "temporary_agent_completed", "temporary_agent_cancelled":
		return false
	default:
		return true
	}
}

// FormatChildLifecycleLine 格式化子 Agent 生命周期系统提示。
func FormatChildLifecycleLine(eventType string, data map[string]any) string {
	id := strings.TrimSpace(fmt.Sprint(data["child_session_id"]))
	short := id
	if len(short) > 16 {
		short = short[:16] + "…"
	}
	purpose := strings.TrimSpace(fmt.Sprint(data["purpose"]))
	switch eventType {
	case "temporary_agent_created":
		if purpose != "" {
			return fmt.Sprintf("临时 Agent 已创建 · %s · %s", purpose, short)
		}
		return fmt.Sprintf("临时 Agent 已创建 · %s", short)
	case "temporary_agent_completed":
		status := strings.TrimSpace(fmt.Sprint(data["status"]))
		if status == "" {
			status = "completed"
		}
		return fmt.Sprintf("临时 Agent 已结束 · %s · %s", short, status)
	case "temporary_agent_cancelled":
		reason := strings.TrimSpace(fmt.Sprint(data["reason"]))
		if reason != "" {
			return fmt.Sprintf("临时 Agent 已取消 · %s · %s", short, reason)
		}
		return fmt.Sprintf("临时 Agent 已取消 · %s", short)
	default:
		return ""
	}
}
