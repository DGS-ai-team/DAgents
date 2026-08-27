package turn

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

func TestBuildMergedCallbackBatch_twoAsync(t *testing.T) {
	hub := stream.NewHub(4, logx.Discard())
	orch := testOrchestrator(t, hub, &llm.MockClient{})

	history := []llm.Message{
		{Role: "user", Content: "run bg"},
		{Role: "assistant", Content: "", ToolCalls: []llm.ToolCall{{
			ID: "call-bg-1", Type: "function",
			Function: llm.ToolCallFunction{Name: "bash_run", Arguments: `{"run_in_background":true}`},
		}}},
		{Role: "tool", ToolCallID: "call-bg-1", Content: "[TOOL_BACKGROUND] job_id=job-1"},
	}
	async1 := queue.AsyncToolResultPayload{JobID: "job-1", ToolName: "bash_run", Status: "succeeded", ResultText: "done"}
	async2 := queue.AsyncToolResultPayload{JobID: "job-2", ToolName: "bash_run", Status: "failed", ErrorText: "exit 1"}
	e1 := SideEffectBatchEntry{
		Built: orch.BuildAsyncSideEffectMessages("s", history, async1),
		Async: async1,
	}
	e2 := SideEffectBatchEntry{
		Built: orch.BuildAsyncSideEffectMessages("s", history, async2),
		Async: async2,
	}
	plan := BuildMergedCallbackBatch([]SideEffectBatchEntry{e1, e2}, history)
	if len(plan.Messages) != 2 {
		t.Fatalf("merged messages = %d, want assistant+tool", len(plan.Messages))
	}
	if plan.Mode != "merged_get_callback" {
		t.Fatalf("mode = %q", plan.Mode)
	}
}
