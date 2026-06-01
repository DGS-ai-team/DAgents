package turn

import (
	"context"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

func TestHandleAsyncToolResult_appendsAssistantAndToolNotUser(t *testing.T) {
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

	before := len(history)
	outcome := orch.HandleAsyncToolResult(context.Background(), "sess-1", &history, AsyncToolResultInput{
		JobID: "job-1", ToolName: "bash_run", ToolCallID: "async-job-1", Status: "succeeded", ResultText: "done",
	}, nil, 0)
	if outcome.Err != nil {
		t.Fatal(outcome.Err)
	}
	if len(history) < before+2 {
		t.Fatalf("history too short: %+v", history)
	}
	if history[before].Role != "assistant" || len(history[before].ToolCalls) == 0 ||
		history[before].ToolCalls[0].Function.Name != "tool_callback" {
		t.Fatalf("async pair[0] = %+v", history[before])
	}
	if history[before+1].Role != "tool" {
		t.Fatalf("async pair[1] = %+v", history[before+1])
	}
	for i := before; i < before+2; i++ {
		if history[i].Role == "user" {
			t.Fatalf("async_tool_result must not write user at index %d", i)
		}
	}
}
