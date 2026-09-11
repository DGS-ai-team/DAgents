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
		{Role: "user", Content: "run async task"},
		{Role: "assistant", Content: "", ToolCalls: []llm.ToolCall{{
			ID: "call-bg-1", Type: "function",
			Function: llm.ToolCallFunction{Name: "browser_run_task", Arguments: `{}`},
		}}},
		{Role: "tool", ToolCallID: "call-async-1", Content: `{"ok":true,"detail":{"status":"accepted"}}`},
	}
	async1 := queue.AsyncToolResultPayload{JobID: "job-1", ToolName: "browser_run_task", Status: "succeeded", ResultText: "done"}
	async2 := queue.AsyncToolResultPayload{JobID: "job-2", ToolName: "browser_run_task", Status: "failed", ErrorText: "exit 1"}
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
