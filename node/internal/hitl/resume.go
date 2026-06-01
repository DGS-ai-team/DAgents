package hitl

import (
	"fmt"
	"strings"
)

// ApprovalPlan 解析 resume 后对 pending tool call 的批准/拒绝集合。
type ApprovalPlan struct {
	Approved map[string]struct{}
	Rejected map[string]struct{}
}

// ParseApprovalResume 解析审批 resume_value（兼容 approve/reject/selection）。
func ParseApprovalResume(value map[string]any, pendingIDs []string) (ApprovalPlan, error) {
	if value == nil {
		return ApprovalPlan{}, fmt.Errorf("empty resume_value")
	}
	typ := strings.ToLower(strings.TrimSpace(fmt.Sprint(value["type"])))
	pending := make(map[string]struct{}, len(pendingIDs))
	for _, id := range pendingIDs {
		pending[id] = struct{}{}
	}

	switch typ {
	case "approve", "approved":
		plan := ApprovalPlan{Approved: make(map[string]struct{}), Rejected: make(map[string]struct{})}
		for id := range pending {
			plan.Approved[id] = struct{}{}
		}
		return plan, nil
	case "reject", "rejected":
		plan := ApprovalPlan{Approved: make(map[string]struct{}), Rejected: make(map[string]struct{})}
		for id := range pending {
			plan.Rejected[id] = struct{}{}
		}
		return plan, nil
	case "selection":
		plan := ApprovalPlan{Approved: make(map[string]struct{}), Rejected: make(map[string]struct{})}
		for _, raw := range toStringSlice(value["approved"]) {
			if _, ok := pending[raw]; !ok {
				return ApprovalPlan{}, fmt.Errorf("unknown approved id: %s", raw)
			}
			plan.Approved[raw] = struct{}{}
		}
		for _, raw := range toStringSlice(value["rejected"]) {
			if _, ok := pending[raw]; !ok {
				return ApprovalPlan{}, fmt.Errorf("unknown rejected id: %s", raw)
			}
			plan.Rejected[raw] = struct{}{}
		}
		if len(plan.Approved)+len(plan.Rejected) != len(pending) {
			return ApprovalPlan{}, fmt.Errorf("selection must cover all pending tool calls")
		}
		return plan, nil
	default:
		// 兼容 kind/decision 写法
		if strings.EqualFold(fmt.Sprint(value["decision"]), "approved") {
			return ParseApprovalResume(map[string]any{"type": "approve"}, pendingIDs)
		}
		if strings.EqualFold(fmt.Sprint(value["decision"]), "denied") {
			return ParseApprovalResume(map[string]any{"type": "reject"}, pendingIDs)
		}
		return ApprovalPlan{}, fmt.Errorf("unsupported approval resume type: %q", typ)
	}
}

// ParseUserInformationResume 解析用户回答 resume。
func ParseUserInformationResume(value map[string]any, toolCallID string) (content string, err error) {
	if value == nil {
		return "", fmt.Errorf("empty resume_value")
	}
	typ := strings.ToLower(strings.TrimSpace(fmt.Sprint(value["type"])))
	if typ != "" && typ != "user_information" {
		return "", fmt.Errorf("unsupported user_information resume type: %q", typ)
	}
	if got, ok := value["tool_call_id"].(string); ok {
		if strings.TrimSpace(got) != "" && strings.TrimSpace(got) != toolCallID {
			return "", fmt.Errorf("tool_call_id mismatch")
		}
	}
	if cancelled, _ := value["cancelled"].(bool); cancelled {
		return "[USER_INFORMATION_CANCELLED] 用户取消了信息补充。", nil
	}
	answer := strings.TrimSpace(fmt.Sprint(value["answer"]))
	selected := toStringSlice(value["selected_options"])
	if answer == "" && len(selected) == 0 {
		return "", fmt.Errorf("answer is required")
	}
	return formatUserInformationResult(answer, selected), nil
}

func formatUserInformationResult(answer string, selected []string) string {
	return fmt.Sprintf("[USER_INFORMATION]\nanswer=%q\nselected_options=%v", answer, selected)
}

func toStringSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			s := strings.TrimSpace(fmt.Sprint(item))
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func (p ApprovalPlan) IsApproved(id string) bool {
	_, ok := p.Approved[id]
	return ok
}
