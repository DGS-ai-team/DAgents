package turn

import (
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func buildHITLRequiredPayload(items []PendingHITLItem) (message string, sseItems []map[string]any) {
	sseItems = make([]map[string]any, 0, len(items))
	hasUserInfo := false
	hasMemoryConflict := false
	hasApproval := false
	hasDuplicate := false
	for _, item := range items {
		if item.MemoryConflict != nil {
			hasMemoryConflict = true
			sseItems = append(sseItems, buildMemoryConflictHITLItem(item))
			continue
		}
		if tools.IsAskUserInformation(item.ToolCall.Function.Name) {
			hasUserInfo = true
			question, uiArgs := buildUserInformationPayload(item.ToolCall)
			sseItems = append(sseItems, map[string]any{
				"hitl_type":             hitlTypeUserInformation,
				"id":                    item.ToolCall.ID,
				"name":                  item.ToolCall.Function.Name,
				"content":               question,
				"user_information_args": uiArgs,
			})
			continue
		}
		hasApproval = true
		if item.DuplicateMeta != nil {
			hasDuplicate = true
		}
		approvalItem := buildApprovalToolItem(item.ToolCall, item.DuplicateMeta)
		approvalItem["hitl_type"] = hitlTypeExecuteTool
		sseItems = append(sseItems, approvalItem)
	}
	message = hitlRequiredMessage(hasUserInfo, hasApproval, hasDuplicate, hasMemoryConflict)
	return message, sseItems
}

func buildMemoryConflictHITLItem(item PendingHITLItem) map[string]any {
	meta := item.MemoryConflict
	desc := strings.TrimSpace(meta.ConflictDescription)
	if desc == "" {
		desc = "检测到长期记忆与新信息存在冲突，请选择保留方式。"
	}
	question := desc
	return map[string]any{
		"hitl_type": hitlTypeMemoryConflict,
		"id":        item.ToolCall.ID,
		"name":      item.ToolCall.Function.Name,
		"content":   question,
		"memory_conflict_meta": map[string]any{
			"existing":             meta.ExistingContent,
			"new_information":      meta.NewInformation,
			"conflict_description": meta.ConflictDescription,
			"merged_both":          meta.MergedBoth,
		},
		"user_information_args": map[string]any{
			"question": question,
			"options": []map[string]any{
				{"id": "keep_old", "label": "保留原有记忆"},
				{"id": "use_new", "label": "使用新记忆替换"},
				{"id": "keep_both", "label": "全部保留（合并）"},
			},
		},
	}
}

func hitlRequiredMessage(hasUserInfo, hasApproval, hasDuplicate, hasMemoryConflict bool) string {
	switch {
	case hasMemoryConflict && (hasUserInfo || hasApproval):
		return "检测到长期记忆冲突与其他待确认项，请依次处理。"
	case hasMemoryConflict:
		return "检测到长期记忆冲突，请选择保留方式后继续。"
	case hasUserInfo && hasApproval:
		return "检测到工具调用与用户询问，等待你的确认与回答后继续。"
	case hasUserInfo:
		return "Agent 需要补充信息。"
	case hasDuplicate:
		return "检测到工具调用；部分为短窗口内与上次完全相同的重复调用，请确认后再执行。"
	case hasApproval:
		return "检测到工具调用，等待用户确认后继续执行。"
	default:
		return "等待用户交互后继续。"
	}
}
