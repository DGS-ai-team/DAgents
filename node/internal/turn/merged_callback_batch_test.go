package turn

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

func TestBuildMergedCallbackBatch_toolLoopStillAssistantTool(t *testing.T) {
	hub := stream.NewHub(4, logx.Discard())
	orch := testOrchestrator(t, hub, &llm.MockClient{})

	history := []llm.Message{
		{Role: "user", Content: "run bg"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c1", Function: llm.ToolCallFunction{Name: "bash"}}}},
		{Role: "tool", ToolCallID: "c1", Content: "ok"},
	}
	async1 := queue.AsyncToolResultPayload{JobID: "job-1", ToolName: "bash_run", Status: "succeeded", ResultText: "done"}
	async2 := queue.AsyncToolResultPayload{JobID: "job-2", ToolName: "bash_run", Status: "failed", ErrorText: "exit 1"}
	entries := []SideEffectBatchEntry{
		{Built: orch.BuildAsyncSideEffectMessages("s", history, async1), Async: async1},
		{Built: orch.BuildAsyncSideEffectMessages("s", history, async2), Async: async2},
	}
	plan := BuildMergedCallbackBatch(entries, history)
	if len(plan.Messages) != 2 {
		t.Fatalf("merged messages = %d, want assistant+tool", len(plan.Messages))
	}
	toolMsg := plan.Messages[len(plan.Messages)-1]
	if toolMsg.Role != "tool" {
		t.Fatalf("last role = %q", toolMsg.Role)
	}
	var payload struct {
		Callbacks []map[string]any `json:"callbacks"`
	}
	if err := json.Unmarshal([]byte(toolMsg.Content), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Callbacks) != 2 {
		t.Fatalf("callbacks = %d", len(payload.Callbacks))
	}
}

func TestPlanSingleSideEffectApply_singleStillToolCallback(t *testing.T) {
	hub := stream.NewHub(4, logx.Discard())
	orch := testOrchestrator(t, hub, &llm.MockClient{})
	built := orch.BuildAsyncSideEffectMessages("s", nil, queue.AsyncToolResultPayload{
		JobID: "job-1", ToolName: "bash_run", Status: "succeeded", ResultText: "done",
	})
	history := []llm.Message{
		{Role: "user", Content: "run"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c1", Function: llm.ToolCallFunction{Name: "bash"}}}},
		{Role: "tool", ToolCallID: "c1", Content: "ok"},
	}
	plan := PlanSingleSideEffectApply(history, built)
	if len(plan.Messages) != 2 {
		t.Fatalf("single apply len = %d", len(plan.Messages))
	}
	if len(plan.Messages[0].ToolCalls) != 1 {
		t.Fatal("expected assistant tool_calls")
	}
	if plan.Messages[0].ToolCalls[0].Function.Name != "tool_callback" {
		t.Fatalf("tool name = %q", plan.Messages[0].ToolCalls[0].Function.Name)
	}
	if strings.Contains(plan.Messages[0].ToolCalls[0].Function.Name, "get_callback") {
		t.Fatal("single item must not use get_callback batch")
	}
}
