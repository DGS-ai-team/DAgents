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

func TestBuildMergedCallbackBatch_asyncAndTrigger(t *testing.T) {
	hub := stream.NewHub(4, logx.Discard())
	orch := testOrchestrator(t, hub, &llm.MockClient{})

	history := []llm.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "ok"},
	}
	async := queue.AsyncToolResultPayload{JobID: "job-1", ToolName: "bash_run", Status: "succeeded", ResultText: "done"}
	entries := []SideEffectBatchEntry{
		{
			Kind:  SideEffectAsync,
			Built: orch.BuildSideEffectMessages(SideEffectAsync, "s", async, "", ""),
			Async: async,
		},
		{
			Kind:           SideEffectExternalMessage,
			Built:          orch.BuildSideEffectMessages(SideEffectExternalMessage, "s", queue.AsyncToolResultPayload{}, "trigger text", llm.UserNameTrigger),
			MessageContent: "trigger text",
			UserName:       llm.UserNameTrigger,
			TriggerID:      "trig-1",
		},
	}
	plan := BuildMergedCallbackBatch(entries, history)
	if plan.Mode != "merged_get_callback" {
		t.Fatalf("mode = %q", plan.Mode)
	}
	if len(plan.Messages) != 3 {
		t.Fatalf("bridge merge len = %d, want user+assistant+tool", len(plan.Messages))
	}
	if plan.Messages[0].Role != "user" {
		t.Fatalf("first role = %q", plan.Messages[0].Role)
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
		t.Fatalf("callbacks = %d, want 2", len(payload.Callbacks))
	}
	if payload.Callbacks[0]["kind"] != "async" || payload.Callbacks[1]["kind"] != "external_message" {
		t.Fatalf("kinds = %v", []any{payload.Callbacks[0]["kind"], payload.Callbacks[1]["kind"]})
	}
	if payload.Callbacks[1]["trigger_id"] != "trig-1" {
		t.Fatalf("trigger_id = %v", payload.Callbacks[1]["trigger_id"])
	}
}

func TestPlanSingleSideEffectApply_singleStillToolCallback(t *testing.T) {
	hub := stream.NewHub(4, logx.Discard())
	orch := testOrchestrator(t, hub, &llm.MockClient{})
	built := orch.BuildSideEffectMessages(SideEffectAsync, "s", queue.AsyncToolResultPayload{
		JobID: "job-1", ToolName: "bash_run", Status: "succeeded", ResultText: "done",
	}, "", "")
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
