package full

import (
	"strings"
	"testing"

	clihitl "github.com/DGS-ai-team/DAgents/client/internal/hitl"
	tuishared "github.com/DGS-ai-team/DAgents/client/internal/tui/shared"
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

func childApprovalData(childID, callID string) map[string]any {
	data := approvalData(callID)
	data["child_session_id"] = childID
	return data
}

func TestEnqueueApprovalKeepsDifferentChildren(t *testing.T) {
	m := &model{children: newChildAgentTracker()}
	m.enqueueApproval(childApprovalData("child-a", "call_a"))
	m.enqueueApproval(childApprovalData("child-b", "call_b"))
	if len(m.hitlQueue) != 2 {
		t.Fatalf("queue len = %d, want 2", len(m.hitlQueue))
	}
	items := clihitl.ExtractToolApprovals(m.hitlQueue[0].data)
	if len(items) != 1 || items[0].CallID != "call_a" {
		t.Fatalf("head = %v", items)
	}
}

func TestEnqueueUserInfoKeepsApprovals(t *testing.T) {
	m := &model{children: newChildAgentTracker()}
	m.enqueueApproval(childApprovalData("child-a", "call_a"))
	m.enqueueUserInfo(map[string]any{
		"user_information_args": map[string]any{
			"tool_call_id": "call-ask",
			"question":     "确认？",
		},
	})
	if len(m.hitlQueue) != 2 {
		t.Fatalf("queue len = %d, want 2", len(m.hitlQueue))
	}
	if m.hitlQueue[0].kind != hitlPendingApproval || m.hitlQueue[1].kind != hitlPendingUserInfo {
		t.Fatalf("kinds = %v, %v", m.hitlQueue[0].kind, m.hitlQueue[1].kind)
	}
}

func TestShowUserInfoWritesMergedTranscript(t *testing.T) {
	m := &model{
		transcript: tuishared.NewTranscript(0),
		children:   newChildAgentTracker(),
	}
	data := map[string]any{
		"user_information_args": map[string]any{
			"tool_call_id": "call-ask",
			"question":     "请确认语言",
		},
	}
	m.initUserInfoState(data)
	m.appendUserInfoTranscript()
	lines := m.transcript.Lines()
	if len(lines) != 2 {
		t.Fatalf("lines=%v", lines)
	}
	if !strings.Contains(lines[0], "Agent 询问") || !strings.Contains(lines[1], "请确认语言") {
		t.Fatalf("merged block=%v", lines)
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
