package hooks

import "strings"

// ToolDenyMessage returns the policy or hook denial text shown for a rejected tool.
func ToolDenyMessage(decision ToolBeforeEachResult) string {
	if msg := strings.TrimSpace(decision.ApprovalReason); msg != "" {
		return msg
	}
	return "rejected: policy_denied"
}
