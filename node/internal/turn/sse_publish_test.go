package turn

import (
	"log/slog"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

func TestPartialToolCallDoesNotAdvanceReplayableAgentCursor(t *testing.T) {
	hub := stream.NewHub(16, nil)
	orchestrator := &Orchestrator{hub: hub, logger: slog.Default()}
	call := llm.ToolCall{
		ID:   "call-1",
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      "bash_run",
			Arguments: `{"command":`,
		},
	}

	orchestrator.publishToolCall("agent-a", call, true, 0)
	if got := hub.CurrentAgentSeq("agent-a"); got != 0 {
		t.Fatalf("partial tool-call cursor = %d, want 0", got)
	}

	call.Function.Arguments = `{"command":"pwd"}`
	orchestrator.publishToolCall("agent-a", call, false, 0)
	if got := hub.CurrentAgentSeq("agent-a"); got != 1 {
		t.Fatalf("complete tool-call cursor = %d, want 1", got)
	}

	subscription := hub.SubscribeAgentCursor(0, 0, "agent-a")
	defer hub.Unsubscribe(subscription.Events)
	select {
	case event := <-subscription.Events:
		if event.Type != "tool_call" || event.Data["partial"] != false || event.AgentSeq != 1 {
			t.Fatalf("replayed event = %+v", event)
		}
	default:
		t.Fatal("complete tool call was not replayable")
	}
}
