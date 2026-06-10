package hitl

import (
	"fmt"
	"strings"

	tuishared "github.com/DGS-ai-team/DAgents/client/internal/tui/shared"
)

const (
	ApprovalModeTriggerSession = "trigger_session"

	TriggerSessionSame         = "same_session"
	TriggerSessionNew          = "new_session"
	TriggerSessionLatestActive = "latest_active_session"
)

// TriggerSessionOption 为 trigger 审批四选项展示与 resume 值。
type TriggerSessionOption struct {
	Label  string
	Target string // 空表示不同意
}

// TriggerSessionOptions 硬编码四档审批选项。
func TriggerSessionOptions() []TriggerSessionOption {
	return []TriggerSessionOption{
		{Label: "同意（在同一个会话中触发）", Target: TriggerSessionSame},
		{Label: "同意（在新会话中触发）", Target: TriggerSessionNew},
		{Label: "同意（在最新一个活跃会话中触发）", Target: TriggerSessionLatestActive},
		{Label: "不同意", Target: ""},
	}
}

// IsTriggerSessionApprovalItem 判断单条 tool 是否使用四选项审批。
func IsTriggerSessionApprovalItem(item ToolApprovalItem) bool {
	if strings.TrimSpace(item.ApprovalMode) == ApprovalModeTriggerSession {
		return true
	}
	name := strings.ToLower(strings.TrimSpace(item.Name))
	return name == "trigger_create" || name == "trigger_fire"
}

// HasTriggerSessionApprovalItems 判断本批审批是否含 trigger 四选项工具。
func HasTriggerSessionApprovalItems(items []ToolApprovalItem) bool {
	for _, it := range items {
		if IsTriggerSessionApprovalItem(it) {
			return true
		}
	}
	return false
}

// FormatTriggerSessionApprovalInteractive 生成 trigger 四选项审批面板文本。
func FormatTriggerSessionApprovalInteractive(
	data map[string]any,
	items []ToolApprovalItem,
	decided map[string]string,
	rejected map[string]bool,
	itemCursor int,
	optionCursor int,
) string {
	options := TriggerSessionOptions()
	if optionCursor < 0 {
		optionCursor = 0
	}
	if optionCursor >= len(options) {
		optionCursor = len(options) - 1
	}
	var b strings.Builder
	if msg, ok := data["message"].(string); ok && strings.TrimSpace(msg) != "" {
		b.WriteString(strings.TrimSpace(msg) + "\n\n")
	}
	current := triggerSessionCurrentItem(items, decided, rejected)
	if current != nil {
		title := tuisharedToolName(*current)
		b.WriteString(fmt.Sprintf("待审批: %s (%s)\n", title, current.CallID))
		writeApprovalItemDetails(&b, *current, "  ")
		b.WriteString("\n")
	}
	for i, opt := range options {
		cursor := " "
		if i == optionCursor {
			cursor = ">"
		}
		fmt.Fprintf(&b, "%s %s\n", cursor, opt.Label)
	}
	_ = itemCursor
	b.WriteString("\n↑/↓ 选择 · Enter 确认 · Y 同会话同意 · N/Esc 不同意")
	return strings.TrimRight(b.String(), "\n")
}

func tuisharedToolName(it ToolApprovalItem) string {
	return tuishared.ToolDisplayName(it.Name, it.Arguments)
}

func triggerSessionCurrentItem(items []ToolApprovalItem, decided map[string]string, rejected map[string]bool) *ToolApprovalItem {
	for i := range items {
		it := items[i]
		if !IsTriggerSessionApprovalItem(it) {
			continue
		}
		if rejected[it.CallID] {
			continue
		}
		if _, ok := decided[it.CallID]; ok {
			continue
		}
		return &items[i]
	}
	return nil
}

// BuildTriggerSessionApprovalResume 构造含 trigger_session_targets 的 selection resume。
func BuildTriggerSessionApprovalResume(
	data map[string]any,
	items []ToolApprovalItem,
	decided map[string]string,
	rejected map[string]bool,
) map[string]any {
	approved := make([]string, 0)
	rejectedIDs := make([]string, 0)
	targets := make(map[string]string)
	for _, it := range items {
		if rejected[it.CallID] {
			rejectedIDs = append(rejectedIDs, it.CallID)
			continue
		}
		if target, ok := decided[it.CallID]; ok && target != "" {
			approved = append(approved, it.CallID)
			if IsTriggerSessionApprovalItem(it) {
				targets[it.CallID] = target
			}
			continue
		}
		if !IsTriggerSessionApprovalItem(it) {
			rejectedIDs = append(rejectedIDs, it.CallID)
		}
	}
	rv := map[string]any{
		"type":     "selection",
		"approved": approved,
		"rejected": rejectedIDs,
	}
	if len(targets) > 0 {
		rv["trigger_session_targets"] = targets
	}
	return attachApprovalRouting(data, rv)
}

// BuildTriggerSessionQuickResume 快捷 Y/N：Y=同会话同意全部 trigger 项。
func BuildTriggerSessionQuickResume(data map[string]any, items []ToolApprovalItem, approveSameSession bool) map[string]any {
	if !approveSameSession {
		rejected := make([]string, 0, len(items))
		for _, it := range items {
			rejected = append(rejected, it.CallID)
		}
		return attachApprovalRouting(data, map[string]any{
			"type":     "selection",
			"approved": []string{},
			"rejected": rejected,
		})
	}
	decided := make(map[string]string)
	rejected := make(map[string]bool)
	for _, it := range items {
		if IsTriggerSessionApprovalItem(it) {
			decided[it.CallID] = TriggerSessionSame
		} else {
			rejected[it.CallID] = true
		}
	}
	return BuildTriggerSessionApprovalResume(data, items, decided, rejected)
}
