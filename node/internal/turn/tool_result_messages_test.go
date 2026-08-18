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

func TestSplitToolResultKeepsReadableClientAndCompressesHistory(t *testing.T) {
	output := strings.Repeat("INFO log line\n", 2000) + "ERROR: command failed\n"
	payload := map[string]any{
		"terminal_id": "term-1",
		"output":      output,
		"next_seq":    9,
		"exited":      true,
	}
	rawBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(rawBytes)
	client, history, _ := (&Orchestrator{}).splitToolResult("session-1", llm.ToolCall{
		ID: "call-terminal", Type: "function",
		Function: llm.ToolCallFunction{Name: "terminal_read", Arguments: `{}`},
	}, raw)
	if client == raw || strings.ContainsAny(client, "\x1b\x07\r") {
		t.Fatal("terminal client result should be readable")
	}
	if len(history) >= len(raw) {
		t.Fatalf("terminal history was not compacted: raw=%d history=%d", len(raw), len(history))
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(history), &got); err != nil {
		t.Fatalf("history is not valid terminal JSON: %v", err)
	}
	if got["next_seq"].(float64) != 9 || got["exited"] != true {
		t.Fatalf("terminal metadata was lost: %#v", got)
	}
}

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
