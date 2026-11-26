package childagent

import "testing"

func TestApplyChildProgressEventUsesConversationDeltaProtocol(t *testing.T) {
	progress := Progress{
		Phase:               "tool_executing",
		CurrentTool:         "bash_run",
		CurrentToolCallID:   "call-1",
		CurrentToolStatus:   "running",
		PendingApproval:     true,
		PendingApprovalData: map[string]any{"request_id": "approval-1"},
	}

	applyChildProgressEvent(&progress, "assistant", map[string]any{"content": "legacy"})
	applyChildProgressEvent(&progress, "reasoning", map[string]any{"content": "legacy"})
	if progress.Phase != "tool_executing" || progress.CurrentToolCallID != "call-1" || !progress.PendingApproval {
		t.Fatalf("legacy stream aliases changed progress: %+v", progress)
	}

	applyChildProgressEvent(&progress, "conversation_delta", map[string]any{"part": "reasoning", "content": "thinking"})
	if progress.Phase != "model_generating" || progress.CurrentTool != "" || progress.CurrentToolCallID != "" || progress.PendingApproval {
		t.Fatalf("conversation delta did not update progress: %+v", progress)
	}
}
