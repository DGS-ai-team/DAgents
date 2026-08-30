package turn

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

func countToolResponses(history []llm.Message, toolCallID string) int {
	n := 0
	for _, m := range history {
		if m.Role == "tool" && m.ToolCallID == toolCallID {
			n++
		}
	}
	return n
}

func TestCancelPendingThenRepair_noDuplicate(t *testing.T) {
	hub := stream.NewHub(8, logx.Discard())
	orch := testOrchestrator(t, hub, &llm.MockClient{})

	history := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{
			ID: "call-bash", Type: "function",
			Function: llm.ToolCallFunction{Name: "bash_run", Arguments: `{"command":"echo hi"}`},
		}}},
	}
	pending := &PendingHITL{
		Items: []PendingHITLItem{{
			ToolCall: llm.ToolCall{
				ID: "call-bash", Type: "function",
				Function: llm.ToolCallFunction{Name: "bash_run", Arguments: `{"command":"echo hi"}`},
			},
		}},
	}

	orch.CancelPendingToolCalls("sess-1", &history, pending, ToolUserInterruptedMessage, map[string]any{"cancelled_by_turn": true})
	orch.RepairUnrespondedToolCalls("sess-1", &history)

	if got := countToolResponses(history, "call-bash"); got != 1 {
		t.Fatalf("tool response count = %d, want 1: %+v", got, history)
	}
	if history[1].Role != "tool" || history[1].Content != ToolUserInterruptedMessage {
		t.Fatalf("repaired tool = %+v", history[1])
	}
}

func TestRepairThenCancelPending_noDuplicate(t *testing.T) {
	hub := stream.NewHub(8, logx.Discard())
	orch := testOrchestrator(t, hub, &llm.MockClient{})

	history := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{
			ID: "call-bash", Type: "function",
			Function: llm.ToolCallFunction{Name: "bash_run", Arguments: `{"command":"echo hi"}`},
		}}},
	}
	pending := &PendingHITL{
		Items: []PendingHITLItem{{
			ToolCall: llm.ToolCall{
				ID: "call-bash", Type: "function",
				Function: llm.ToolCallFunction{Name: "bash_run", Arguments: `{"command":"echo hi"}`},
			},
		}},
	}

	orch.RepairUnrespondedToolCalls("sess-1", &history)
	orch.CancelPendingToolCalls("sess-1", &history, pending, ToolUserInterruptedMessage, map[string]any{"cancelled_by_turn": true})

	if got := countToolResponses(history, "call-bash"); got != 1 {
		t.Fatalf("tool response count = %d, want 1: %+v", got, history)
	}
}

func TestCancelPendingTwiceNoDuplicate(t *testing.T) {
	hub := stream.NewHub(8, logx.Discard())
	orch := testOrchestrator(t, hub, &llm.MockClient{})

	history := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call-bash", Function: llm.ToolCallFunction{Name: "bash_run"}}}},
	}
	pending := &PendingHITL{
		Items: []PendingHITLItem{{
			ToolCall: llm.ToolCall{ID: "call-bash", Function: llm.ToolCallFunction{Name: "bash_run"}},
		}},
	}

	orch.CancelPendingToolCalls("sess-1", &history, pending, ToolUserInterruptedMessage, map[string]any{"cancelled_by_turn": true})
	orch.CancelPendingToolCalls("sess-1", &history, pending, ToolUserInterruptedMessage, map[string]any{"cancelled_by_turn": true})

	if got := countToolResponses(history, "call-bash"); got != 1 {
		t.Fatalf("tool response count = %d, want 1: %+v", got, history)
	}
}
