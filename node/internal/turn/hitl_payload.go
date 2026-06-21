package turn

import (
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func buildHITLRequiredPayload(items []PendingHITLItem) (message string, sseItems []map[string]any) {
	sseItems = make([]map[string]any, 0, len(items))
	hasUserInfo := false
	hasApproval := false
	hasDuplicate := false
	for _, item := range items {
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
	message = hitlRequiredMessage(hasUserInfo, hasApproval, hasDuplicate)
	return message, sseItems
}

func hitlRequiredMessage(hasUserInfo, hasApproval, hasDuplicate bool) string {
	switch {
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
