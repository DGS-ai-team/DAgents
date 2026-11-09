package turn

import (
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
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

func TestCancelPendingTwiceNoDuplicate(t *testing.T) {
	hub := stream.NewHub(8, logx.Discard())
	orch := testOrchestrator(t, hub, &llm.MockClient{})
	events := hub.Subscribe(0)
	defer hub.Unsubscribe(events)

	history := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call-bash", Function: llm.ToolCallFunction{Name: "bash_run", Arguments: `{"command":"echo hi"}`}}}},
	}
	pending := &PendingHITL{
		Items: []PendingHITLItem{{
			ToolCall: llm.ToolCall{ID: "call-bash", Function: llm.ToolCallFunction{Name: "bash_run", Arguments: `{"command":"echo hi"}`}},
		}},
	}

	orch.CancelPendingToolCalls("sess-1", &history, pending, ToolUserInterruptedMessage, map[string]any{"cancelled_by_turn": true})
	orch.CancelPendingToolCalls("sess-1", &history, pending, ToolUserInterruptedMessage, map[string]any{"cancelled_by_turn": true})

	if got := countToolResponses(history, "call-bash"); got != 1 {
		t.Fatalf("tool response count = %d, want 1: %+v", got, history)
	}
	for _, message := range history {
		if message.Role == "tool" && message.ToolCallID == "call-bash" {
			if message.ToolResultMetadata == nil || message.ToolResultMetadata.Status != string(tools.ResultStatusCancelled) {
				t.Fatalf("cancelled tool result metadata = %+v, want cancelled", message.ToolResultMetadata)
			}
		}
	}
	select {
	case event := <-events:
		if event.Type != "tool_result" || event.Data["status"] != string(tools.ResultStatusCancelled) {
			t.Fatalf("cancelled tool result SSE=%#v, want status=cancelled", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for cancelled tool_result event")
	}
}
