package turn

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

func TestRepairUnrespondedToolCalls_appendsToolMessages(t *testing.T) {
	hub := stream.NewHub(8, logx.Discard())
	orch := testOrchestrator(t, hub, &llm.MockClient{})

	history := []llm.Message{
		{Role: "user", Content: "run tool"},
		{Role: "assistant", Content: "", ToolCalls: []llm.ToolCall{{
			ID: "call-orphan", Type: "function",
			Function: llm.ToolCallFunction{Name: "bash_run", Arguments: `{"command":"echo hi"}`},
		}}},
		{Role: "user", Content: "follow up"},
	}

	if !orch.RepairUnrespondedToolCalls("sess-1", &history) {
		t.Fatal("expected repair")
	}
	if len(history) != 4 {
		t.Fatalf("history len = %d, want 4: %+v", len(history), history)
	}
	if history[2].Role != "tool" || history[2].ToolCallID != "call-orphan" {
		t.Fatalf("repaired tool = %+v", history[2])
	}
	if history[3].Role != "user" || history[3].Content != "follow up" {
		t.Fatalf("tail user moved: %+v", history[3])
	}
}

func TestRepairUnrespondedToolCalls_noopWhenAnswered(t *testing.T) {
	hub := stream.NewHub(8, logx.Discard())
	orch := testOrchestrator(t, hub, &llm.MockClient{})

	history := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call-1"}}},
		{Role: "tool", ToolCallID: "call-1", Content: "ok"},
	}
	before := len(history)
	if orch.RepairUnrespondedToolCalls("sess-1", &history) {
		t.Fatal("unexpected repair")
	}
	if len(history) != before {
		t.Fatalf("history changed: %+v", history)
	}
}

func TestUnrespondedToolCallsAfterLastAssistant_ignoresEarlierAssistant(t *testing.T) {
	history := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "old"}}},
		{Role: "tool", ToolCallID: "old", Content: "done"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "new"}}},
	}
	calls := unrespondedToolCallsAfterLastAssistant(history)
	if len(calls) != 1 || calls[0].ID != "new" {
		t.Fatalf("calls = %+v", calls)
	}
}
