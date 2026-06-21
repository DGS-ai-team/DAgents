package turn

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

func TestPlanSingleSideEffectApply_tailToolAppendsCallbackNotUser(t *testing.T) {
	hub := stream.NewHub(8, logx.Discard())
	orch := testOrchestrator(t, hub, &llm.MockClient{})

	history := []llm.Message{
		{Role: "user", Content: "run bg"},
		{Role: "assistant", Content: "ok", ToolCalls: []llm.ToolCall{{
			ID: "call-bg-1", Type: "function",
			Function: llm.ToolCallFunction{Name: "bash_run", Arguments: `{"command":"sleep 1"}`},
		}}},
		{Role: "tool", ToolCallID: "call-bg-1", Content: "accepted"},
	}

	built := orch.BuildSideEffectMessages(SideEffectAsync, "sess-1", history, queue.AsyncToolResultPayload{
		JobID: "job-1", ToolName: "bash_run", ToolCallID: "async-job-1", Status: "succeeded", ResultText: "done",
	}, "", "")
	plan := PlanSingleSideEffectApply(history, built)
	if len(plan.Messages) != 2 {
		t.Fatalf("plan messages = %d, want 2 (assistant+tool)", len(plan.Messages))
	}
	if plan.Messages[0].Role != "assistant" || plan.Messages[1].Role != "tool" {
		t.Fatalf("plan = %+v", plan.Messages)
	}
	for _, m := range plan.Messages {
		if m.Role == "user" {
			t.Fatal("tailTool apply must not write user message")
		}
	}
}
