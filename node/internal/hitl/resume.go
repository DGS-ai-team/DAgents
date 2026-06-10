package hitl

import (
	"fmt"
	"strings"
)

// ApprovalPlan 解析 resume 后对 pending tool call 的批准/拒绝集合。
type ApprovalPlan struct {
	Approved              map[string]struct{}
	Rejected              map[string]struct{}
	TriggerSessionTargets map[string]string // call_id -> same_session | new_session | latest_active_session
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
		targets, err := parseTriggerSessionTargets(value["trigger_session_targets"], pending)
		if err != nil {
			return ApprovalPlan{}, err
		}
		plan.TriggerSessionTargets = targets
		return plan, nil
	case "reject", "rejected":
		plan := ApprovalPlan{Approved: make(map[string]struct{}), Rejected: make(map[string]struct{})}
		for id := range pending {
			plan.Rejected[id] = struct{}{}
		}
		if raw := value["trigger_session_targets"]; raw != nil {
			if _, err := parseTriggerSessionTargets(raw, pending); err != nil {
				return ApprovalPlan{}, err
			}
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
		targets, err := parseTriggerSessionTargets(value["trigger_session_targets"], pending)
		if err != nil {
			return ApprovalPlan{}, err
		}
		for id := range targets {
			if _, ok := plan.Approved[id]; !ok {
				return ApprovalPlan{}, fmt.Errorf("trigger_session_targets for non-approved id: %s", id)
			}
		}
		plan.TriggerSessionTargets = targets
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

// ResumeValueKind 推断 resume_value 的高层类型，仅供日志与诊断（非严格校验）。
func ResumeValueKind(value map[string]any) string {
	if value == nil {
		return "nil"
	}
	typ := strings.ToLower(strings.TrimSpace(fmt.Sprint(value["type"])))
	switch typ {
	case "selection", "approve", "approved", "reject", "rejected":
		return "approval"
	case "user_information":
		return "user_information"
	case "":
		if _, ok := value["approved"]; ok {
			return "approval"
		}
		if _, ok := value["rejected"]; ok {
			return "approval"
		}
		if _, ok := value["answer"]; ok {
			return "user_information"
		}
		if _, ok := value["selected_options"]; ok {
			return "user_information"
		}
		return "unknown"
	default:
		return "unknown:" + typ
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

// TriggerSessionTarget 返回 call 的 trigger 投递目标；未指定时返回空（由编排器 fallback same_session）。
func (p ApprovalPlan) TriggerSessionTarget(id string) string {
	if p.TriggerSessionTargets == nil {
		return ""
	}
	return strings.TrimSpace(p.TriggerSessionTargets[id])
}
