package hooks

import "testing"

func TestToolDenyMessage(t *testing.T) {
	if got := ToolDenyMessage(ToolBeforeEachResult{ApprovalReason: "blocked by policy"}); got != "blocked by policy" {
		t.Fatalf("reason = %q", got)
	}
	if got := ToolDenyMessage(ToolBeforeEachResult{}); got != "rejected: policy_denied" {
		t.Fatalf("fallback = %q", got)
	}
}
