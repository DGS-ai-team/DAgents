package full

import (
	"testing"

	clihitl "github.com/DGS-ai-team/DAgents/client/internal/hitl"
)

func approvalData(callID string) map[string]any {
	return map[string]any{
		"approval_args": map[string]any{
			"tool_calls": []any{
				map[string]any{"id": callID, "name": "bash_run", "arguments": map[string]any{}},
			},
		},
	}
}

func TestEnqueueApprovalReplacesStaleApproval(t *testing.T) {
	m := &model{children: newChildAgentTracker()}
	m.enqueueApproval(approvalData("call_old"))
	m.enqueueApproval(approvalData("call_new"))

	if len(m.hitlQueue) != 1 {
		t.Fatalf("queue len = %d, want 1", len(m.hitlQueue))
	}
	items := clihitl.ExtractToolApprovals(m.hitlQueue[0].data)
	if len(items) != 1 || items[0].CallID != "call_new" {
		t.Fatalf("pending call_id = %v, want call_new", items)
	}
}

func TestInvalidateHITLForUserMessageClearsQueue(t *testing.T) {
	m := &model{children: newChildAgentTracker()}
	m.enqueueApproval(approvalData("call_old"))
	m.mode = modeApproval
	m.hitlData = approvalData("call_old")

	m.invalidateHITLForUserMessage()

	if len(m.hitlQueue) != 0 {
		t.Fatalf("queue len = %d, want 0", len(m.hitlQueue))
	}
	if m.mode != modeChat {
		t.Fatalf("mode = %v, want modeChat", m.mode)
	}
	if m.hitlData != nil {
		t.Fatal("hitlData should be nil after invalidate")
	}
}
