package hitl

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ToolApprovalItem 为待审批的单条 tool call 摘要。
type ToolApprovalItem struct {
	CallID    string
	Name      string
	RawArgs   string
	Arguments map[string]any
	Reason    string
	Risk      string
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
		rawArgs := mapStringField(m, "raw_arguments")
		var argMap map[string]any
		if raw, ok := m["arguments"].(map[string]any); ok {
			argMap = raw
		}
		if rawArgs == "" {
			if s, ok := m["arguments"].(string); ok {
				rawArgs = strings.TrimSpace(s)
			} else if len(argMap) > 0 {
				b, _ := json.Marshal(argMap)
				rawArgs = string(b)
			}
		}
		item := ToolApprovalItem{
			CallID:    callID,
			Name:      name,
			RawArgs:   rawArgs,
			Arguments: argMap,
			Reason:    mapStringField(m, "approval_reason"),
			Risk:      mapStringField(m, "risk_level"),
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
		writeApprovalItemDetails(&b, it, "     ")
	}
	return strings.TrimRight(b.String(), "\n")
}

// BuildApprovalResume 构造审批 resume_value；approveAll=true 批准全部 pending。
func BuildApprovalResume(data map[string]any, approveAll bool) map[string]any {
	items := ExtractToolApprovals(data)
	if len(items) == 0 {
		if approveAll {
			return attachApprovalRouting(data, map[string]any{"type": "approve"})
		}
		return attachApprovalRouting(data, map[string]any{"type": "reject"})
	}
	if approveAll {
		approved := make([]string, 0, len(items))
		for _, it := range items {
			approved = append(approved, it.CallID)
		}
		rejected := []string{}
		return attachApprovalRouting(data, map[string]any{"type": "selection", "approved": approved, "rejected": rejected})
	}
	rejected := make([]string, 0, len(items))
	for _, it := range items {
		rejected = append(rejected, it.CallID)
	}
	return attachApprovalRouting(data, map[string]any{"type": "selection", "approved": []string{}, "rejected": rejected})
}

// ResolveApprovalSelection 解析 Enter 提交前的勾选状态。

// 逻辑：
// 1. 复制已有勾选；
// 2. 若用户未 Space 勾选任何项，则默认批准当前光标所在工具（避免 Enter 误提交成全拒绝）；
// 3. 返回供 BuildApprovalSelectionResume 使用的 map。

// 关键边界：用户显式 Space 取消勾选后，即使光标在该行也不自动批准。
func ResolveApprovalSelection(items []ToolApprovalItem, selected map[string]bool, cursor int) map[string]bool {
	out := make(map[string]bool, len(selected)+1)
	for id, on := range selected {
		out[id] = on
	}
	hasApproved := false
	for _, on := range out {
		if on {
			hasApproved = true
			break
		}
	}
	if !hasApproved && cursor >= 0 && cursor < len(items) {
		out[items[cursor].CallID] = true
	}
	return out
}

// BuildApprovalSelectionResume 按逐条勾选构造 selection resume；未在 approved 中的视为 rejected。
func BuildApprovalSelectionResume(data map[string]any, approved map[string]bool) map[string]any {
	items := ExtractToolApprovals(data)
	if len(items) == 0 {
		return attachApprovalRouting(data, map[string]any{"type": "reject"})
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
	return attachApprovalRouting(data, map[string]any{
		"type":     "selection",
		"approved": approvedIDs,
		"rejected": rejectedIDs,
	})
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
		writeApprovalItemDetails(&b, it, "     ")
	}
	b.WriteString("\n↑/↓ 移动 · Space 切换 · Enter 确认（未勾选时默认批准光标项） · Y 全批准 · N/Esc 全拒绝")
	return strings.TrimRight(b.String(), "\n")
}
