package hitl

import (
	"fmt"
	"strings"
)

const (
	HITLTypeUserInformation = "user_information"
	HITLTypeExecuteTool     = "execute_tool"
)

// UserInformationDataFromHITLItem 从 hitl_required item 构造 user_information_required 形 SSE data。
func UserInformationDataFromHITLItem(item map[string]any) map[string]any {
	if item == nil {
		return nil
	}
	data := map[string]any{
		"display_type": "normal_text",
	}
	if content, ok := item["content"].(string); ok && strings.TrimSpace(content) != "" {
		data["content"] = content
	}
	if args, ok := item["user_information_args"].(map[string]any); ok {
		data["user_information_args"] = args
	}
	return data
}

// ApprovalDataFromHITLBatch 从 hitl_required 与 execute_tool items 构造 approval_required 形 SSE data。
func ApprovalDataFromHITLBatch(batch map[string]any, executeItems []map[string]any) map[string]any {
	if len(executeItems) == 0 {
		return nil
	}
	toolCalls := make([]any, 0, len(executeItems))
	for _, item := range executeItems {
		toolCalls = append(toolCalls, item)
	}
	data := map[string]any{
		"approval_type": "execute_tool",
		"approval_args": map[string]any{"tool_calls": toolCalls},
		"display_type":  "normal_text",
	}
	copyHitlRoutingFields(batch, data)
	if hitlID := strings.TrimSpace(fmt.Sprint(batch["hitl_id"])); hitlID != "" && hitlID != "<nil>" {
		data["approval_id"] = hitlID
	}
	if msg := strings.TrimSpace(fmt.Sprint(batch["message"])); msg != "" && msg != "<nil>" {
		data["message"] = msg
	}
	return data
}

func copyHitlRoutingFields(batch map[string]any, dst map[string]any) {
	if batch == nil || dst == nil {
		return
	}
	if id := ChildSessionIDFromData(batch); id != "" {
		dst["child_session_id"] = id
	}
	if scope := strings.TrimSpace(fmt.Sprint(batch["hitl_scope"])); scope != "" && scope != "<nil>" {
		dst["hitl_scope"] = scope
	}
	if purpose := strings.TrimSpace(fmt.Sprint(batch["child_purpose"])); purpose != "" && purpose != "<nil>" {
		dst["child_purpose"] = purpose
	}
}

func hitlItemsFromData(raw any) []map[string]any {
	switch items := raw.(type) {
	case []any:
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case []map[string]any:
		return items
	default:
		return nil
	}
}

// ExpandHITLRequired 将 hitl_required SSE 展开为 Client 可入队的 user_information / approval 事件序列。
func ExpandHITLRequired(data map[string]any) (userInfo []map[string]any, approval map[string]any) {
	var executeItems []map[string]any
	for _, item := range hitlItemsFromData(data["items"]) {
		switch strings.TrimSpace(fmt.Sprint(item["hitl_type"])) {
		case HITLTypeUserInformation:
			if ui := UserInformationDataFromHITLItem(item); ui != nil {
				copyHitlRoutingFields(data, ui)
				userInfo = append(userInfo, ui)
			}
		case HITLTypeExecuteTool:
			executeItems = append(executeItems, item)
		}
	}
	if len(executeItems) > 0 {
		approval = ApprovalDataFromHITLBatch(data, executeItems)
	}
	return userInfo, approval
}
