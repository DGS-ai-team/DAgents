package hitl

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ToolApprovalItem 为待审批的单条 tool call 摘要。
type ToolApprovalItem struct {
	CallID   string
	Name     string
	RawArgs  string
	Reason   string
	Risk     string
}

// ExtractToolApprovals 从 approval_required SSE data 解析 tool_calls 列表。

// 逻辑：
// 1. 读 approval_args.tool_calls；
// 2. 过滤无 id 的项；
// 3. 缺 raw_arguments 时用 arguments JSON 兜底。
func ExtractToolApprovals(data map[string]any) []ToolApprovalItem {
	args, _ := data["approval_args"].(map[string]any)
	if args == nil {
		return nil
	}
	rawCalls, _ := args["tool_calls"].([]any)
	if len(rawCalls) == 0 {
		return nil
	}
	out := make([]ToolApprovalItem, 0, len(rawCalls))
	for _, raw := range rawCalls {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		callID := strings.TrimSpace(fmt.Sprint(m["id"]))
		name := strings.TrimSpace(fmt.Sprint(m["name"]))
		if callID == "" {
			continue
		}
		if name == "" {
			name = "unknown"
		}
		rawArgs := strings.TrimSpace(fmt.Sprint(m["raw_arguments"]))
		if rawArgs == "" {
			if argMap, ok := m["arguments"].(map[string]any); ok {
				b, _ := json.Marshal(argMap)
				rawArgs = string(b)
			}
		}
		item := ToolApprovalItem{
			CallID:  callID,
			Name:    name,
			RawArgs: rawArgs,
			Reason:  strings.TrimSpace(fmt.Sprint(m["approval_reason"])),
			Risk:    strings.TrimSpace(fmt.Sprint(m["risk_level"])),
		}
		out = append(out, item)
	}
	return out
}

// FormatApprovalPrompt 生成终端/全屏 TUI 用的审批说明文本。
func FormatApprovalPrompt(data map[string]any) string {
	if msg, ok := data["message"].(string); ok && strings.TrimSpace(msg) != "" {
		return strings.TrimSpace(msg)
	}
	items := ExtractToolApprovals(data)
	if len(items) == 0 {
		return "检测到待审批工具调用"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("待审批工具 (%d):\n", len(items)))
	for i, it := range items {
		b.WriteString(fmt.Sprintf("  %d. %s (%s)\n", i+1, it.Name, it.CallID))
		if it.Reason != "" {
			b.WriteString("     原因: " + it.Reason + "\n")
		}
		if it.Risk != "" {
			b.WriteString("     风险: " + it.Risk + "\n")
		}
		if it.RawArgs != "" {
			args := it.RawArgs
			if len(args) > 120 {
				args = args[:120] + "..."
			}
			b.WriteString("     参数: " + args + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// BuildApprovalResume 构造审批 resume_value；approveAll=true 批准全部 pending。
func BuildApprovalResume(data map[string]any, approveAll bool) map[string]any {
	items := ExtractToolApprovals(data)
	if len(items) == 0 {
		if approveAll {
			return map[string]any{"type": "approve"}
		}
		return map[string]any{"type": "reject"}
	}
	if approveAll {
		approved := make([]string, 0, len(items))
		for _, it := range items {
			approved = append(approved, it.CallID)
		}
		rejected := []string{}
		return map[string]any{"type": "selection", "approved": approved, "rejected": rejected}
	}
	rejected := make([]string, 0, len(items))
	for _, it := range items {
		rejected = append(rejected, it.CallID)
	}
	return map[string]any{"type": "selection", "approved": []string{}, "rejected": rejected}
}

// BuildApprovalSelectionResume 按逐条勾选构造 selection resume；未在 approved 中的视为 rejected。
func BuildApprovalSelectionResume(data map[string]any, approved map[string]bool) map[string]any {
	items := ExtractToolApprovals(data)
	if len(items) == 0 {
		return map[string]any{"type": "reject"}
	}
	approvedIDs := make([]string, 0)
	rejectedIDs := make([]string, 0)
	for _, it := range items {
		if approved[it.CallID] {
			approvedIDs = append(approvedIDs, it.CallID)
		} else {
			rejectedIDs = append(rejectedIDs, it.CallID)
		}
	}
	return map[string]any{
		"type":     "selection",
		"approved": approvedIDs,
		"rejected": rejectedIDs,
	}
}

// FormatApprovalInteractive 生成交互式审批文本（含光标与勾选状态）。
func FormatApprovalInteractive(data map[string]any, approved map[string]bool, cursor int) string {
	items := ExtractToolApprovals(data)
	if len(items) == 0 {
		return FormatApprovalPrompt(data)
	}
	var b strings.Builder
	if msg, ok := data["message"].(string); ok && strings.TrimSpace(msg) != "" {
		b.WriteString(strings.TrimSpace(msg) + "\n\n")
	}
	b.WriteString(fmt.Sprintf("待审批工具 (%d):\n", len(items)))
	for i, it := range items {
		mark := "[ ]"
		if approved[it.CallID] {
			mark = "[x]"
		}
		prefix := "  "
		if i == cursor {
			prefix = "> "
		}
		fmt.Fprintf(&b, "%s%s %s (%s)\n", prefix, mark, it.Name, it.CallID)
		if it.Reason != "" {
			b.WriteString("     原因: " + it.Reason + "\n")
		}
	}
	b.WriteString("\n↑/↓ 移动 · Space 切换 · Enter 确认 · Y 全批准 · N/Esc 全拒绝")
	return strings.TrimRight(b.String(), "\n")
}
