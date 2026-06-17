package hitl

import (
	"fmt"
	"strings"

	tuishared "github.com/DGS-ai-team/DAgents/client/internal/tui/shared"
)

// A2APeerLabel 返回对端 Agent 展示名（优先 card name，否则 agent_id）。
func A2APeerLabel(data map[string]any) string {
	if data == nil {
		return ""
	}
	if name := mapStringField(data, "a2a_peer_agent_name"); name != "" {
		return name
	}
	return mapStringField(data, "a2a_peer_agent_id")
}

// A2ARelayApprovedSummary 返回 A2A 中继工具审批提交后的终态摘要。
func A2ARelayApprovedSummary(peerLabel string, approved bool) string {
	return tuishared.FormatA2ARelayApprovedSummary(peerLabel, approved)
}

// A2ARelayToolSuffix 返回工具行尾部的 from 标识（纯文本，供终端展示）。
func A2ARelayToolSuffix(data map[string]any) string {
	return tuishared.FormatA2ARelayPeerSuffix(A2APeerLabel(data))
}

// ParseApprovalResumeSelection 从 resume_value 解析 approved/rejected call id 集合。
func ParseApprovalResumeSelection(resume, hitlData map[string]any) (approved, rejected map[string]bool) {
	approved = make(map[string]bool)
	rejected = make(map[string]bool)
	if resume == nil {
		return approved, rejected
	}
	typ := strings.TrimSpace(fmt.Sprint(resume["type"]))
	switch typ {
	case "approve":
		for _, it := range ExtractToolApprovals(hitlData) {
			approved[it.CallID] = true
		}
	case "reject":
		for _, it := range ExtractToolApprovals(hitlData) {
			rejected[it.CallID] = true
		}
	case "selection":
		for _, id := range stringSliceField(resume["approved"]) {
			approved[id] = true
		}
		for _, id := range stringSliceField(resume["rejected"]) {
			rejected[id] = true
		}
	}
	return approved, rejected
}

func stringSliceField(raw any) []string {
	switch v := raw.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, s := range v {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" && s != "<nil>" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
